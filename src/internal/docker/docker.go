package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const workDir = "/smoko-work"

// Client wraps the Docker SDK.
type Client struct {
	cli        *client.Client
	pulledOnce sync.Map // tracks images already verified as present
}

// New creates a new Docker client using environment defaults (DOCKER_HOST etc.).
func New() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker: create client: %w", err)
	}
	return &Client{cli: cli}, nil
}

// Close releases the underlying client.
func (c *Client) Close() error { return c.cli.Close() }

// PullIfMissing pulls the image if it isn't already present locally.
// Results are cached: subsequent calls for the same image are no-ops.
func (c *Client) PullIfMissing(ctx context.Context, imageName string) error {
	if _, ok := c.pulledOnce.Load(imageName); ok {
		return nil
	}
	// Try to inspect locally first
	_, _, err := c.cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		c.pulledOnce.Store(imageName, true)
		return nil // already present
	}
	rc, err := c.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("docker: pull %s: %w", imageName, err)
	}
	_, _ = io.Copy(io.Discard, rc) // consume output so pull completes
	if err := rc.Close(); err != nil {
		return err
	}
	c.pulledOnce.Store(imageName, true)
	return nil
}

// CreateContainer creates a container with the given image and env vars.
// Returns the container ID.
func (c *Client) CreateContainer(ctx context.Context, imageName string, env []string) (string, error) {
	resp, err := c.cli.ContainerCreate(ctx, &container.Config{
		Image:      imageName,
		Env:        env,
		WorkingDir: workDir,
		// Keep the container alive so we can exec into it
		Cmd: []string{"sh", "-c", "mkdir -p " + workDir + " && tail -f /dev/null"},
		Tty: false,
	}, nil, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("docker: create container: %w", err)
	}
	if err := c.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("docker: start container: %w", err)
	}
	return resp.ID, nil
}

// ExecCommand runs a shell command inside containerID and returns stdout, stderr, and the exit code.
func (c *Client) ExecCommand(ctx context.Context, containerID, workdir, command string, stdin string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	if workdir == "" {
		workdir = workDir
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	execResp, err := c.cli.ContainerExecCreate(execCtx, containerID, container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  stdin != "",
		WorkingDir:   workdir,
		Cmd:          []string{"sh", "-c", command},
	})
	if err != nil {
		return "", "", -1, fmt.Errorf("docker: exec create: %w", err)
	}

	attach, err := c.cli.ContainerExecAttach(execCtx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return "", "", -1, fmt.Errorf("docker: exec attach: %w", err)
	}
	defer attach.Close()

	if stdin != "" {
		_, _ = io.WriteString(attach.Conn, stdin)
		attach.CloseWrite()
	}

	var outBuf, errBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&outBuf, &errBuf, attach.Reader); err != nil && execCtx.Err() == nil {
		return "", "", -1, fmt.Errorf("docker: read exec output: %w", err)
	}

	inspect, err := c.cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return "", "", -1, fmt.Errorf("docker: exec inspect: %w", err)
	}

	return outBuf.String(), errBuf.String(), inspect.ExitCode, nil
}

// FileEntry represents a file to write into a container.
type FileEntry struct {
	Path    string // absolute path inside the container
	Content string
}

// WriteFile copies content into the container at path (creates parent dirs).
func (c *Client) WriteFile(ctx context.Context, containerID, path, content string) error {
	return c.WriteFiles(ctx, containerID, []FileEntry{{Path: path, Content: content}})
}

// WriteFiles copies multiple files into the container in a single tar upload.
// Parent directories are created automatically via tar directory entries, avoiding
// individual mkdir exec calls.
func (c *Client) WriteFiles(ctx context.Context, containerID string, files []FileEntry) error {
	if len(files) == 0 {
		return nil
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Track which directories we've already added to the tar
	dirs := make(map[string]bool)
	for _, f := range files {
		// Ensure all parent directories are represented in the tar
		parts := strings.Split(strings.TrimPrefix(f.Path, "/"), "/")
		for i := 1; i < len(parts); i++ {
			dir := "/" + strings.Join(parts[:i], "/") + "/"
			if !dirs[dir] {
				dirs[dir] = true
				_ = tw.WriteHeader(&tar.Header{
					Typeflag: tar.TypeDir,
					Name:     dir,
					Mode:     0755,
				})
			}
		}

		body := []byte(f.Content)
		if err := tw.WriteHeader(&tar.Header{
			Name: f.Path,
			Mode: 0644,
			Size: int64(len(body)),
		}); err != nil {
			return err
		}
		if _, err := tw.Write(body); err != nil {
			return err
		}
	}
	tw.Close()

	return c.cli.CopyToContainer(ctx, containerID, "/", &buf, container.CopyToContainerOptions{})
}

// MakeDir creates a directory (and parents) inside the container.
func (c *Client) MakeDir(ctx context.Context, containerID, path string) error {
	_, _, code, err := c.ExecCommand(ctx, containerID, "", "mkdir -p "+ShellQuote(workDir+"/"+strings.TrimPrefix(path, "/")), "", 10*time.Second)
	if err != nil {
		return fmt.Errorf("docker: mkdir %s: %w", path, err)
	}
	if code != 0 {
		return fmt.Errorf("docker: mkdir %s: exit code %d", path, code)
	}
	return nil
}

// ReadFile reads the contents of a file inside the container.
func (c *Client) ReadFile(ctx context.Context, containerID, path string) (string, error) {
	absPath := absWorkPath(path)
	stdout, _, code, err := c.ExecCommand(ctx, containerID, "", "cat "+ShellQuote(absPath), "", 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("docker: read file %s: %w", path, err)
	}
	if code != 0 {
		return "", fmt.Errorf("docker: file %s not found (exit %d)", path, code)
	}
	return stdout, nil
}

// FileExists checks if a file exists inside the container.
func (c *Client) FileExists(ctx context.Context, containerID, path string) (bool, error) {
	absPath := absWorkPath(path)
	_, _, code, err := c.ExecCommand(ctx, containerID, "", "test -f "+ShellQuote(absPath), "", 10*time.Second)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// DirExists checks if a directory exists inside the container.
func (c *Client) DirExists(ctx context.Context, containerID, path string) (bool, error) {
	absPath := absWorkPath(path)
	_, _, code, err := c.ExecCommand(ctx, containerID, "", "test -d "+ShellQuote(absPath), "", 10*time.Second)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// RemoveContainer stops and removes a container.
func (c *Client) RemoveContainer(ctx context.Context, containerID string) error {
	_ = c.cli.ContainerStop(ctx, containerID, container.StopOptions{})
	return c.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// FSCheckKind identifies the type of filesystem check.
type FSCheckKind int

const (
	FSCheckFileExists FSCheckKind = iota
	FSCheckDirExists
	FSCheckReadFile
)

// FSCheck describes a single filesystem check to batch.
type FSCheck struct {
	Kind FSCheckKind
	Path string // path (absolute or relative to workdir)
}

// FSResult holds the result of a single batched filesystem check.
type FSResult struct {
	Exists  bool
	Content string // only populated for FSCheckReadFile
	Err     error
}

// BatchFSCheck performs multiple filesystem checks in a single docker exec.
// Returns results in the same order as the input checks.
func (c *Client) BatchFSCheck(ctx context.Context, containerID string, checks []FSCheck) ([]FSResult, error) {
	if len(checks) == 0 {
		return nil, nil
	}

	// For a single check, fall back to individual methods for simplicity
	if len(checks) == 1 {
		return c.singleFSCheck(ctx, containerID, checks[0])
	}

	// Build a shell script that outputs structured results. File reads use a
	// byte count so marker-looking file content cannot corrupt parsing.
	var sb strings.Builder
	for i, chk := range checks {
		absPath := absWorkPath(chk.Path)
		startMarker := fmt.Sprintf("__SMOKO_CHK_%d_START__", i)
		endMarker := fmt.Sprintf("__SMOKO_CHK_%d_END__", i)

		switch chk.Kind {
		case FSCheckFileExists:
			fmt.Fprintf(&sb, "printf '%%s\\n' %s; test -f %s && printf 'YES\\n' || printf 'NO\\n'; printf '%%s\\n' %s\n",
				ShellQuote(startMarker), ShellQuote(absPath), ShellQuote(endMarker))
		case FSCheckDirExists:
			fmt.Fprintf(&sb, "printf '%%s\\n' %s; test -d %s && printf 'YES\\n' || printf 'NO\\n'; printf '%%s\\n' %s\n",
				ShellQuote(startMarker), ShellQuote(absPath), ShellQuote(endMarker))
		case FSCheckReadFile:
			quotedPath := ShellQuote(absPath)
			fmt.Fprintf(&sb, "printf '%%s\\n' %s; if bytes=$(wc -c < %s 2>/dev/null); then bytes=$(printf '%%s' \"$bytes\" | tr -d '[:space:]'); printf 'OK %%s\\n' \"$bytes\"; cat %s 2>/dev/null; printf '\\n%%s\\n' %s; else printf 'ERR\\n'; printf '%%s\\n' %s; fi\n",
				ShellQuote(startMarker), quotedPath, quotedPath, ShellQuote(endMarker), ShellQuote(endMarker))
		}
	}

	stdout, _, _, err := c.ExecCommand(ctx, containerID, "", sb.String(), "", 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("docker: batch fs check: %w", err)
	}

	return parseBatchFSCheckOutput(stdout, checks), nil
}

func parseBatchFSCheckOutput(stdout string, checks []FSCheck) []FSResult {
	results := make([]FSResult, len(checks))
	pos := 0

	for i, chk := range checks {
		startMarker := fmt.Sprintf("__SMOKO_CHK_%d_START__", i)
		endMarker := fmt.Sprintf("__SMOKO_CHK_%d_END__", i)

		line, next, ok := readProtocolLine(stdout, pos)
		if !ok || line != startMarker {
			results[i] = FSResult{Err: fmt.Errorf("missing marker for check %d", i)}
			continue
		}
		pos = next

		switch chk.Kind {
		case FSCheckFileExists, FSCheckDirExists:
			resultLine, next, ok := readProtocolLine(stdout, pos)
			if !ok {
				results[i] = FSResult{Err: fmt.Errorf("missing result for check %d", i)}
				continue
			}
			pos = next
			if err := consumeEndMarker(stdout, &pos, endMarker, i); err != nil {
				results[i] = FSResult{Err: err}
				continue
			}
			switch resultLine {
			case "YES":
				results[i] = FSResult{Exists: true}
			case "NO":
				results[i] = FSResult{Exists: false}
			default:
				results[i] = FSResult{Err: fmt.Errorf("invalid result %q for check %d", resultLine, i)}
			}
		case FSCheckReadFile:
			header, next, ok := readProtocolLine(stdout, pos)
			if !ok {
				results[i] = FSResult{Err: fmt.Errorf("missing read header for check %d", i)}
				continue
			}
			pos = next
			if header == "ERR" {
				if err := consumeEndMarker(stdout, &pos, endMarker, i); err != nil {
					results[i] = FSResult{Err: err}
					continue
				}
				results[i] = FSResult{Err: fmt.Errorf("docker: file %s not found", chk.Path)}
				continue
			}
			if !strings.HasPrefix(header, "OK ") {
				results[i] = FSResult{Err: fmt.Errorf("invalid read header %q for check %d", header, i)}
				continue
			}
			size, err := parseByteCount(strings.TrimPrefix(header, "OK "))
			if err != nil {
				results[i] = FSResult{Err: fmt.Errorf("invalid byte count for check %d: %w", i, err)}
				continue
			}
			if size < 0 || pos+size > len(stdout) {
				results[i] = FSResult{Err: fmt.Errorf("truncated file content for check %d", i)}
				continue
			}
			content := stdout[pos : pos+size]
			pos += size
			if pos >= len(stdout) || stdout[pos] != '\n' {
				results[i] = FSResult{Err: fmt.Errorf("missing content delimiter for check %d", i)}
				continue
			}
			pos++
			if err := consumeEndMarker(stdout, &pos, endMarker, i); err != nil {
				results[i] = FSResult{Err: err}
				continue
			}
			results[i] = FSResult{Exists: true, Content: content}
		default:
			results[i] = FSResult{Err: fmt.Errorf("unknown check kind")}
		}
	}

	return results
}

func readProtocolLine(s string, pos int) (line string, next int, ok bool) {
	if pos > len(s) {
		return "", pos, false
	}
	idx := strings.IndexByte(s[pos:], '\n')
	if idx < 0 {
		return "", pos, false
	}
	line = s[pos : pos+idx]
	line = strings.TrimSuffix(line, "\r")
	return line, pos + idx + 1, true
}

func consumeEndMarker(stdout string, pos *int, endMarker string, checkIndex int) error {
	line, next, ok := readProtocolLine(stdout, *pos)
	if !ok || line != endMarker {
		return fmt.Errorf("missing end marker for check %d", checkIndex)
	}
	*pos = next
	return nil
}

func parseByteCount(raw string) (int, error) {
	var size int
	if _, err := fmt.Sscan(raw, &size); err != nil {
		return 0, err
	}
	return size, nil
}

func (c *Client) singleFSCheck(ctx context.Context, containerID string, chk FSCheck) ([]FSResult, error) {
	switch chk.Kind {
	case FSCheckFileExists:
		exists, err := c.FileExists(ctx, containerID, chk.Path)
		return []FSResult{{Exists: exists, Err: err}}, nil
	case FSCheckDirExists:
		exists, err := c.DirExists(ctx, containerID, chk.Path)
		return []FSResult{{Exists: exists, Err: err}}, nil
	case FSCheckReadFile:
		content, err := c.ReadFile(ctx, containerID, chk.Path)
		if err != nil {
			return []FSResult{{Err: err}}, nil
		}
		return []FSResult{{Exists: true, Content: content}}, nil
	default:
		return []FSResult{{Err: fmt.Errorf("unknown check kind")}}, nil
	}
}

// WorkDir returns the working directory used inside containers.
func WorkDir() string { return workDir }

func absWorkPath(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return workDir + "/" + p
}

// ShellQuote wraps s in single quotes, escaping any existing single quotes.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

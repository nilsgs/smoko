package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const workDir = "/smoko-work"

// Client wraps the Docker SDK.
type Client struct {
	cli *client.Client
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
func (c *Client) PullIfMissing(ctx context.Context, imageName string) error {
	// Try to inspect locally first
	_, _, err := c.cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return nil // already present
	}
	rc, err := c.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("docker: pull %s: %w", imageName, err)
	}
	_, _ = io.Copy(io.Discard, rc) // consume output so pull completes
	return rc.Close()
}

// CreateContainer creates a container with the given image and env vars.
// Returns the container ID.
func (c *Client) CreateContainer(ctx context.Context, imageName string, env []string) (string, error) {
	resp, err := c.cli.ContainerCreate(ctx, &container.Config{
		Image:      imageName,
		Env:        env,
		WorkingDir: workDir,
		// Keep the container alive so we can exec into it
		Cmd:        []string{"sh", "-c", "mkdir -p " + workDir + " && tail -f /dev/null"},
		Tty:        false,
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

// WriteFile copies content into the container at path (creates parent dirs).
func (c *Client) WriteFile(ctx context.Context, containerID, path, content string) error {
	// Ensure the parent directory exists
	dir := parentDir(path)
	if dir != "" && dir != "." {
		if _, _, code, err := c.ExecCommand(ctx, containerID, "", "mkdir -p "+ShellQuote(dir), "", 10*time.Second); err != nil || code != 0 {
			return fmt.Errorf("docker: mkdir %s: %w", dir, err)
		}
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte(content)
	if err := tw.WriteHeader(&tar.Header{
		Name: path,
		Mode: 0644,
		Size: int64(len(body)),
	}); err != nil {
		return err
	}
	if _, err := tw.Write(body); err != nil {
		return err
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

// WorkDir returns the working directory used inside containers.
func WorkDir() string { return workDir }

func absWorkPath(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return workDir + "/" + p
}

func parentDir(path string) string {
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		return path[:idx]
	}
	return ""
}

// ShellQuote wraps s in single quotes, escaping any existing single quotes.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

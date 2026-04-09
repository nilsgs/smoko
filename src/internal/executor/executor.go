package executor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nskut/smoko/internal/docker"
	"github.com/nskut/smoko/internal/parser"
)

type dockerRunner interface {
	WriteFile(ctx context.Context, containerID, path, content string) error
	WriteFiles(ctx context.Context, containerID string, files []docker.FileEntry) error
	MakeDir(ctx context.Context, containerID, path string) error
	ExecCommand(ctx context.Context, containerID, workdir, command, stdin string, timeout time.Duration) (stdout, stderr string, exitCode int, err error)
}

// WriteEnvFile writes all environment variable declarations to .smoko_env inside
// the container in one atomic write. Must be called once after container creation
// and before any Given steps run.
func WriteEnvFile(ctx context.Context, dc dockerRunner, containerID string, vars []string) error {
	if len(vars) == 0 {
		return nil
	}

	var sb strings.Builder
	for _, kv := range vars {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name, value := kv[:eq], kv[eq+1:]
		fmt.Fprintf(&sb, "export %s=%s\n", name, docker.ShellQuote(value))
	}

	return dc.WriteFile(ctx, containerID, docker.WorkDir()+"/.smoko_env", sb.String())
}

// RunGivenSteps executes all Given steps against the container, batching file
// operations only when they are adjacent so the declared order is preserved.
func RunGivenSteps(ctx context.Context, dc dockerRunner, containerID string, steps []parser.Step, timeout time.Duration) error {
	ops, err := buildGivenOps(steps)
	if err != nil {
		return err
	}

	for _, op := range ops {
		if err := executeGivenOp(ctx, dc, containerID, op, timeout); err != nil {
			return err
		}
	}

	return nil
}

// RunGiven executes a single Given step against the container.
// Prefer RunGivenSteps for ordered, batched execution.
func RunGiven(ctx context.Context, dc dockerRunner, containerID string, step parser.Step, timeout time.Duration) error {
	ops, err := buildGivenOps([]parser.Step{step})
	if err != nil {
		return err
	}

	for _, op := range ops {
		if err := executeGivenOp(ctx, dc, containerID, op, timeout); err != nil {
			return err
		}
	}

	return nil
}

// CollectEnvVars scans Given steps for environment variable declarations and
// returns them as KEY=VALUE pairs for container creation.
func CollectEnvVars(steps []parser.Step) []string {
	var env []string
	for _, s := range steps {
		if s.ResolvedType != parser.StepGiven {
			continue
		}
		if m := reEnvVar.FindStringSubmatch(s.Text); m != nil {
			env = append(env, m[1]+"="+m[2])
		}
	}
	return env
}

// RunWhen executes a When step and returns the captured result.
func RunWhen(ctx context.Context, dc dockerRunner, containerID string, step parser.Step, timeout time.Duration) (*WhenResult, error) {
	text := step.Text

	var command, stdin string
	var expectedExitCode *int

	if m := reRunWithInput.FindStringSubmatch(text); m != nil {
		command = m[1]
		stdin = m[2]
	} else if m := reRunExpectingCode.FindStringSubmatch(text); m != nil {
		command = m[1]
		code := 0
		if _, err := fmt.Sscan(m[2], &code); err != nil {
			return nil, fmt.Errorf("invalid exit code %q: %w", m[2], err)
		}
		expectedExitCode = &code
	} else if m := reRun.FindStringSubmatch(text); m != nil {
		command = m[1]
	} else {
		return nil, fmt.Errorf("unknown When step: %q", text)
	}

	stdout, stderr, exitCode, err := dc.ExecCommand(ctx, containerID, docker.WorkDir(), wrapCommand(command), stdin, timeout)
	if err != nil {
		return nil, fmt.Errorf("when step exec: %w", err)
	}

	return &WhenResult{
		Stdout:           stdout,
		Stderr:           stderr,
		ExitCode:         exitCode,
		ExpectedExitCode: expectedExitCode,
		Command:          command,
	}, nil
}

// WhenResult holds captured output from a When step.
type WhenResult struct {
	Stdout           string
	Stderr           string
	ExitCode         int
	ExpectedExitCode *int // nil = not constrained by When step
	Command          string
}

// CombinedOutput returns stdout + stderr.
func (r *WhenResult) CombinedOutput() string {
	return r.Stdout + r.Stderr
}

type givenKind int

const (
	givenNoop givenKind = iota
	givenFile
	givenDir
	givenRun
)

type givenAction struct {
	kind     givenKind
	stepText string
	file     docker.FileEntry
	path     string
	command  string
}

type givenOpKind int

const (
	givenOpWriteFiles givenOpKind = iota
	givenOpMakeDir
	givenOpRunCommand
)

type givenOp struct {
	kind     givenOpKind
	stepText string
	files    []docker.FileEntry
	path     string
	command  string
}

func buildGivenOps(steps []parser.Step) ([]givenOp, error) {
	var ops []givenOp
	var pendingFiles []docker.FileEntry

	flushFiles := func() {
		if len(pendingFiles) == 0 {
			return
		}
		files := append([]docker.FileEntry(nil), pendingFiles...)
		ops = append(ops, givenOp{kind: givenOpWriteFiles, files: files})
		pendingFiles = nil
	}

	for _, step := range steps {
		if step.ResolvedType != parser.StepGiven {
			continue
		}

		action, err := classifyGivenStep(step)
		if err != nil {
			return nil, err
		}

		switch action.kind {
		case givenNoop:
			continue
		case givenFile:
			pendingFiles = append(pendingFiles, action.file)
		case givenDir:
			flushFiles()
			ops = append(ops, givenOp{
				kind:     givenOpMakeDir,
				stepText: action.stepText,
				path:     action.path,
			})
		case givenRun:
			flushFiles()
			ops = append(ops, givenOp{
				kind:     givenOpRunCommand,
				stepText: action.stepText,
				command:  action.command,
			})
		}
	}

	flushFiles()
	return ops, nil
}

func classifyGivenStep(step parser.Step) (givenAction, error) {
	text := step.Text

	if m := reFileWithContent.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenFile,
			stepText: text,
			file: docker.FileEntry{
				Path:    docker.WorkDir() + "/" + m[1],
				Content: step.Block,
			},
		}, nil
	}

	if m := reFileExists.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenFile,
			stepText: text,
			file: docker.FileEntry{
				Path:    docker.WorkDir() + "/" + m[1],
				Content: "",
			},
		}, nil
	}

	if m := reDirExists.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenDir,
			stepText: text,
			path:     m[1],
		}, nil
	}

	if m := reGivenRun.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenRun,
			stepText: text,
			command:  m[1],
		}, nil
	}

	if reEmptyWorkDir.MatchString(text) {
		return givenAction{kind: givenNoop, stepText: text}, nil
	}

	if reEnvVar.MatchString(text) {
		return givenAction{kind: givenNoop, stepText: text}, nil
	}

	return givenAction{}, fmt.Errorf("unknown Given step: %q", text)
}

func executeGivenOp(ctx context.Context, dc dockerRunner, containerID string, op givenOp, timeout time.Duration) error {
	switch op.kind {
	case givenOpWriteFiles:
		if err := dc.WriteFiles(ctx, containerID, op.files); err != nil {
			return fmt.Errorf("write files: %w", err)
		}
	case givenOpMakeDir:
		if err := dc.MakeDir(ctx, containerID, op.path); err != nil {
			return fmt.Errorf("Given %q: %w", op.stepText, err)
		}
	case givenOpRunCommand:
		if err := runGivenCommand(ctx, dc, containerID, op.command, timeout); err != nil {
			return fmt.Errorf("Given %q: %w", op.stepText, err)
		}
	}

	return nil
}

func runGivenCommand(ctx context.Context, dc dockerRunner, containerID, command string, timeout time.Duration) error {
	stdout, stderr, exitCode, err := dc.ExecCommand(ctx, containerID, docker.WorkDir(), wrapCommand(command), "", timeout)
	if err != nil {
		return fmt.Errorf("setup command %q: %w", command, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("%s", formatCommandFailure(command, exitCode, stdout, stderr))
	}
	return nil
}

func wrapCommand(command string) string {
	return "[ -f " + docker.WorkDir() + "/.smoko_env ] && . " + docker.WorkDir() + "/.smoko_env; " + command
}

func formatCommandFailure(command string, exitCode int, stdout, stderr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "command %q exited with code %d", command, exitCode)
	if stdout != "" {
		fmt.Fprintf(&b, "\nstdout:\n%s", stdout)
	}
	if stderr != "" {
		fmt.Fprintf(&b, "\nstderr:\n%s", stderr)
	}
	return b.String()
}

var (
	reFileWithContent = regexp.MustCompile(`^a file "([^"]+)" with content:?$`)
	reFileExists      = regexp.MustCompile(`^a file "([^"]+)" exists$`)
	reDirExists       = regexp.MustCompile(`^(?:the )?directory "([^"]+)" exists$`)
	reEmptyWorkDir    = regexp.MustCompile(`^an empty working directory$`)
	reEnvVar          = regexp.MustCompile(`^environment variable "([^"]+)" is set to "([^"]*)"$`)
	reGivenRun        = regexp.MustCompile(`^I run "([^"]+)"$`)

	reRun              = regexp.MustCompile(`^I run "([^"]+)"$`)
	reRunWithInput     = regexp.MustCompile(`^I run "([^"]+)" with input "([^"]*)"$`)
	reRunExpectingCode = regexp.MustCompile(`^I run "([^"]+)" expecting exit code (\d+)$`)
)

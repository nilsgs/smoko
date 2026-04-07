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

// WriteEnvFile writes all environment variable declarations to .smoko_env inside
// the container in one atomic write. Must be called once after container creation
// and before any Given steps run.
func WriteEnvFile(ctx context.Context, dc *docker.Client, containerID string, vars []string) error {
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

// RunGiven executes a Given step against the container.
// env accumulates environment variables that should be set at container-creation time.
// (env vars set in Given must be passed before CreateContainer, so they are collected
// during a pre-scan of Given steps; RunGiven handles file/dir operations only.)
func RunGiven(ctx context.Context, dc *docker.Client, containerID string, step parser.Step) error {
	text := step.Text

	// Given a file "X" with content: <block>
	if m := reFileWithContent.FindStringSubmatch(text); m != nil {
		path := m[1]
		content := step.Block
		absPath := docker.WorkDir() + "/" + path
		return dc.WriteFile(ctx, containerID, absPath, content)
	}

	// Given a file "X" exists
	if m := reFileExists.FindStringSubmatch(text); m != nil {
		path := m[1]
		absPath := docker.WorkDir() + "/" + path
		return dc.WriteFile(ctx, containerID, absPath, "")
	}

	// Given the directory "X" exists
	if m := reDirExists.FindStringSubmatch(text); m != nil {
		path := m[1]
		return dc.MakeDir(ctx, containerID, path)
	}

	// Given an empty working directory — no-op, container starts clean
	if reEmptyWorkDir.MatchString(text) {
		return nil
	}

	// Given environment variable "X" is set to "Y"
	// Env vars are written to .smoko_env once via WriteEnvFile before Given steps run.
	if reEnvVar.MatchString(text) {
		return nil
	}

	return fmt.Errorf("unknown Given step: %q", text)
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
func RunWhen(ctx context.Context, dc *docker.Client, containerID string, step parser.Step, timeout time.Duration) (*WhenResult, error) {
	text := step.Text

	var command, stdin string
	var expectedExitCode *int

	// When I run "cmd" with input "stdin"
	if m := reRunWithInput.FindStringSubmatch(text); m != nil {
		command = m[1]
		stdin = m[2]
	} else if m := reRunExpectingCode.FindStringSubmatch(text); m != nil {
		// When I run "cmd" expecting exit code N
		command = m[1]
		code := 0
		if _, err := fmt.Sscan(m[2], &code); err != nil {
			return nil, fmt.Errorf("invalid exit code %q: %w", m[2], err)
		}
		expectedExitCode = &code
	} else if m := reRun.FindStringSubmatch(text); m != nil {
		// When I run "cmd"
		command = m[1]
	} else {
		return nil, fmt.Errorf("unknown When step: %q", text)
	}

	// Source the env file if it exists, then run the command
	wrappedCmd := "[ -f " + docker.WorkDir() + "/.smoko_env ] && . " + docker.WorkDir() + "/.smoko_env; " + command

	stdout, stderr, exitCode, err := dc.ExecCommand(ctx, containerID, docker.WorkDir(), wrappedCmd, stdin, timeout)
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

var (
	reFileWithContent = regexp.MustCompile(`^a file "([^"]+)" with content:?$`)
	reFileExists      = regexp.MustCompile(`^a file "([^"]+)" exists$`)
	reDirExists       = regexp.MustCompile(`^(?:the )?directory "([^"]+)" exists$`)
	reEmptyWorkDir    = regexp.MustCompile(`^an empty working directory$`)
	reEnvVar          = regexp.MustCompile(`^environment variable "([^"]+)" is set to "([^"]*)"$`)

	reRun              = regexp.MustCompile(`^I run "([^"]+)"$`)
	reRunWithInput     = regexp.MustCompile(`^I run "([^"]+)" with input "([^"]*)"$`)
	reRunExpectingCode = regexp.MustCompile(`^I run "([^"]+)" expecting exit code (\d+)$`)
)

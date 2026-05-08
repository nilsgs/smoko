package assertions

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nskut/smoko/internal/docker"
)

type dockerCommandRunner interface {
	ExecCommand(ctx context.Context, containerID, workdir, command, stdin string, timeout time.Duration) (stdout, stderr string, exitCode int, err error)
}

func evaluateGitAssertion(ctx context.Context, text string, dc dockerReader, containerID string, env []string) (Result, bool) {
	if m := reGitCleanDirty.FindStringSubmatch(text); m != nil {
		repoPath, err := resolveGitAssertionRepoPath(unescapeString(m[1]), env)
		if err != nil {
			return fail("invalid git repository path %q: %v", m[1], err), true
		}
		status, result := gitStatusPorcelain(ctx, dc, containerID, repoPath)
		if !result.Pass {
			return result, true
		}

		dirty := strings.TrimSpace(status) != ""
		wantDirty := m[2] == "dirty"
		if wantDirty && !dirty {
			return fail("git repository %q is clean", repoPath), true
		}
		if !wantDirty && dirty {
			return fail("git repository %q is dirty\nStatus:\n%s", repoPath, status), true
		}
		return pass(), true
	}

	if m := reGitHasBranch.FindStringSubmatch(text); m != nil {
		repoPath, err := resolveGitAssertionRepoPath(unescapeString(m[1]), env)
		if err != nil {
			return fail("invalid git repository path %q: %v", m[1], err), true
		}
		branch := unescapeString(m[2])
		if strings.TrimSpace(branch) == "" {
			return fail("git branch name is empty"), true
		}

		command := "git -C " + docker.ShellQuote(repoPath) + " show-ref --verify --quiet " + docker.ShellQuote("refs/heads/"+branch)
		stdout, stderr, exitCode, result := runGitAssertionCommand(ctx, dc, containerID, command)
		if !result.Pass {
			return result, true
		}
		if exitCode == 0 {
			return pass(), true
		}
		if exitCode == 1 {
			return fail("git repository %q does not have branch %q", repoPath, branch), true
		}
		return fail("git branch check failed: %s", formatGitAssertionCommandFailure(command, exitCode, stdout, stderr)), true
	}

	return Result{}, false
}

func gitStatusPorcelain(ctx context.Context, dc dockerReader, containerID, repoPath string) (string, Result) {
	command := "git -C " + docker.ShellQuote(repoPath) + " status --porcelain"
	stdout, stderr, exitCode, result := runGitAssertionCommand(ctx, dc, containerID, command)
	if !result.Pass {
		return "", result
	}
	if exitCode != 0 {
		return "", fail("git status failed: %s", formatGitAssertionCommandFailure(command, exitCode, stdout, stderr))
	}
	return stdout, pass()
}

func runGitAssertionCommand(ctx context.Context, dc dockerReader, containerID, command string) (string, string, int, Result) {
	runner, ok := dc.(dockerCommandRunner)
	if !ok {
		return "", "", 0, fail("git assertions require a docker runner that can execute commands")
	}

	stdout, stderr, exitCode, err := runner.ExecCommand(ctx, containerID, docker.WorkDir(), requireGitAssertionCommand(command), "", 10*time.Second)
	if err != nil {
		return stdout, stderr, exitCode, fail("git assertion command failed: %v", err)
	}
	return stdout, stderr, exitCode, pass()
}

func requireGitAssertionCommand(command string) string {
	return "command -v git >/dev/null 2>&1 || { echo 'Git assertions require git on PATH in the test image.' >&2; exit 127; };\n" + command
}

func resolveGitAssertionRepoPath(repoPath string, env []string) (string, error) {
	expanded := expandVars(repoPath, env)
	normalized := strings.ReplaceAll(expanded, "\\", "/")
	if hasGitAssertionParentSegment(normalized) {
		return "", fmt.Errorf("path must not contain '..'")
	}
	return docker.WorkPath(normalized)
}

func hasGitAssertionParentSegment(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func formatGitAssertionCommandFailure(command string, exitCode int, stdout, stderr string) string {
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
	reGitCleanDirty = regexp.MustCompile(`^git repository "((?:[^"\\]|\\.)*)" is (clean|dirty)$`)
	reGitHasBranch  = regexp.MustCompile(`^git repository "((?:[^"\\]|\\.)*)" has branch "((?:[^"\\]|\\.)*)"$`)
)

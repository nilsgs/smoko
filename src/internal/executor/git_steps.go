package executor

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/nskut/smoko/internal/docker"
	"github.com/nskut/smoko/internal/parser"
)

type gitGivenKind int

const (
	gitGivenRepository gitGivenKind = iota
	gitGivenCommittedFile
	gitGivenUntrackedFile
	gitGivenModifiedFile
	gitGivenBranch
)

type gitGivenAction struct {
	kind     gitGivenKind
	repoPath string
	filePath string
	content  string
	branch   string
}

func classifyGitGivenStep(step parser.Step) (givenAction, bool, error) {
	text := step.Text

	if m := reGitRepoExists.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenGit,
			stepText: text,
			git: &gitGivenAction{
				kind:     gitGivenRepository,
				repoPath: unescapeGivenString(m[1]),
			},
		}, true, nil
	}

	if m := reGitCommittedFile.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenGit,
			stepText: text,
			git: &gitGivenAction{
				kind:     gitGivenCommittedFile,
				repoPath: unescapeGivenString(m[1]),
				filePath: unescapeGivenString(m[2]),
				content:  step.Block,
			},
		}, true, nil
	}

	if m := reGitUntrackedFile.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenGit,
			stepText: text,
			git: &gitGivenAction{
				kind:     gitGivenUntrackedFile,
				repoPath: unescapeGivenString(m[1]),
				filePath: unescapeGivenString(m[2]),
				content:  step.Block,
			},
		}, true, nil
	}

	if m := reGitModifiedFile.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenGit,
			stepText: text,
			git: &gitGivenAction{
				kind:     gitGivenModifiedFile,
				repoPath: unescapeGivenString(m[1]),
				filePath: unescapeGivenString(m[2]),
				content:  step.Block,
			},
		}, true, nil
	}

	if m := reGitBranch.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenGit,
			stepText: text,
			git: &gitGivenAction{
				kind:     gitGivenBranch,
				repoPath: unescapeGivenString(m[1]),
				branch:   unescapeGivenString(m[2]),
			},
		}, true, nil
	}

	return givenAction{}, false, nil
}

func executeGitGivenAction(ctx context.Context, dc dockerRunner, containerID string, action *gitGivenAction, timeout time.Duration) error {
	if action == nil {
		return fmt.Errorf("missing git action")
	}

	repoPath, err := cleanGitRepoPath(action.repoPath)
	if err != nil {
		return fmt.Errorf("invalid git repository path %q: %w", action.repoPath, err)
	}

	switch action.kind {
	case gitGivenRepository:
		return ensureGitRepository(ctx, dc, containerID, repoPath, timeout)
	case gitGivenCommittedFile:
		return createCommittedGitFile(ctx, dc, containerID, repoPath, action.filePath, action.content, timeout)
	case gitGivenUntrackedFile:
		return createUntrackedGitFile(ctx, dc, containerID, repoPath, action.filePath, action.content, timeout)
	case gitGivenModifiedFile:
		return createModifiedGitFile(ctx, dc, containerID, repoPath, action.filePath, action.content, timeout)
	case gitGivenBranch:
		return checkoutGitBranch(ctx, dc, containerID, repoPath, action.branch, timeout)
	default:
		return fmt.Errorf("unknown git action")
	}
}

func ensureGitRepository(ctx context.Context, dc dockerRunner, containerID, repoPath string, timeout time.Duration) error {
	repo := docker.ShellQuote(repoPath)
	command := strings.Join([]string{
		"set -e",
		"if [ ! -d " + repo + "/.git ]; then",
		"  mkdir -p " + repo,
		"  if ! git init -b main " + repo + " >/dev/null 2>&1; then",
		"    git init " + repo,
		"    git -C " + repo + " checkout -B main",
		"  fi",
		"fi",
		"git -C " + repo + " config user.email smoko@example.invalid",
		"git -C " + repo + " config user.name Smoko",
		"if ! git -C " + repo + " rev-parse --verify HEAD >/dev/null 2>&1; then",
		"  git -C " + repo + " checkout -B main",
		"  git -C " + repo + " commit --allow-empty -m initial",
		"fi",
	}, "\n")

	return runRequiredGitCommand(ctx, dc, containerID, command, timeout)
}

func createCommittedGitFile(ctx context.Context, dc dockerRunner, containerID, repoPath, filePath, content string, timeout time.Duration) error {
	fileRel, err := cleanGitFilePath(filePath)
	if err != nil {
		return fmt.Errorf("invalid git file path %q: %w", filePath, err)
	}
	if err := ensureGitRepository(ctx, dc, containerID, repoPath, timeout); err != nil {
		return err
	}

	tracked, err := gitFileIsTracked(ctx, dc, containerID, repoPath, fileRel, timeout)
	if err != nil {
		return err
	}
	if err := dc.WriteFile(ctx, containerID, path.Join(repoPath, fileRel), content); err != nil {
		return fmt.Errorf("write git file %q: %w", fileRel, err)
	}
	if err := runRequiredGitCommand(ctx, dc, containerID, "git -C "+docker.ShellQuote(repoPath)+" add -- "+docker.ShellQuote(fileRel), timeout); err != nil {
		return err
	}

	stdout, stderr, exitCode, err := execGitCommand(ctx, dc, containerID, "git -C "+docker.ShellQuote(repoPath)+" diff --cached --quiet -- "+docker.ShellQuote(fileRel), timeout)
	if err != nil {
		return fmt.Errorf("check staged git file %q: %w", fileRel, err)
	}
	if exitCode == 0 {
		return nil
	}
	if exitCode != 1 {
		return fmt.Errorf("%s", formatCommandFailure("git diff --cached --quiet -- "+fileRel, exitCode, stdout, stderr))
	}

	message := "Add " + fileRel
	if tracked {
		message = "Update " + fileRel
	}
	return runRequiredGitCommand(ctx, dc, containerID, "git -C "+docker.ShellQuote(repoPath)+" commit -m "+docker.ShellQuote(message)+" -- "+docker.ShellQuote(fileRel), timeout)
}

func createUntrackedGitFile(ctx context.Context, dc dockerRunner, containerID, repoPath, filePath, content string, timeout time.Duration) error {
	fileRel, err := cleanGitFilePath(filePath)
	if err != nil {
		return fmt.Errorf("invalid git file path %q: %w", filePath, err)
	}
	if err := ensureGitRepository(ctx, dc, containerID, repoPath, timeout); err != nil {
		return err
	}
	tracked, err := gitFileIsTracked(ctx, dc, containerID, repoPath, fileRel, timeout)
	if err != nil {
		return err
	}
	if tracked {
		return fmt.Errorf("untracked file step requires %q to be untracked", fileRel)
	}
	if err := dc.WriteFile(ctx, containerID, path.Join(repoPath, fileRel), content); err != nil {
		return fmt.Errorf("write git file %q: %w", fileRel, err)
	}
	return nil
}

func createModifiedGitFile(ctx context.Context, dc dockerRunner, containerID, repoPath, filePath, content string, timeout time.Duration) error {
	fileRel, err := cleanGitFilePath(filePath)
	if err != nil {
		return fmt.Errorf("invalid git file path %q: %w", filePath, err)
	}
	if err := requireGitRepository(ctx, dc, containerID, repoPath, timeout); err != nil {
		return err
	}
	tracked, err := gitFileIsTracked(ctx, dc, containerID, repoPath, fileRel, timeout)
	if err != nil {
		return err
	}
	if !tracked {
		return fmt.Errorf("modified file step requires tracked file %q", fileRel)
	}
	if err := dc.WriteFile(ctx, containerID, path.Join(repoPath, fileRel), content); err != nil {
		return fmt.Errorf("write git file %q: %w", fileRel, err)
	}
	return nil
}

func checkoutGitBranch(ctx context.Context, dc dockerRunner, containerID, repoPath, branch string, timeout time.Duration) error {
	if strings.TrimSpace(branch) == "" {
		return fmt.Errorf("git branch name is empty")
	}
	if err := ensureGitRepository(ctx, dc, containerID, repoPath, timeout); err != nil {
		return err
	}
	return runRequiredGitCommand(ctx, dc, containerID, "git -C "+docker.ShellQuote(repoPath)+" checkout -B "+docker.ShellQuote(branch), timeout)
}

func requireGitRepository(ctx context.Context, dc dockerRunner, containerID, repoPath string, timeout time.Duration) error {
	command := "git -C " + docker.ShellQuote(repoPath) + " rev-parse --is-inside-work-tree >/dev/null"
	stdout, stderr, exitCode, err := execGitCommand(ctx, dc, containerID, command, timeout)
	if err != nil {
		return fmt.Errorf("check git repository %q: %w", repoPath, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("git repository %q does not exist or is not initialized\n%s", repoPath, formatCommandFailure(command, exitCode, stdout, stderr))
	}
	return nil
}

func gitFileIsTracked(ctx context.Context, dc dockerRunner, containerID, repoPath, fileRel string, timeout time.Duration) (bool, error) {
	command := "git -C " + docker.ShellQuote(repoPath) + " ls-files --error-unmatch -- " + docker.ShellQuote(fileRel)
	stdout, stderr, exitCode, err := execGitCommand(ctx, dc, containerID, command, timeout)
	if err != nil {
		return false, fmt.Errorf("check tracked git file %q: %w", fileRel, err)
	}
	if exitCode == 0 {
		return true, nil
	}
	if exitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("%s", formatCommandFailure(command, exitCode, stdout, stderr))
}

func runRequiredGitCommand(ctx context.Context, dc dockerRunner, containerID, command string, timeout time.Duration) error {
	stdout, stderr, exitCode, err := execGitCommand(ctx, dc, containerID, command, timeout)
	if err != nil {
		return fmt.Errorf("run git setup command: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("%s", formatCommandFailure(command, exitCode, stdout, stderr))
	}
	return nil
}

func execGitCommand(ctx context.Context, dc dockerRunner, containerID, command string, timeout time.Duration) (string, string, int, error) {
	return dc.ExecCommand(ctx, containerID, docker.WorkDir(), requireGitCommand(command), "", timeout)
}

func requireGitCommand(command string) string {
	return "command -v git >/dev/null 2>&1 || { echo 'Git steps require git on PATH in the test image.' >&2; exit 127; };\n" + command
}

func cleanGitRepoPath(repoPath string) (string, error) {
	normalized := strings.ReplaceAll(repoPath, "\\", "/")
	if hasGitParentSegment(normalized) {
		return "", fmt.Errorf("path must not contain '..'")
	}
	return docker.WorkPath(normalized)
}

func cleanGitFilePath(filePath string) (string, error) {
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	if normalized == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.ContainsRune(normalized, '\x00') {
		return "", fmt.Errorf("path contains NUL byte")
	}
	if path.IsAbs(normalized) || isWindowsDrivePath(normalized) {
		return "", fmt.Errorf("path must be relative to the git repository")
	}
	if hasGitParentSegment(normalized) {
		return "", fmt.Errorf("path must not contain '..'")
	}

	cleaned := path.Clean(normalized)
	if cleaned == "." {
		return "", fmt.Errorf("path is empty")
	}
	return cleaned, nil
}

func hasGitParentSegment(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func isWindowsDrivePath(p string) bool {
	return len(p) >= 2 && p[1] == ':' && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z'))
}

var (
	reGitRepoExists    = regexp.MustCompile(`^a git repository "((?:[^"\\]|\\.)*)" exists$`)
	reGitCommittedFile = regexp.MustCompile(`^git repository "((?:[^"\\]|\\.)*)" has committed file "((?:[^"\\]|\\.)*)" with content:?$`)
	reGitUntrackedFile = regexp.MustCompile(`^git repository "((?:[^"\\]|\\.)*)" has untracked file "((?:[^"\\]|\\.)*)" with content:?$`)
	reGitModifiedFile  = regexp.MustCompile(`^git repository "((?:[^"\\]|\\.)*)" has modified file "((?:[^"\\]|\\.)*)" with content:?$`)
	reGitBranch        = regexp.MustCompile(`^git repository "((?:[^"\\]|\\.)*)" is on branch "((?:[^"\\]|\\.)*)"$`)
)

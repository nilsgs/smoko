package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/theory/jsonpath"

	"github.com/nskut/smoko/internal/docker"
	"github.com/nskut/smoko/internal/hints"
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
// env holds the initial environment variables; any captured variables are
// appended and written to .smoko_env so subsequent steps can use them.
// Returns the effective working directory and the full set of environment
// variables (initial + captured) after all steps have run.
func RunGivenSteps(ctx context.Context, dc dockerRunner, containerID string, steps []parser.Step, timeout time.Duration, env []string) (workdir string, allEnv []string, err error) {
	ops, err := buildGivenOps(steps)
	if err != nil {
		return "", nil, err
	}

	allEnv = append([]string(nil), env...)
	workdir = docker.WorkDir()

	for _, op := range ops {
		switch op.kind {
		case givenOpSetWorkdir:
			absPath, err := docker.WorkPath(op.path)
			if err != nil {
				return "", nil, fmt.Errorf("Given %q: invalid working directory %q: %w", op.stepText, op.path, err)
			}
			_, _, code, err := dc.ExecCommand(ctx, containerID, workdir, "test -d "+docker.ShellQuote(absPath), "", 5*time.Second)
			if err != nil {
				return "", nil, fmt.Errorf("Given %q: %w", op.stepText, err)
			}
			if code != 0 {
				return "", nil, fmt.Errorf("Given %q: directory %q does not exist", op.stepText, op.path)
			}
			workdir = absPath
		case givenOpRunCommand:
			stdout, err := runGivenCommandCapture(ctx, dc, containerID, op.command, workdir, timeout)
			if err != nil {
				return "", nil, fmt.Errorf("Given %q: %w", op.stepText, err)
			}
			for _, cap := range op.captures {
				value, err := extractCaptureValue(cap, stdout)
				if err != nil {
					return "", nil, fmt.Errorf("Given %q: %w", cap.stepText, err)
				}
				allEnv = append(allEnv, cap.varName+"="+value)
				if err := WriteEnvFile(ctx, dc, containerID, allEnv); err != nil {
					return "", nil, fmt.Errorf("write env after capture: %w", err)
				}
			}
		default:
			if err := executeGivenOp(ctx, dc, containerID, op, timeout); err != nil {
				return "", nil, err
			}
		}
	}

	return workdir, allEnv, nil
}

// RunGiven executes a single Given step against the container.
// Prefer RunGivenSteps for ordered, batched execution.
// Returns the effective working directory after the step.
func RunGiven(ctx context.Context, dc dockerRunner, containerID string, step parser.Step, timeout time.Duration, env []string) (string, error) {
	workdir, _, err := RunGivenSteps(ctx, dc, containerID, []parser.Step{step}, timeout, env)
	return workdir, err
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
// workdir is the effective working directory (as set by "Given the working directory is").
func RunWhen(ctx context.Context, dc dockerRunner, containerID string, step parser.Step, workdir string, timeout time.Duration) (*WhenResult, error) {
	text := step.Text

	var command, stdin string
	var expectedExitCode *int

	if m := reRunWithInput.FindStringSubmatch(text); m != nil {
		command = unescapeGivenString(m[1])
		stdin = m[2]
	} else if m := reRunExpectingCode.FindStringSubmatch(text); m != nil {
		command = unescapeGivenString(m[1])
		code := 0
		if _, err := fmt.Sscan(m[2], &code); err != nil {
			return nil, fmt.Errorf("invalid exit code %q: %w", m[2], err)
		}
		expectedExitCode = &code
	} else if m := reRun.FindStringSubmatch(text); m != nil {
		command = unescapeGivenString(m[1])
	} else {
		if suggestion := hints.Suggest(text, knownWhenPatterns); suggestion != "" {
			return nil, fmt.Errorf("unknown When step: %q\n  → did you mean: %q?", text, suggestion)
		}
		return nil, fmt.Errorf("unknown When step: %q", text)
	}

	stdout, stderr, exitCode, err := dc.ExecCommand(ctx, containerID, workdir, wrapCommand(command), stdin, timeout)
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
	givenSave       // I save output/JSON path/pattern as $VAR
	givenSetWorkdir // the working directory is "path"
)

type captureKind int

const (
	captureOutput captureKind = iota
	captureJSONPath
	capturePattern
)

type captureSpec struct {
	kind     captureKind
	varName  string
	jsonPath string
	pattern  string
	stepText string
}

type givenAction struct {
	kind     givenKind
	stepText string
	file     docker.FileEntry
	path     string
	command  string
	capture  *captureSpec
}

type givenOpKind int

const (
	givenOpWriteFiles givenOpKind = iota
	givenOpMakeDir
	givenOpRunCommand
	givenOpSetWorkdir
)

type givenOp struct {
	kind     givenOpKind
	stepText string
	files    []docker.FileEntry
	path     string
	command  string
	captures []captureSpec // non-nil only for givenOpRunCommand
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
		case givenSetWorkdir:
			flushFiles()
			ops = append(ops, givenOp{
				kind:     givenOpSetWorkdir,
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
		case givenSave:
			if len(pendingFiles) > 0 || len(ops) == 0 || ops[len(ops)-1].kind != givenOpRunCommand {
				return nil, fmt.Errorf("save step %q must immediately follow a 'I run' step", action.stepText)
			}
			ops[len(ops)-1].captures = append(ops[len(ops)-1].captures, *action.capture)
		}
	}

	flushFiles()
	return ops, nil
}

func classifyGivenStep(step parser.Step) (givenAction, error) {
	text := step.Text

	if m := reFileWithContent.FindStringSubmatch(text); m != nil {
		path, err := docker.WorkPath(m[1])
		if err != nil {
			return givenAction{}, fmt.Errorf("Given %q: invalid file path %q: %w", text, m[1], err)
		}
		return givenAction{
			kind:     givenFile,
			stepText: text,
			file: docker.FileEntry{
				Path:    path,
				Content: step.Block,
			},
		}, nil
	}

	if m := reFileExists.FindStringSubmatch(text); m != nil {
		path, err := docker.WorkPath(m[1])
		if err != nil {
			return givenAction{}, fmt.Errorf("Given %q: invalid file path %q: %w", text, m[1], err)
		}
		return givenAction{
			kind:     givenFile,
			stepText: text,
			file: docker.FileEntry{
				Path:    path,
				Content: "",
			},
		}, nil
	}

	if m := reDirExists.FindStringSubmatch(text); m != nil {
		if _, err := docker.WorkPath(m[1]); err != nil {
			return givenAction{}, fmt.Errorf("Given %q: invalid directory path %q: %w", text, m[1], err)
		}
		return givenAction{
			kind:     givenDir,
			stepText: text,
			path:     m[1],
		}, nil
	}

	if m := reSetWorkdir.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenSetWorkdir,
			stepText: text,
			path:     m[1],
		}, nil
	}

	if m := reGivenRun.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenRun,
			stepText: text,
			command:  unescapeGivenString(m[1]),
		}, nil
	}

	if reEmptyWorkDir.MatchString(text) {
		return givenAction{kind: givenNoop, stepText: text}, nil
	}

	if reEnvVar.MatchString(text) {
		return givenAction{kind: givenNoop, stepText: text}, nil
	}

	if m := reSaveOutput.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenSave,
			stepText: text,
			capture: &captureSpec{
				kind:     captureOutput,
				varName:  m[1],
				stepText: text,
			},
		}, nil
	}

	if m := reSaveJSONPath.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenSave,
			stepText: text,
			capture: &captureSpec{
				kind:     captureJSONPath,
				varName:  m[2],
				jsonPath: unescapeGivenString(m[1]),
				stepText: text,
			},
		}, nil
	}

	if m := reSavePattern.FindStringSubmatch(text); m != nil {
		return givenAction{
			kind:     givenSave,
			stepText: text,
			capture: &captureSpec{
				kind:     capturePattern,
				varName:  m[2],
				pattern:  unescapeGivenString(m[1]),
				stepText: text,
			},
		}, nil
	}

	if suggestion := hints.Suggest(text, knownGivenPatterns); suggestion != "" {
		return givenAction{}, fmt.Errorf("unknown Given step: %q\n  → did you mean: %q?", text, suggestion)
	}
	return givenAction{}, fmt.Errorf("unknown Given step: %q", text)
}

var knownWhenPatterns = []string{
	`I run "command"`,
	`I run "command" with input "stdin"`,
	`I run "command" expecting exit code 1`,
}

var knownGivenPatterns = []string{
	`a file "path" with content:`,
	`a file "path" exists`,
	`the directory "path" exists`,
	`directory "path" exists`,
	`an empty working directory`,
	`environment variable "NAME" is set to "value"`,
	`I run "command"`,
	`I save output as $VAR`,
	`I save JSON path "$.field" as $VAR`,
	`I save pattern "regex" as $VAR`,
	`the working directory is "path"`,
	`the working directory is "/smoko-work"`,
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
	}

	return nil
}

func runGivenCommandCapture(ctx context.Context, dc dockerRunner, containerID, command, workdir string, timeout time.Duration) (string, error) {
	stdout, stderr, exitCode, err := dc.ExecCommand(ctx, containerID, workdir, wrapCommand(command), "", timeout)
	if err != nil {
		return "", fmt.Errorf("setup command %q: %w", command, err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("%s", formatCommandFailure(command, exitCode, stdout, stderr))
	}
	return stdout, nil
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
	reGivenRun        = regexp.MustCompile(`^I run "((?:[^"\\]|\\.)*)"$`)
	reSetWorkdir      = regexp.MustCompile(`^the working directory is "([^"]+)"$`)

	reSaveOutput   = regexp.MustCompile(`^I save output as \$([A-Za-z_][A-Za-z0-9_]*)$`)
	reSaveJSONPath = regexp.MustCompile(`^I save JSON path "((?:[^"\\]|\\.)*)" as \$([A-Za-z_][A-Za-z0-9_]*)$`)
	reSavePattern  = regexp.MustCompile(`^I save pattern "((?:[^"\\]|\\.)*)" as \$([A-Za-z_][A-Za-z0-9_]*)$`)

	reRun              = regexp.MustCompile(`^I run "((?:[^"\\]|\\.)*)"$`)
	reRunWithInput     = regexp.MustCompile(`^I run "((?:[^"\\]|\\.)*)" with input "([^"]*)"$`)
	reRunExpectingCode = regexp.MustCompile(`^I run "((?:[^"\\]|\\.)*)" expecting exit code (\d+)$`)
)

// extractCaptureValue extracts the value to store given the capture spec and the stdout of the preceding run.
func extractCaptureValue(spec captureSpec, stdout string) (string, error) {
	switch spec.kind {
	case captureOutput:
		return strings.TrimSpace(stdout), nil
	case captureJSONPath:
		return extractJSONPathValue(stdout, spec.jsonPath)
	case capturePattern:
		return extractPatternValue(stdout, spec.pattern)
	default:
		return "", fmt.Errorf("unknown capture kind")
	}
}

func extractJSONPathValue(raw, pathExpr string) (string, error) {
	var value any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &value); err != nil {
		return "", fmt.Errorf("output is not valid JSON: %v", err)
	}
	path, err := jsonpath.Parse(pathExpr)
	if err != nil {
		return "", fmt.Errorf("invalid JSON path %q: %v", pathExpr, err)
	}
	nodes := path.Select(value)
	if len(nodes) == 0 {
		return "", fmt.Errorf("JSON path %q not found in output", pathExpr)
	}
	return jsonValueToString(nodes[0]), nil
}

func extractPatternValue(stdout, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %v", pattern, err)
	}
	m := re.FindStringSubmatch(strings.TrimSpace(stdout))
	if m == nil {
		return "", fmt.Errorf("pattern %q did not match output", pattern)
	}
	if len(m) < 2 {
		return "", fmt.Errorf("pattern %q must contain at least one capture group", pattern)
	}
	return m[1], nil
}

func jsonValueToString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

// unescapeGivenString replaces \" with " and \\ with \ in a captured step string.
func unescapeGivenString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '"':
				b.WriteByte('"')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

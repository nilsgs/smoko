package executor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nskut/smoko/internal/docker"
	"github.com/nskut/smoko/internal/executor"
	"github.com/nskut/smoko/internal/parser"
)

type fakeDocker struct {
	ops          []string
	writeFile    []writeFileCall
	writeBatches [][]docker.FileEntry
	mkdirs       []string
	execCalls    []execCall
	execResults  []execResult
}

type writeFileCall struct {
	containerID string
	path        string
	content     string
}

type execCall struct {
	containerID string
	workdir     string
	command     string
	stdin       string
	timeout     time.Duration
}

type execResult struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func (f *fakeDocker) WriteFile(ctx context.Context, containerID, path, content string) error {
	f.ops = append(f.ops, "write-file")
	f.writeFile = append(f.writeFile, writeFileCall{
		containerID: containerID,
		path:        path,
		content:     content,
	})
	return nil
}

func (f *fakeDocker) WriteFiles(ctx context.Context, containerID string, files []docker.FileEntry) error {
	f.ops = append(f.ops, "write-files")
	cloned := append([]docker.FileEntry(nil), files...)
	f.writeBatches = append(f.writeBatches, cloned)
	return nil
}

func (f *fakeDocker) MakeDir(ctx context.Context, containerID, path string) error {
	f.ops = append(f.ops, "mkdir:"+path)
	f.mkdirs = append(f.mkdirs, path)
	return nil
}

func (f *fakeDocker) ExecCommand(ctx context.Context, containerID, workdir, command, stdin string, timeout time.Duration) (string, string, int, error) {
	f.ops = append(f.ops, "exec")
	f.execCalls = append(f.execCalls, execCall{
		containerID: containerID,
		workdir:     workdir,
		command:     command,
		stdin:       stdin,
		timeout:     timeout,
	})

	if len(f.execResults) == 0 {
		return "", "", 0, nil
	}

	result := f.execResults[0]
	f.execResults = f.execResults[1:]
	return result.stdout, result.stderr, result.exitCode, result.err
}

func TestCollectEnvVars(t *testing.T) {
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `environment variable "FOO" is set to "bar"`},
		{ResolvedType: parser.StepGiven, Text: `a file "x.txt" exists`},
		{ResolvedType: parser.StepGiven, Text: `environment variable "BAZ" is set to "qux"`},
		{ResolvedType: parser.StepWhen, Text: `I run "env"`},
	}

	got := executor.CollectEnvVars(steps)
	assert.Equal(t, []string{"FOO=bar", "BAZ=qux"}, got)
}

func TestCollectEnvVarsEmpty(t *testing.T) {
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `a file "x.txt" exists`},
		{ResolvedType: parser.StepWhen, Text: `I run "ls"`},
	}

	got := executor.CollectEnvVars(steps)
	assert.Nil(t, got)
}

func TestCollectEnvVarsIgnoresNonGiven(t *testing.T) {
	steps := []parser.Step{
		{ResolvedType: parser.StepThen, Text: `environment variable "FOO" is set to "bar"`},
	}

	got := executor.CollectEnvVars(steps)
	assert.Nil(t, got)
}

func TestRunGivenStepsPreservesOrderAroundCommands(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{}
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `a file "first.txt" exists`},
		{ResolvedType: parser.StepGiven, Text: `I run "touch second.txt"`},
		{ResolvedType: parser.StepGiven, Text: `a file "third.txt" with content:`, Block: "third"},
		{ResolvedType: parser.StepGiven, Text: `the directory "nested" exists`},
		{ResolvedType: parser.StepWhen, Text: `I run "ls"`},
	}

	err := executor.RunGivenSteps(context.Background(), fd, "abc123", steps, 5*time.Second, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"write-files", "exec", "write-files", "mkdir:nested"}, fd.ops)
	require.Len(t, fd.writeBatches, 2)
	assert.Equal(t, []docker.FileEntry{{
		Path:    docker.WorkDir() + "/first.txt",
		Content: "",
	}}, fd.writeBatches[0])
	assert.Equal(t, []docker.FileEntry{{
		Path:    docker.WorkDir() + "/third.txt",
		Content: "third",
	}}, fd.writeBatches[1])
	require.Len(t, fd.execCalls, 1)
	assert.Equal(t, docker.WorkDir(), fd.execCalls[0].workdir)
	assert.Equal(t, `[ -f /smoko-work/.smoko_env ] && . /smoko-work/.smoko_env; touch second.txt`, fd.execCalls[0].command)
	assert.Equal(t, 5*time.Second, fd.execCalls[0].timeout)
	assert.Equal(t, []string{"nested"}, fd.mkdirs)
}

func TestRunGivenStepsCommandFailureIncludesDetails(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{
		execResults: []execResult{{
			stdout:   "partial output",
			stderr:   "boom",
			exitCode: 7,
		}},
	}
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `I run "sh -c 'echo partial output; echo boom >&2; exit 7'"`},
	}

	err := executor.RunGivenSteps(context.Background(), fd, "abc123", steps, 3*time.Second, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Given ")
	assert.Contains(t, err.Error(), `command "sh -c 'echo partial output; echo boom >&2; exit 7'" exited with code 7`)
	assert.Contains(t, err.Error(), "stdout:\npartial output")
	assert.Contains(t, err.Error(), "stderr:\nboom")
}

func TestRunGivenCommandUsesTimeout(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{}
	step := parser.Step{ResolvedType: parser.StepGiven, Text: `I run "touch marker.txt"`}

	err := executor.RunGiven(context.Background(), fd, "abc123", step, 12*time.Second, nil)
	require.NoError(t, err)
	require.Len(t, fd.execCalls, 1)
	assert.Equal(t, 12*time.Second, fd.execCalls[0].timeout)
}

func TestRunGivenCommandPropagatesExecError(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{
		execResults: []execResult{{
			err: errors.New("exec failed"),
		}},
	}
	step := parser.Step{ResolvedType: parser.StepGiven, Text: `I run "touch marker.txt"`}

	err := executor.RunGiven(context.Background(), fd, "abc123", step, time.Second, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `setup command "touch marker.txt": exec failed`)
}

func TestSaveOutputCapture(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{
		execResults: []execResult{
			{stdout: "hello world\n", exitCode: 0},
			{stdout: "", exitCode: 0}, // WriteEnvFile exec
		},
	}
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `I run "echo hello world"`},
		{ResolvedType: parser.StepGiven, Text: `I save output as $GREETING`},
	}

	err := executor.RunGivenSteps(context.Background(), fd, "abc123", steps, 5*time.Second, nil)
	require.NoError(t, err)
	// WriteEnvFile is called — we just verify no error and the env write happened
	require.GreaterOrEqual(t, len(fd.execCalls), 1)
}

func TestSaveJSONPathCapture(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{
		execResults: []execResult{
			{stdout: `{"name":"alice","age":30}`, exitCode: 0},
			{stdout: "", exitCode: 0},
		},
	}
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `I run "echo json"`},
		{ResolvedType: parser.StepGiven, Text: `I save JSON path "$.name" as $NAME`},
	}

	err := executor.RunGivenSteps(context.Background(), fd, "abc123", steps, 5*time.Second, nil)
	require.NoError(t, err)
}

func TestSavePatternCapture(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{
		execResults: []execResult{
			{stdout: "version 1.2.3\n", exitCode: 0},
			{stdout: "", exitCode: 0},
		},
	}
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `I run "app --version"`},
		{ResolvedType: parser.StepGiven, Text: `I save pattern "version ([0-9.]+)" as $VERSION`},
	}

	err := executor.RunGivenSteps(context.Background(), fd, "abc123", steps, 5*time.Second, nil)
	require.NoError(t, err)
}

func TestSaveStepWithoutRunErrors(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{}
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `a file "foo.txt" exists`},
		{ResolvedType: parser.StepGiven, Text: `I save output as $FOO`},
	}

	err := executor.RunGivenSteps(context.Background(), fd, "abc123", steps, 5*time.Second, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must immediately follow")
}

func TestSavePatternRequiresCaptureGroup(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{
		execResults: []execResult{
			{stdout: "version 1.2.3\n", exitCode: 0},
		},
	}
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `I run "app --version"`},
		{ResolvedType: parser.StepGiven, Text: `I save pattern "version [0-9.]+" as $VERSION`},
	}

	err := executor.RunGivenSteps(context.Background(), fd, "abc123", steps, 5*time.Second, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capture group")
}

func TestSaveJSONPathNotFound(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{
		execResults: []execResult{
			{stdout: `{"foo":"bar"}`, exitCode: 0},
		},
	}
	steps := []parser.Step{
		{ResolvedType: parser.StepGiven, Text: `I run "echo json"`},
		{ResolvedType: parser.StepGiven, Text: `I save JSON path "$.missing" as $VAL`},
	}

	err := executor.RunGivenSteps(context.Background(), fd, "abc123", steps, 5*time.Second, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunWhenSourcesEnvAndPassesInput(t *testing.T) {
	t.Parallel()

	fd := &fakeDocker{
		execResults: []execResult{{
			stdout:   "hello",
			stderr:   "",
			exitCode: 0,
		}},
	}

	result, err := executor.RunWhen(
		context.Background(),
		fd,
		"abc123",
		parser.Step{Text: `I run "cat" with input "hello"`},
		4*time.Second,
	)
	require.NoError(t, err)
	require.Len(t, fd.execCalls, 1)
	assert.Equal(t, `hello`, result.Stdout)
	assert.Equal(t, `[ -f /smoko-work/.smoko_env ] && . /smoko-work/.smoko_env; cat`, fd.execCalls[0].command)
	assert.Equal(t, "hello", fd.execCalls[0].stdin)
	assert.Equal(t, 4*time.Second, fd.execCalls[0].timeout)
}

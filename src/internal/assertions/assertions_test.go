package assertions_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nskut/smoko/internal/assertions"
	"github.com/nskut/smoko/internal/docker"
	"github.com/nskut/smoko/internal/executor"
	"github.com/nskut/smoko/internal/parser"
)

type fakeDocker struct {
	fileExistsResult   bool
	dirExistsResult    bool
	readFileContent    string
	readFileErr        error
	batchResults       []docker.FSResult
	batchErr           error
	batchChecks        []docker.FSCheck
	lastFileExistsPath string
	execCalls          []execCall
	execResults        []execResult
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

func (f *fakeDocker) FileExists(ctx context.Context, containerID, path string) (bool, error) {
	f.lastFileExistsPath = path
	return f.fileExistsResult, nil
}

func (f *fakeDocker) DirExists(ctx context.Context, containerID, path string) (bool, error) {
	return f.dirExistsResult, nil
}

func (f *fakeDocker) ReadFile(ctx context.Context, containerID, path string) (string, error) {
	return f.readFileContent, f.readFileErr
}

func (f *fakeDocker) BatchFSCheck(ctx context.Context, containerID string, checks []docker.FSCheck) ([]docker.FSResult, error) {
	f.batchChecks = append([]docker.FSCheck(nil), checks...)
	return f.batchResults, f.batchErr
}

func (f *fakeDocker) ExecCommand(ctx context.Context, containerID, workdir, command, stdin string, timeout time.Duration) (string, string, int, error) {
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

func newWhenResult(stdout, stderr string, exitCode int) *executor.WhenResult {
	return &executor.WhenResult{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	}
}

func TestExitCodeIs(t *testing.T) {
	wr := newWhenResult("", "", 0)
	step := parser.Step{Text: "exit code is 0"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestExitCodeIsFailure(t *testing.T) {
	wr := newWhenResult("", "", 1)
	step := parser.Step{Text: "exit code is 0"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "exit code")
}

func TestExitCodeIsNot(t *testing.T) {
	wr := newWhenResult("", "", 1)
	step := parser.Step{Text: "exit code is not 0"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputContains(t *testing.T) {
	wr := newWhenResult("hello world", "", 0)
	step := parser.Step{Text: `output contains "hello"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputDoesNotContain(t *testing.T) {
	wr := newWhenResult("hello world", "", 0)
	step := parser.Step{Text: `output does not contain "error"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputDoesNotContainFailure(t *testing.T) {
	wr := newWhenResult("error: something failed", "", 0)
	step := parser.Step{Text: `output does not contain "error"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
}

func TestStdoutContains(t *testing.T) {
	wr := newWhenResult("only stdout", "only stderr", 0)
	step := parser.Step{Text: `stdout contains "only stdout"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestStderrContains(t *testing.T) {
	wr := newWhenResult("only stdout", "only stderr", 0)
	step := parser.Step{Text: `stderr contains "only stderr"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputMatchesPattern(t *testing.T) {
	wr := newWhenResult("version 1.2.3", "", 0)
	step := parser.Step{Text: `output matches pattern "version \d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestStdoutMatchesPattern(t *testing.T) {
	wr := newWhenResult("version 1.2.3", "stderr text", 0)
	step := parser.Step{Text: `stdout matches pattern "version \d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestStderrMatchesPattern(t *testing.T) {
	wr := newWhenResult("stdout text", "error: boom", 0)
	step := parser.Step{Text: `stderr matches pattern "error: \w+"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestStderrDoesNotMatchPattern(t *testing.T) {
	wr := newWhenResult("stdout text", "all fine", 0)
	step := parser.Step{Text: `stderr does not match pattern "panic:"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestStderrDoesNotMatchPatternFailure(t *testing.T) {
	wr := newWhenResult("stdout text", "panic: nil pointer", 0)
	step := parser.Step{Text: `stderr does not match pattern "panic:"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
}

func TestOutputMatchesPatternFailure(t *testing.T) {
	wr := newWhenResult("no version here", "", 0)
	step := parser.Step{Text: `output matches pattern "version \d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
}

func TestOutputMatchesInvalidRegex(t *testing.T) {
	wr := newWhenResult("anything", "", 0)
	step := parser.Step{Text: `output matches pattern "[invalid"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "invalid regex")
}

func TestOutputEquals(t *testing.T) {
	wr := newWhenResult("hello world\n", "", 0)
	step := parser.Step{Text: `output equals "hello world"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputDoesNotEqual(t *testing.T) {
	wr := newWhenResult("hello world", "", 0)
	step := parser.Step{Text: `output does not equal "goodbye"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputEqualsFailure(t *testing.T) {
	wr := newWhenResult("hello world", "", 0)
	step := parser.Step{Text: `output equals "goodbye"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "does not equal")
}

func TestStdoutEquals(t *testing.T) {
	wr := newWhenResult("hello\n", "some error", 0)
	step := parser.Step{Text: `stdout equals "hello"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestStderrEquals(t *testing.T) {
	wr := newWhenResult("hello", "error msg\n", 0)
	step := parser.Step{Text: `stderr equals "error msg"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputIsEmpty(t *testing.T) {
	wr := newWhenResult("", "", 0)
	step := parser.Step{Text: "output is empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputIsNotEmpty(t *testing.T) {
	wr := newWhenResult("something", "", 0)
	step := parser.Step{Text: "output is not empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputIsEmptyFailure(t *testing.T) {
	wr := newWhenResult("not empty", "", 0)
	step := parser.Step{Text: "output is empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
}

func TestOutputIsNotEmptyFailure(t *testing.T) {
	wr := newWhenResult("", "", 0)
	step := parser.Step{Text: "output is not empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
}

func TestStderrIsEmpty(t *testing.T) {
	wr := newWhenResult("stdout stuff", "", 0)
	step := parser.Step{Text: "stderr is empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestFileMatchesPattern(t *testing.T) {
	fd := &fakeDocker{readFileContent: "v1.2.3\n"}
	step := parser.Step{Text: `file "VERSION" matches pattern "v\d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, r.Pass)
}

func TestFileDoesNotMatchPattern(t *testing.T) {
	fd := &fakeDocker{readFileContent: "release notes here\n"}
	step := parser.Step{Text: `file "notes.txt" does not match pattern "^ERROR"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, r.Pass)
}

func TestFileMatchesPatternFailure(t *testing.T) {
	fd := &fakeDocker{readFileContent: "no version\n"}
	step := parser.Step{Text: `file "VERSION" matches pattern "v\d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "does not match pattern")
}

func TestFileEquals(t *testing.T) {
	fd := &fakeDocker{readFileContent: "1.3.0\n"}
	step := parser.Step{Text: `file "VERSION" equals "1.3.0"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, r.Pass)
}

func TestFileDoesNotEqual(t *testing.T) {
	fd := &fakeDocker{readFileContent: "1.3.0\n"}
	step := parser.Step{Text: `file "VERSION" does not equal "2.0.0"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, r.Pass)
}

func TestFileEqualsFailure(t *testing.T) {
	fd := &fakeDocker{readFileContent: "1.3.0\n"}
	step := parser.Step{Text: `file "VERSION" equals "2.0.0"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "does not equal")
}

func TestFileIsEmpty(t *testing.T) {
	fd := &fakeDocker{readFileContent: ""}
	step := parser.Step{Text: `file "empty.txt" is empty`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, r.Pass)
}

func TestFileIsNotEmpty(t *testing.T) {
	fd := &fakeDocker{readFileContent: "some content\n"}
	step := parser.Step{Text: `file "data.txt" is not empty`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, r.Pass)
}

func TestFileIsEmptyFailure(t *testing.T) {
	fd := &fakeDocker{readFileContent: "not empty"}
	step := parser.Step{Text: `file "data.txt" is empty`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.False(t, r.Pass)
}

func TestFileIsNotEmptyFailure(t *testing.T) {
	fd := &fakeDocker{readFileContent: ""}
	step := parser.Step{Text: `file "data.txt" is not empty`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.False(t, r.Pass)
}

func TestEvaluateAllBatchesNewFileAssertions(t *testing.T) {
	fd := &fakeDocker{
		batchResults: []docker.FSResult{
			{Content: "v1.2.3\n"},
			{Content: "1.3.0\n"},
			{Content: ""},
			{Content: "has data"},
		},
	}
	steps := []parser.Step{
		{ResolvedType: parser.StepThen, Text: `file "VERSION" matches pattern "v\d+\.\d+\.\d+"`},
		{ResolvedType: parser.StepThen, Text: `file "VERSION" equals "1.3.0"`},
		{ResolvedType: parser.StepThen, Text: `file "empty.txt" is empty`},
		{ResolvedType: parser.StepThen, Text: `file "data.txt" is not empty`},
	}
	results := assertions.EvaluateAll(context.Background(), steps, newWhenResult("", "", 0), fd, "cid", nil)
	require.Len(t, fd.batchChecks, 4)
	assert.True(t, results[0].Pass)
	assert.True(t, results[1].Pass)
	assert.True(t, results[2].Pass)
	assert.True(t, results[3].Pass)
}

func TestUnknownAssertion(t *testing.T) {
	wr := newWhenResult("", "", 0)
	step := parser.Step{Text: "totally unknown assertion"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "unknown Then assertion")
}

func TestOutputContainsRejectsTrailingText(t *testing.T) {
	wr := newWhenResult("hello world", "", 0)
	step := parser.Step{Text: `output contains "hello" eventually`}

	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)

	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "unknown Then assertion")
}

func TestOutputMatchesRejectsTrailingText(t *testing.T) {
	wr := newWhenResult("version 1.2.3", "", 0)
	step := parser.Step{Text: `output matches pattern "version" eventually`}

	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)

	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "unknown Then assertion")
}

func TestDirectoryExistsRejectsTrailingText(t *testing.T) {
	fd := &fakeDocker{dirExistsResult: true}
	step := parser.Step{Text: `directory "out" exists eventually`}

	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)

	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "unknown Then assertion")
}

func TestFileContainsRejectsTrailingText(t *testing.T) {
	fd := &fakeDocker{readFileContent: "hello world"}
	step := parser.Step{Text: `file "out.txt" contains "hello" eventually`}

	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)

	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "unknown Then assertion")
}

func TestOutputContainsEscapedQuote(t *testing.T) {
	wr := newWhenResult(`"name": "value"`, "", 0)
	step := parser.Step{Text: `output contains "\"name\": \"value\""`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputDoesNotContainEscapedQuote(t *testing.T) {
	wr := newWhenResult(`no json here`, "", 0)
	step := parser.Step{Text: `output does not contain "\"name\": \"value\""`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputContainsEscapedQuoteFailure(t *testing.T) {
	wr := newWhenResult(`no json here`, "", 0)
	step := parser.Step{Text: `output contains "\"name\": \"value\""`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
}

func TestOutputMatchesEscapedQuote(t *testing.T) {
	wr := newWhenResult(`{"ok":true}`, "", 0)
	step := parser.Step{Text: `output matches pattern "\{\"ok\":true\}"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputJSONPathExists(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Alice"}}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.user.name" exists`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestStdoutJSONPathEqualsString(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Alice"}}`, "", 0)
	step := parser.Step{Text: `stdout as JSON at path "$.user.name" equals "Alice"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestStderrJSONPathEqualsBoolean(t *testing.T) {
	wr := newWhenResult("", `{"ok":true}`, 0)
	step := parser.Step{Text: `stderr as JSON at path "$.ok" equals true`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputJSONPathEqualsNumber(t *testing.T) {
	wr := newWhenResult(`{"count":3}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.count" equals 3`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputJSONPathEqualsNull(t *testing.T) {
	wr := newWhenResult(`{"value":null}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.value" equals null`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputJSONPathEqualsBlockObject(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Alice","roles":["admin"]}}`, "", 0)
	step := parser.Step{
		Text:  `output as JSON at path "$.user" equals:`,
		Block: "{\n  \"name\": \"Alice\",\n  \"roles\": [\"admin\"]\n}",
	}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.True(t, r.Pass)
}

func TestOutputJSONPathInvalidJSON(t *testing.T) {
	wr := newWhenResult(`not json`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.user.name" exists`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "output is not valid JSON")
}

func TestOutputJSONPathInvalidPath(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Alice"}}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.user[" exists`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "invalid JSONPath")
}

func TestOutputJSONPathEqualsRequiresSingleMatch(t *testing.T) {
	wr := newWhenResult(`{"items":[{"id":1},{"id":2}]}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.items[*].id" equals 1`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "matched 2 values")
}

func TestOutputJSONPathEqualsFailureShowsExpectedAndActual(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Bob"}}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.user.name" equals "Alice"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "", nil)
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, `expected "Alice", got "Bob"`)
}

func TestFileJSONPathExists(t *testing.T) {
	fd := &fakeDocker{readFileContent: `{"meta":{"ok":true}}`}
	step := parser.Step{Text: `file "result.json" as JSON at path "$.meta.ok" exists`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, r.Pass)
}

func TestFileJSONPathEqualsArrayBlock(t *testing.T) {
	fd := &fakeDocker{readFileContent: `{"items":[1,2,3]}`}
	step := parser.Step{
		Text:  `file "result.json" as JSON at path "$.items" equals:`,
		Block: "[1, 2, 3]",
	}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, r.Pass)
}

func TestEvaluateAllBatchesFileJSONAssertions(t *testing.T) {
	fd := &fakeDocker{
		batchResults: []docker.FSResult{
			{Exists: true, Content: `{"user":{"name":"Alice"}}`},
			{Exists: true, Content: `{"items":[1,2,3]}`},
		},
	}
	steps := []parser.Step{
		{ResolvedType: parser.StepThen, Text: `file "user.json" as JSON at path "$.user.name" equals "Alice"`},
		{ResolvedType: parser.StepThen, Text: `file "items.json" as JSON at path "$.items" equals:`, Block: "[1,2,3]"},
	}

	results := assertions.EvaluateAll(context.Background(), steps, newWhenResult("", "", 0), fd, "cid", nil)
	require.Len(t, fd.batchChecks, 2)
	assert.Equal(t, []docker.FSCheck{
		{Kind: docker.FSCheckReadFile, Path: "user.json"},
		{Kind: docker.FSCheckReadFile, Path: "items.json"},
	}, fd.batchChecks)
	assert.True(t, results[0].Pass)
	assert.True(t, results[1].Pass)
}

func TestEvaluateAllFallsBackWhenBatchFails(t *testing.T) {
	fd := &fakeDocker{
		batchErr:        errors.New("batch failed"),
		readFileContent: `{"user":{"name":"Alice"}}`,
	}
	steps := []parser.Step{
		{ResolvedType: parser.StepThen, Text: `file "user.json" as JSON at path "$.user.name" equals "Alice"`},
	}

	results := assertions.EvaluateAll(context.Background(), steps, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, results[0].Pass)
}

func TestFileDoesNotContain(t *testing.T) {
	fd := &fakeDocker{readFileContent: "hello world"}
	step := parser.Step{Text: `file "out.txt" does not contain "error"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.True(t, r.Pass)
}

func TestFileDoesNotContainFailure(t *testing.T) {
	fd := &fakeDocker{readFileContent: "error: something went wrong"}
	step := parser.Step{Text: `file "out.txt" does not contain "error"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)
	assert.False(t, r.Pass)
}

func TestFileExistsWithVarExpansion(t *testing.T) {
	fd := &fakeDocker{fileExistsResult: true}
	step := parser.Step{Text: `file "$DIR/output.txt" exists`}
	env := []string{"DIR=/smoko-work/mydir"}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", env)
	assert.True(t, r.Pass)
	// The path passed to Docker should have the variable expanded
	assert.Equal(t, "/smoko-work/mydir/output.txt", fd.lastFileExistsPath)
}

func TestFileContainsWithVarExpansion(t *testing.T) {
	fd := &fakeDocker{readFileContent: "hello"}
	step := parser.Step{Text: `file "$OUTFILE" contains "hello"`}
	env := []string{"OUTFILE=/smoko-work/result.txt"}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", env)
	assert.True(t, r.Pass)
}

func TestGitRepositoryIsDirtyWithVarExpansion(t *testing.T) {
	fd := &fakeDocker{
		execResults: []execResult{{stdout: " M README.md\n", exitCode: 0}},
	}
	step := parser.Step{Text: `git repository "$REPO" is dirty`}
	env := []string{"REPO=/smoko-work/repo"}

	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", env)

	require.True(t, r.Pass)
	require.Len(t, fd.execCalls, 1)
	assert.Contains(t, fd.execCalls[0].command, `command -v git`)
	assert.Contains(t, fd.execCalls[0].command, `git -C '/smoko-work/repo' status --porcelain`)
	assert.Equal(t, docker.WorkDir(), fd.execCalls[0].workdir)
}

func TestGitRepositoryCleanFailure(t *testing.T) {
	fd := &fakeDocker{
		execResults: []execResult{{stdout: "?? scratch.txt\n", exitCode: 0}},
	}
	step := parser.Step{Text: `git repository "repo" is clean`}

	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)

	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "is dirty")
	assert.Contains(t, r.Message, "?? scratch.txt")
}

func TestGitRepositoryHasBranch(t *testing.T) {
	fd := &fakeDocker{
		execResults: []execResult{{exitCode: 0}},
	}
	step := parser.Step{Text: `git repository "repo" has branch "feature/test"`}

	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)

	require.True(t, r.Pass)
	require.Len(t, fd.execCalls, 1)
	assert.Contains(t, fd.execCalls[0].command, `show-ref --verify --quiet 'refs/heads/feature/test'`)
}

func TestGitRepositoryMissingBranch(t *testing.T) {
	fd := &fakeDocker{
		execResults: []execResult{{exitCode: 1}},
	}
	step := parser.Step{Text: `git repository "repo" has branch "feature/test"`}

	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid", nil)

	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, `does not have branch "feature/test"`)
}

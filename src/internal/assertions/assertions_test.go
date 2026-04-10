package assertions_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nskut/smoko/internal/assertions"
	"github.com/nskut/smoko/internal/docker"
	"github.com/nskut/smoko/internal/executor"
	"github.com/nskut/smoko/internal/parser"
)

type fakeDocker struct {
	fileExistsResult bool
	dirExistsResult  bool
	readFileContent  string
	readFileErr      error
	batchResults     []docker.FSResult
	batchErr         error
	batchChecks      []docker.FSCheck
}

func (f *fakeDocker) FileExists(ctx context.Context, containerID, path string) (bool, error) {
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
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestExitCodeIsFailure(t *testing.T) {
	wr := newWhenResult("", "", 1)
	step := parser.Step{Text: "exit code is 0"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "exit code")
}

func TestExitCodeIsNot(t *testing.T) {
	wr := newWhenResult("", "", 1)
	step := parser.Step{Text: "exit code is not 0"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputContains(t *testing.T) {
	wr := newWhenResult("hello world", "", 0)
	step := parser.Step{Text: `output contains "hello"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputDoesNotContain(t *testing.T) {
	wr := newWhenResult("hello world", "", 0)
	step := parser.Step{Text: `output does not contain "error"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputDoesNotContainFailure(t *testing.T) {
	wr := newWhenResult("error: something failed", "", 0)
	step := parser.Step{Text: `output does not contain "error"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
}

func TestStdoutContains(t *testing.T) {
	wr := newWhenResult("only stdout", "only stderr", 0)
	step := parser.Step{Text: `stdout contains "only stdout"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestStderrContains(t *testing.T) {
	wr := newWhenResult("only stdout", "only stderr", 0)
	step := parser.Step{Text: `stderr contains "only stderr"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputMatchesPattern(t *testing.T) {
	wr := newWhenResult("version 1.2.3", "", 0)
	step := parser.Step{Text: `output matches pattern "version \d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestStdoutMatchesPattern(t *testing.T) {
	wr := newWhenResult("version 1.2.3", "stderr text", 0)
	step := parser.Step{Text: `stdout matches pattern "version \d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestStderrMatchesPattern(t *testing.T) {
	wr := newWhenResult("stdout text", "error: boom", 0)
	step := parser.Step{Text: `stderr matches pattern "error: \w+"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestStderrDoesNotMatchPattern(t *testing.T) {
	wr := newWhenResult("stdout text", "all fine", 0)
	step := parser.Step{Text: `stderr does not match pattern "panic:"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestStderrDoesNotMatchPatternFailure(t *testing.T) {
	wr := newWhenResult("stdout text", "panic: nil pointer", 0)
	step := parser.Step{Text: `stderr does not match pattern "panic:"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
}

func TestOutputMatchesPatternFailure(t *testing.T) {
	wr := newWhenResult("no version here", "", 0)
	step := parser.Step{Text: `output matches pattern "version \d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
}

func TestOutputMatchesInvalidRegex(t *testing.T) {
	wr := newWhenResult("anything", "", 0)
	step := parser.Step{Text: `output matches pattern "[invalid"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "invalid regex")
}

func TestOutputEquals(t *testing.T) {
	wr := newWhenResult("hello world\n", "", 0)
	step := parser.Step{Text: `output equals "hello world"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputDoesNotEqual(t *testing.T) {
	wr := newWhenResult("hello world", "", 0)
	step := parser.Step{Text: `output does not equal "goodbye"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputEqualsFailure(t *testing.T) {
	wr := newWhenResult("hello world", "", 0)
	step := parser.Step{Text: `output equals "goodbye"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "does not equal")
}

func TestStdoutEquals(t *testing.T) {
	wr := newWhenResult("hello\n", "some error", 0)
	step := parser.Step{Text: `stdout equals "hello"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestStderrEquals(t *testing.T) {
	wr := newWhenResult("hello", "error msg\n", 0)
	step := parser.Step{Text: `stderr equals "error msg"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputIsEmpty(t *testing.T) {
	wr := newWhenResult("", "", 0)
	step := parser.Step{Text: "output is empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputIsNotEmpty(t *testing.T) {
	wr := newWhenResult("something", "", 0)
	step := parser.Step{Text: "output is not empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputIsEmptyFailure(t *testing.T) {
	wr := newWhenResult("not empty", "", 0)
	step := parser.Step{Text: "output is empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
}

func TestOutputIsNotEmptyFailure(t *testing.T) {
	wr := newWhenResult("", "", 0)
	step := parser.Step{Text: "output is not empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
}

func TestStderrIsEmpty(t *testing.T) {
	wr := newWhenResult("stdout stuff", "", 0)
	step := parser.Step{Text: "stderr is empty"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestFileMatchesPattern(t *testing.T) {
	fd := &fakeDocker{readFileContent: "v1.2.3\n"}
	step := parser.Step{Text: `file "VERSION" matches pattern "v\d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.True(t, r.Pass)
}

func TestFileDoesNotMatchPattern(t *testing.T) {
	fd := &fakeDocker{readFileContent: "release notes here\n"}
	step := parser.Step{Text: `file "notes.txt" does not match pattern "^ERROR"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.True(t, r.Pass)
}

func TestFileMatchesPatternFailure(t *testing.T) {
	fd := &fakeDocker{readFileContent: "no version\n"}
	step := parser.Step{Text: `file "VERSION" matches pattern "v\d+\.\d+\.\d+"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "does not match pattern")
}

func TestFileEquals(t *testing.T) {
	fd := &fakeDocker{readFileContent: "1.3.0\n"}
	step := parser.Step{Text: `file "VERSION" equals "1.3.0"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.True(t, r.Pass)
}

func TestFileDoesNotEqual(t *testing.T) {
	fd := &fakeDocker{readFileContent: "1.3.0\n"}
	step := parser.Step{Text: `file "VERSION" does not equal "2.0.0"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.True(t, r.Pass)
}

func TestFileEqualsFailure(t *testing.T) {
	fd := &fakeDocker{readFileContent: "1.3.0\n"}
	step := parser.Step{Text: `file "VERSION" equals "2.0.0"`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "does not equal")
}

func TestFileIsEmpty(t *testing.T) {
	fd := &fakeDocker{readFileContent: ""}
	step := parser.Step{Text: `file "empty.txt" is empty`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.True(t, r.Pass)
}

func TestFileIsNotEmpty(t *testing.T) {
	fd := &fakeDocker{readFileContent: "some content\n"}
	step := parser.Step{Text: `file "data.txt" is not empty`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.True(t, r.Pass)
}

func TestFileIsEmptyFailure(t *testing.T) {
	fd := &fakeDocker{readFileContent: "not empty"}
	step := parser.Step{Text: `file "data.txt" is empty`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.False(t, r.Pass)
}

func TestFileIsNotEmptyFailure(t *testing.T) {
	fd := &fakeDocker{readFileContent: ""}
	step := parser.Step{Text: `file "data.txt" is not empty`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
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
	results := assertions.EvaluateAll(context.Background(), steps, newWhenResult("", "", 0), fd, "cid")
	require.Len(t, fd.batchChecks, 4)
	assert.True(t, results[0].Pass)
	assert.True(t, results[1].Pass)
	assert.True(t, results[2].Pass)
	assert.True(t, results[3].Pass)
}

func TestUnknownAssertion(t *testing.T) {
	wr := newWhenResult("", "", 0)
	step := parser.Step{Text: "totally unknown assertion"}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "unknown Then assertion")
}

func TestOutputContainsEscapedQuote(t *testing.T) {
	wr := newWhenResult(`"name": "value"`, "", 0)
	step := parser.Step{Text: `output contains "\"name\": \"value\""`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputDoesNotContainEscapedQuote(t *testing.T) {
	wr := newWhenResult(`no json here`, "", 0)
	step := parser.Step{Text: `output does not contain "\"name\": \"value\""`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputContainsEscapedQuoteFailure(t *testing.T) {
	wr := newWhenResult(`no json here`, "", 0)
	step := parser.Step{Text: `output contains "\"name\": \"value\""`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
}

func TestOutputMatchesEscapedQuote(t *testing.T) {
	wr := newWhenResult(`{"ok":true}`, "", 0)
	step := parser.Step{Text: `output matches pattern "\{\"ok\":true\}"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputJSONPathExists(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Alice"}}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.user.name" exists`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestStdoutJSONPathEqualsString(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Alice"}}`, "", 0)
	step := parser.Step{Text: `stdout as JSON at path "$.user.name" equals "Alice"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestStderrJSONPathEqualsBoolean(t *testing.T) {
	wr := newWhenResult("", `{"ok":true}`, 0)
	step := parser.Step{Text: `stderr as JSON at path "$.ok" equals true`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputJSONPathEqualsNumber(t *testing.T) {
	wr := newWhenResult(`{"count":3}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.count" equals 3`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputJSONPathEqualsNull(t *testing.T) {
	wr := newWhenResult(`{"value":null}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.value" equals null`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputJSONPathEqualsBlockObject(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Alice","roles":["admin"]}}`, "", 0)
	step := parser.Step{
		Text:  `output as JSON at path "$.user" equals:`,
		Block: "{\n  \"name\": \"Alice\",\n  \"roles\": [\"admin\"]\n}",
	}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.True(t, r.Pass)
}

func TestOutputJSONPathInvalidJSON(t *testing.T) {
	wr := newWhenResult(`not json`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.user.name" exists`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "output is not valid JSON")
}

func TestOutputJSONPathInvalidPath(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Alice"}}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.user[" exists`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "invalid JSONPath")
}

func TestOutputJSONPathEqualsRequiresSingleMatch(t *testing.T) {
	wr := newWhenResult(`{"items":[{"id":1},{"id":2}]}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.items[*].id" equals 1`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, "matched 2 values")
}

func TestOutputJSONPathEqualsFailureShowsExpectedAndActual(t *testing.T) {
	wr := newWhenResult(`{"user":{"name":"Bob"}}`, "", 0)
	step := parser.Step{Text: `output as JSON at path "$.user.name" equals "Alice"`}
	r := assertions.Evaluate(context.Background(), step, wr, nil, "")
	assert.False(t, r.Pass)
	assert.Contains(t, r.Message, `expected "Alice", got "Bob"`)
}

func TestFileJSONPathExists(t *testing.T) {
	fd := &fakeDocker{readFileContent: `{"meta":{"ok":true}}`}
	step := parser.Step{Text: `file "result.json" as JSON at path "$.meta.ok" exists`}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
	assert.True(t, r.Pass)
}

func TestFileJSONPathEqualsArrayBlock(t *testing.T) {
	fd := &fakeDocker{readFileContent: `{"items":[1,2,3]}`}
	step := parser.Step{
		Text:  `file "result.json" as JSON at path "$.items" equals:`,
		Block: "[1, 2, 3]",
	}
	r := assertions.Evaluate(context.Background(), step, newWhenResult("", "", 0), fd, "cid")
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

	results := assertions.EvaluateAll(context.Background(), steps, newWhenResult("", "", 0), fd, "cid")
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

	results := assertions.EvaluateAll(context.Background(), steps, newWhenResult("", "", 0), fd, "cid")
	assert.True(t, results[0].Pass)
}

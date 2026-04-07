package assertions_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nskut/smoko/internal/assertions"
	"github.com/nskut/smoko/internal/executor"
	"github.com/nskut/smoko/internal/parser"
)

// newWhenResult is a test helper that returns a WhenResult with the given fields.
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

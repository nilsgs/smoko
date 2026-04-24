package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nskut/smoko/internal/config"
	"github.com/nskut/smoko/internal/executor"
	"github.com/nskut/smoko/internal/parser"
	"github.com/nskut/smoko/internal/reporter"
)

func TestRunCmdDefaults(t *testing.T) {
	cmd := runCmd()

	assert.Equal(t, "1", cmd.Flags().Lookup("timeout").DefValue)
	assert.Equal(t, "0", cmd.Flags().Lookup("parallel").DefValue)
	assert.Equal(t, "", cmd.Flags().Lookup("output").DefValue)
}

func TestResolveTimeoutUsesBuiltInDefault(t *testing.T) {
	timeout := resolveTimeout(config.Config{}, config.DefaultTimeout, false)

	assert.Equal(t, time.Second, timeout)
}

func TestResolveTimeoutUsesConfigWhenFlagNotSet(t *testing.T) {
	timeout := resolveTimeout(config.Config{Timeout: 5}, config.DefaultTimeout, false)

	assert.Equal(t, 5*time.Second, timeout)
}

func TestResolveTimeoutUsesFlagWhenSet(t *testing.T) {
	timeout := resolveTimeout(config.Config{Timeout: 5}, 2, true)

	assert.Equal(t, 2*time.Second, timeout)
}

func TestResolveWorkerCountUsesAutoForZeroOrLess(t *testing.T) {
	workers := resolveWorkerCount(0)

	assert.Equal(t, runtime.GOMAXPROCS(0), workers)
}

func TestResolveWorkerCountUsesExplicitValue(t *testing.T) {
	workers := resolveWorkerCount(3)

	assert.Equal(t, 3, workers)
}

func TestParseOutputModeDefaultsToText(t *testing.T) {
	mode, err := parseOutputMode("")

	require.NoError(t, err)
	assert.Equal(t, reporter.OutputModeText, mode)
}

func TestParseOutputModeAcceptsJSON(t *testing.T) {
	mode, err := parseOutputMode("json")

	require.NoError(t, err)
	assert.Equal(t, reporter.OutputModeJSON, mode)
}

func TestParseOutputModeRejectsUnknownValue(t *testing.T) {
	_, err := parseOutputMode("human")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "supported: json")
}

func echoCmd(msg string) string {
	if runtime.GOOS == "windows" {
		return "echo " + msg
	}
	return "echo " + msg
}

func failCmd() string {
	if runtime.GOOS == "windows" {
		return "exit 1"
	}
	return "exit 1"
}

func TestRunBuildSuppressesOutputOnSuccess(t *testing.T) {
	// Redirect stderr to capture the "Building:" header and any build output.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w

	buildErr := runBuild(echoCmd("build-output-should-be-hidden"), t.TempDir(), reporter.OutputModeText, false)

	w.Close()
	os.Stderr = orig
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	require.NoError(t, buildErr)
	// Strip the "Building: <cmd>" header line, then verify no command output leaked through.
	var nonHeader []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "Building:") {
			nonHeader = append(nonHeader, line)
		}
	}
	remaining := strings.Join(nonHeader, "\n")
	assert.False(t, strings.Contains(remaining, "build-output-should-be-hidden"), "build stdout should be suppressed on success")
}

func TestRunBuildPrintsOutputOnFailure(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "echo build-failure-output && exit 1"
	} else {
		cmd = "echo build-failure-output && exit 1"
	}
	buildErr := runBuild(cmd, t.TempDir(), reporter.OutputModeText, false)

	w.Close()
	os.Stderr = orig
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	require.Error(t, buildErr)
	assert.Contains(t, buildErr.Error(), "build failed")
	assert.Contains(t, out, "build-failure-output", "captured build output should be printed on failure")
}

func TestRunBuildVerboseStreamsOutput(t *testing.T) {
	// In verbose mode runBuild streams directly — it should not return an error on success.
	err := runBuild(echoCmd("verbose-output"), t.TempDir(), reporter.OutputModeText, true)
	require.NoError(t, err)
}
func TestValidateParsedFilesAcceptsStrictScenarioOrder(t *testing.T) {
	files := []parsedFile{{
		name: "test.smoko",
		features: []parser.Feature{{
			Name: "Feature",
			Background: []parser.Step{
				step(parser.StepGiven, "a file exists", 2),
			},
			Scenarios: []parser.Scenario{{
				Name: "valid",
				Line: 3,
				Steps: []parser.Step{
					step(parser.StepGiven, "setup", 4),
					step(parser.StepWhen, "I run command", 5),
					step(parser.StepThen, "exit code is 0", 6),
				},
			}},
		}},
	}}

	require.NoError(t, validateParsedFiles(files))
}

func TestValidateParsedFilesRejectsBackgroundWhenStep(t *testing.T) {
	files := []parsedFile{{
		name: "test.smoko",
		features: []parser.Feature{{
			Name:       "Feature",
			Background: []parser.Step{step(parser.StepWhen, "I run command", 2)},
			Scenarios: []parser.Scenario{{
				Name: "valid",
				Line: 3,
				Steps: []parser.Step{
					step(parser.StepWhen, "I run command", 4),
					step(parser.StepThen, "exit code is 0", 5),
				},
			}},
		}},
	}}

	err := validateParsedFiles(files)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "background")
	assert.Contains(t, err.Error(), "must be a Given")
}

func TestValidateParsedFilesRejectsMissingWhen(t *testing.T) {
	sc := parser.Scenario{
		Name: "missing when",
		Line: 10,
		Steps: []parser.Step{
			step(parser.StepGiven, "setup", 11),
		},
	}

	err := validateScenario("test.smoko", "Feature", sc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one When")
}

func TestValidateParsedFilesRejectsMissingThen(t *testing.T) {
	sc := parser.Scenario{
		Name:  "missing then",
		Line:  10,
		Steps: []parser.Step{step(parser.StepWhen, "I run command", 11)},
	}

	err := validateScenario("test.smoko", "Feature", sc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one Then")
}

func TestValidateParsedFilesRejectsGivenAfterWhen(t *testing.T) {
	sc := parser.Scenario{
		Name: "given after when",
		Line: 10,
		Steps: []parser.Step{
			step(parser.StepWhen, "I run command", 11),
			step(parser.StepGiven, "late setup", 12),
			step(parser.StepThen, "exit code is 0", 13),
		},
	}

	err := validateScenario("test.smoko", "Feature", sc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "after the When")
}

func TestValidateParsedFilesRejectsThenBeforeWhen(t *testing.T) {
	sc := parser.Scenario{
		Name: "then before when",
		Line: 10,
		Steps: []parser.Step{
			step(parser.StepThen, "exit code is 0", 11),
			step(parser.StepWhen, "I run command", 12),
		},
	}

	err := validateScenario("test.smoko", "Feature", sc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "before a When")
}

func TestValidateParsedFilesRejectsMultipleWhenSteps(t *testing.T) {
	sc := parser.Scenario{
		Name: "multiple when",
		Line: 10,
		Steps: []parser.Step{
			step(parser.StepWhen, "I run first", 11),
			step(parser.StepWhen, "I run second", 12),
			step(parser.StepThen, "exit code is 0", 13),
		},
	}

	err := validateScenario("test.smoko", "Feature", sc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple When")
}

func TestRunTestsListValidatesScenarioOrder(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "invalid.smoko")
	err := os.WriteFile(specPath, []byte(`Feature: Invalid
  Image: alpine:latest

  Scenario: Given after When
    When I run "true"
    Given a file "late.txt" exists
    Then exit code is 0
`), 0644)
	require.NoError(t, err)

	err = runTests(specPath, "", config.DefaultTimeout, false, false, "", false, 1, true, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "after the When")
}

func TestRunTestsListSkipsConfiguredBuild(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() {
		require.NoError(t, os.Chdir(cwd))
	}()

	err = os.WriteFile(filepath.Join(dir, ".smokorc"), []byte(`build = "exit 99"`), 0644)
	require.NoError(t, err)
	specPath := filepath.Join(dir, "valid.smoko")
	err = os.WriteFile(specPath, []byte(`Feature: Valid
  Scenario: Lists without build
    When I run "true"
    Then exit code is 0
`), 0644)
	require.NoError(t, err)

	err = runTests(specPath, "", config.DefaultTimeout, false, false, "", false, 1, false, true)

	require.NoError(t, err)
}

func TestExpectedExitCodeAssertionPassesWhenExitCodeMatches(t *testing.T) {
	expected := 2
	step := parser.Step{Text: `I run "false" expecting exit code 2`, Line: 12}
	wr := &executor.WhenResult{ExitCode: 2, ExpectedExitCode: &expected}

	ar, ok := expectedExitCodeAssertion(&step, wr)

	require.True(t, ok)
	assert.True(t, ar.Pass)
	assert.Empty(t, ar.Message)
	assert.Equal(t, step.Text, ar.StepText)
	assert.Equal(t, step.Line, ar.StepLine)
}

func TestExpectedExitCodeAssertionFailsWhenExitCodeDiffers(t *testing.T) {
	expected := 1
	step := parser.Step{Text: `I run "true" expecting exit code 1`, Line: 8}
	wr := &executor.WhenResult{ExitCode: 0, ExpectedExitCode: &expected}

	ar, ok := expectedExitCodeAssertion(&step, wr)

	require.True(t, ok)
	assert.False(t, ar.Pass)
	assert.Equal(t, "exit code: expected 1, got 0", ar.Message)
	assert.Equal(t, step.Text, ar.StepText)
	assert.Equal(t, step.Line, ar.StepLine)
}

func TestExpectedExitCodeAssertionSkipsUnannotatedWhen(t *testing.T) {
	step := parser.Step{Text: `I run "true"`, Line: 8}
	wr := &executor.WhenResult{ExitCode: 0}

	_, ok := expectedExitCodeAssertion(&step, wr)

	assert.False(t, ok)
}

func step(kind parser.StepType, text string, line int) parser.Step {
	return parser.Step{
		Type:         kind,
		ResolvedType: kind,
		Text:         text,
		Line:         line,
	}
}

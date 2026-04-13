package reporter_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nskut/smoko/internal/reporter"
)

func TestAddPassingScenarioPrintsStatusWithDuration(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, false, reporter.OutputModeText, "dev")
	rep.Add(reporter.ScenarioReport{
		FeatureName:  "My Feature",
		ScenarioName: "My Scenario",
		Passed:       true,
		Duration:     1250 * time.Millisecond,
	})

	out := buf.String()
	assert.Contains(t, out, "PASS My Feature / My Scenario (1.25s)")
}

func TestPrintSummaryShowsFailuresFeatureDurationsAndSuiteTime(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, false, reporter.OutputModeText, "dev")
	rep.Add(reporter.ScenarioReport{
		Order:        1,
		File:         "specs/a.smoko",
		FeatureName:  "Feature A",
		ScenarioName: "Failing Scenario",
		ScenarioLine: 12,
		Duration:     250 * time.Millisecond,
		AssertionResults: []reporter.AssertionReport{
			{Pass: false, Message: "exit code: expected 0, got 1", StepText: `exit code is 0`, StepLine: 14},
		},
	})
	rep.Add(reporter.ScenarioReport{
		Order:        0,
		File:         "specs/a.smoko",
		FeatureName:  "Feature A",
		ScenarioName: "Passing Scenario",
		Passed:       true,
		ScenarioLine: 4,
		Duration:     125 * time.Millisecond,
	})

	passed := rep.PrintSummary(2*time.Second, false)

	assert.False(t, passed)
	out := buf.String()
	assert.Contains(t, out, "Failures:")
	assert.Contains(t, out, "[FAIL] Feature A / Failing Scenario (250ms)")
	assert.Contains(t, out, "step 14: exit code is 0")
	assert.Contains(t, out, "Feature Durations:")
	assert.Contains(t, out, "Feature A (2 scenarios) 375ms")
	assert.Contains(t, out, "1 passed, 1 failed, 0 errors (2 total) in 2s")
}

func TestVerboseSummaryIncludesCapturedStreams(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, true, reporter.OutputModeText, "dev")
	rep.Add(reporter.ScenarioReport{
		File:         "specs/a.smoko",
		FeatureName:  "F",
		ScenarioName: "S",
		Passed:       true,
		Duration:     10 * time.Millisecond,
		Stdout:       "hello\nworld\n",
	})

	rep.PrintSummary(10*time.Millisecond, false)

	out := buf.String()
	assert.Contains(t, out, "Verbose Output:")
	assert.Contains(t, out, "stdout:")
	assert.Contains(t, out, "      hello")
	assert.Contains(t, out, "      world")
}

func TestJSONSummaryIncludesDurationsAndStableOrder(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, false, reporter.OutputModeJSON, "1.2.3")
	exitCode := 7
	rep.Add(reporter.ScenarioReport{
		Order:        2,
		File:         "specs/b.smoko",
		FeatureName:  "B",
		ScenarioName: "Later",
		ScenarioLine: 20,
		Duration:     2 * time.Second,
		Passed:       true,
		ExitCode:     &exitCode,
		Stdout:       "ok\n",
	})
	rep.Add(reporter.ScenarioReport{
		Order:        1,
		File:         "specs/a.smoko",
		FeatureName:  "A",
		ScenarioName: "Earlier",
		ScenarioLine: 10,
		Duration:     1500 * time.Millisecond,
		AssertionResults: []reporter.AssertionReport{
			{Pass: false, Message: "boom", StepText: "output contains x", StepLine: 11},
		},
	})

	passed := rep.PrintSummary(5*time.Second, true)

	assert.False(t, passed)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.Equal(t, "1", payload["schema_version"])
	summary := payload["summary"].(map[string]any)
	assert.Equal(t, "5s", summary["duration"])
	assert.Equal(t, true, summary["incomplete"])

	scenarios := payload["scenarios"].([]any)
	first := scenarios[0].(map[string]any)
	second := scenarios[1].(map[string]any)
	assert.Equal(t, "Earlier", first["scenario"])
	assert.Equal(t, "Later", second["scenario"])
	assert.Equal(t, "1.5s", first["duration"])
	assert.Equal(t, "2s", second["duration"])
}

package reporter_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nskut/smoko/internal/assertions"
	"github.com/nskut/smoko/internal/reporter"
)

func TestAddPassingScenario(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, false)
	rep.Add(reporter.ScenarioReport{
		FeatureName:  "My Feature",
		ScenarioName: "My Scenario",
		Passed:       true,
	})
	out := buf.String()
	assert.Contains(t, out, "✓")
	assert.Contains(t, out, "My Feature")
	assert.Contains(t, out, "My Scenario")
}

func TestAddFailingScenario(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, false)
	rep.Add(reporter.ScenarioReport{
		FeatureName:  "My Feature",
		ScenarioName: "My Scenario",
		Passed:       false,
		AssertionResults: []assertions.Result{
			{Pass: false, Message: "exit code: expected 0, got 1"},
		},
	})
	out := buf.String()
	assert.Contains(t, out, "✗")
	assert.Contains(t, out, "exit code: expected 0, got 1")
}

func TestAddErrorScenario(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, false)
	rep.Add(reporter.ScenarioReport{
		FeatureName:  "My Feature",
		ScenarioName: "My Scenario",
		Error:        assert.AnError,
	})
	out := buf.String()
	assert.Contains(t, out, "Error:")
}

func TestPrintSummaryAllPassed(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, false)
	rep.Add(reporter.ScenarioReport{FeatureName: "F", ScenarioName: "S1", Passed: true})
	rep.Add(reporter.ScenarioReport{FeatureName: "F", ScenarioName: "S2", Passed: true})
	passed := rep.PrintSummary()
	assert.True(t, passed)
	assert.Contains(t, buf.String(), "2 passed")
}

func TestPrintSummaryWithFailures(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, false)
	rep.Add(reporter.ScenarioReport{FeatureName: "F", ScenarioName: "S1", Passed: true})
	rep.Add(reporter.ScenarioReport{FeatureName: "F", ScenarioName: "S2", Passed: false})
	passed := rep.PrintSummary()
	assert.False(t, passed)
	assert.Contains(t, buf.String(), "1 passed, 1 failed")
}

func TestVerboseShowsStdout(t *testing.T) {
	var buf bytes.Buffer
	rep := reporter.New(&buf, true)
	rep.Add(reporter.ScenarioReport{
		FeatureName:  "F",
		ScenarioName: "S",
		Passed:       true,
		Stdout:       "some output\n",
	})
	assert.Contains(t, buf.String(), "some output")
}

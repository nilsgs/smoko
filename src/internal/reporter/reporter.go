package reporter

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type OutputMode string

const (
	OutputModeText OutputMode = ""
	OutputModeJSON OutputMode = "json"
)

// AssertionReport holds the result of one Then assertion together with source metadata.
type AssertionReport struct {
	Pass     bool   `json:"pass"`
	Message  string `json:"message,omitempty"`
	StepText string `json:"step_text,omitempty"`
	StepLine int    `json:"step_line,omitempty"`
}

// ScenarioReport holds the result of running one scenario.
type ScenarioReport struct {
	Order            int               `json:"-"`
	File             string            `json:"file"`
	FeatureName      string            `json:"feature"`
	ScenarioName     string            `json:"scenario"`
	ScenarioLine     int               `json:"scenario_line,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	Passed           bool              `json:"-"`
	Duration         time.Duration     `json:"-"`
	AssertionResults []AssertionReport `json:"assertions"`
	Stdout           string            `json:"stdout,omitempty"`
	Stderr           string            `json:"stderr,omitempty"`
	Error            error             `json:"-"`
	ExitCode         *int              `json:"exit_code,omitempty"`
}

type featureReport struct {
	File       string         `json:"file"`
	Name       string         `json:"name"`
	Duration   string         `json:"duration"`
	ScenarioCt int            `json:"scenario_count"`
	Scenarios  []jsonScenario `json:"scenarios"`
}

type jsonScenario struct {
	File         string            `json:"file"`
	Feature      string            `json:"feature"`
	Scenario     string            `json:"scenario"`
	ScenarioLine int               `json:"scenario_line,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	Status       string            `json:"status"`
	Duration     string            `json:"duration"`
	ExitCode     *int              `json:"exit_code,omitempty"`
	Stdout       string            `json:"stdout,omitempty"`
	Stderr       string            `json:"stderr,omitempty"`
	Assertions   []AssertionReport `json:"assertions,omitempty"`
	Error        string            `json:"error,omitempty"`
}

type jsonSummary struct {
	Passed     int    `json:"passed"`
	Failed     int    `json:"failed"`
	Errors     int    `json:"errors"`
	Total      int    `json:"total"`
	Duration   string `json:"duration"`
	Incomplete bool   `json:"incomplete,omitempty"`
}

type jsonReport struct {
	SchemaVersion string          `json:"schema_version"`
	ToolVersion   string          `json:"tool_version,omitempty"`
	Success       bool            `json:"success"`
	Summary       jsonSummary     `json:"summary"`
	Features      []featureReport `json:"features,omitempty"`
	Scenarios     []jsonScenario  `json:"scenarios,omitempty"`
	FatalError    string          `json:"fatal_error,omitempty"`
}

type summary struct {
	passed int
	failed int
	errors int
	total  int
}

type featureKey struct {
	file string
	name string
}

// Reporter collects and prints scenario results.
type Reporter struct {
	w           io.Writer
	verbose     bool
	mode        OutputMode
	color       bool
	toolVersion string
	mu          sync.Mutex
	reports     []ScenarioReport
}

// New creates a Reporter that writes to w.
func New(w io.Writer, verbose bool, mode OutputMode, toolVersion string) *Reporter {
	color := mode == OutputModeText && isTerminal(w) && os.Getenv("NO_COLOR") == ""
	return &Reporter{w: w, verbose: verbose, mode: mode, color: color, toolVersion: toolVersion}
}

// Add records a scenario result. In text mode it also prints a live one-line status.
// Safe for concurrent use.
func (r *Reporter) Add(rep ScenarioReport) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.reports = append(r.reports, rep)

	if r.mode == OutputModeJSON {
		return
	}

	r.printLine(rep)
}

// PrintSummary writes the final report and returns true if all tests passed.
func (r *Reporter) PrintSummary(suiteDuration time.Duration, incomplete bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.mode == OutputModeJSON {
		return r.printJSON(suiteDuration, incomplete, "")
	}

	return r.printText(suiteDuration)
}

// PrintFatal emits a fatal non-scenario report, primarily for machine-readable mode.
func (r *Reporter) PrintFatal(err error, suiteDuration time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.mode == OutputModeJSON {
		return r.printJSON(suiteDuration, false, err.Error())
	}

	fmt.Fprintf(r.w, "ERROR %v\n", err)
	fmt.Fprintf(r.w, "\n0 passed, 0 failed, 1 error (0 total) in %s\n", formatDuration(suiteDuration))
	return false
}

func (r *Reporter) printText(suiteDuration time.Duration) bool {
	reports := r.sortedReports()
	sum := summarize(reports)

	if len(reports) > 0 {
		fmt.Fprintln(r.w)
	}

	r.printFailures(reports)
	r.printVerboseOutput(reports)
	r.printFeatureDurations(reports)

	fmt.Fprintf(
		r.w,
		"%s%d passed, %d failed, %d errors (%d total) in %s%s\n",
		r.summaryColor(sum),
		sum.passed,
		sum.failed,
		sum.errors,
		sum.total,
		formatDuration(suiteDuration),
		r.reset(),
	)

	return sum.failed == 0 && sum.errors == 0
}

func (r *Reporter) printJSON(suiteDuration time.Duration, incomplete bool, fatal string) bool {
	reports := r.sortedReports()
	sum := summarize(reports)
	payload := jsonReport{
		SchemaVersion: "1",
		ToolVersion:   r.toolVersion,
		Success:       sum.failed == 0 && sum.errors == 0 && fatal == "",
		Summary: jsonSummary{
			Passed:     sum.passed,
			Failed:     sum.failed,
			Errors:     sum.errors,
			Total:      sum.total,
			Duration:   formatDuration(suiteDuration),
			Incomplete: incomplete,
		},
		Features:   buildFeatureReports(reports),
		Scenarios:  buildJSONScenarios(reports),
		FatalError: fatal,
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
	return payload.Success
}

func (r *Reporter) printLine(rep ScenarioReport) {
	status := "PASS"
	color := r.green()
	if rep.Error != nil {
		status = "ERROR"
		color = r.red()
	} else if !rep.Passed {
		status = "FAIL"
		color = r.red()
	}
	fmt.Fprintf(
		r.w,
		"%s%s%s %s / %s (%s)\n",
		color,
		status,
		r.reset(),
		rep.FeatureName,
		rep.ScenarioName,
		formatDuration(rep.Duration),
	)
}

func (r *Reporter) printFailures(reports []ScenarioReport) {
	first := true
	for _, rep := range reports {
		if rep.Error == nil && rep.Passed {
			continue
		}
		if first {
			fmt.Fprintln(r.w, "Failures:")
			first = false
		}

		status := "FAIL"
		if rep.Error != nil {
			status = "ERROR"
		}
		fmt.Fprintf(r.w, "  [%s] %s / %s (%s)\n", status, rep.FeatureName, rep.ScenarioName, formatDuration(rep.Duration))
		if rep.File != "" {
			if rep.ScenarioLine > 0 {
				fmt.Fprintf(r.w, "    at %s:%d\n", rep.File, rep.ScenarioLine)
			} else {
				fmt.Fprintf(r.w, "    at %s\n", rep.File)
			}
		}
		if rep.Error != nil {
			fmt.Fprintf(r.w, "    error: %v\n", rep.Error)
			continue
		}
		for _, ar := range rep.AssertionResults {
			if ar.Pass {
				continue
			}
			if ar.StepLine > 0 || ar.StepText != "" {
				if ar.StepLine > 0 {
					fmt.Fprintf(r.w, "    step %d: %s\n", ar.StepLine, ar.StepText)
				} else {
					fmt.Fprintf(r.w, "    step: %s\n", ar.StepText)
				}
			}
			fmt.Fprintf(r.w, "    %s\n", ar.Message)
		}
	}
	if !first {
		fmt.Fprintln(r.w)
	}
}

func (r *Reporter) printVerboseOutput(reports []ScenarioReport) {
	if !r.verbose {
		return
	}

	first := true
	for _, rep := range reports {
		if rep.Stdout == "" && rep.Stderr == "" {
			continue
		}
		if first {
			fmt.Fprintln(r.w, "Verbose Output:")
			first = false
		}
		fmt.Fprintf(r.w, "  %s / %s (%s)\n", rep.FeatureName, rep.ScenarioName, formatDuration(rep.Duration))
		if rep.Stdout != "" {
			fmt.Fprintf(r.w, "    stdout:\n%s\n", blockIndent(rep.Stdout, "      "))
		}
		if rep.Stderr != "" {
			fmt.Fprintf(r.w, "    stderr:\n%s\n", blockIndent(rep.Stderr, "      "))
		}
	}
	if !first {
		fmt.Fprintln(r.w)
	}
}

func (r *Reporter) printFeatureDurations(reports []ScenarioReport) {
	if len(reports) == 0 {
		return
	}

	ordered := make([]featureKey, 0)
	seen := make(map[featureKey]bool)
	durations := make(map[featureKey]time.Duration)
	counts := make(map[featureKey]int)

	for _, rep := range reports {
		key := featureKey{file: rep.File, name: rep.FeatureName}
		if !seen[key] {
			seen[key] = true
			ordered = append(ordered, key)
		}
		durations[key] += rep.Duration
		counts[key]++
	}

	fmt.Fprintln(r.w, "Feature Durations:")
	for _, key := range ordered {
		fmt.Fprintf(r.w, "  %s (%d scenarios) %s\n", key.name, counts[key], formatDuration(durations[key]))
	}
	fmt.Fprintln(r.w)
}

func (r *Reporter) summaryColor(sum summary) string {
	if sum.failed == 0 && sum.errors == 0 {
		return r.green()
	}
	return r.red()
}

func (r *Reporter) green() string {
	if r.color {
		return "\033[32m"
	}
	return ""
}

func (r *Reporter) red() string {
	if r.color {
		return "\033[31m"
	}
	return ""
}

func (r *Reporter) reset() string {
	if r.color {
		return "\033[0m"
	}
	return ""
}

func (r *Reporter) sortedReports() []ScenarioReport {
	reports := append([]ScenarioReport(nil), r.reports...)
	sort.SliceStable(reports, func(i, j int) bool {
		return reports[i].Order < reports[j].Order
	})
	return reports
}

func summarize(reports []ScenarioReport) summary {
	sum := summary{total: len(reports)}
	for _, rep := range reports {
		switch {
		case rep.Error != nil:
			sum.errors++
		case rep.Passed:
			sum.passed++
		default:
			sum.failed++
		}
	}
	return sum
}

func buildFeatureReports(reports []ScenarioReport) []featureReport {
	ordered := make([]featureKey, 0)
	seen := make(map[featureKey]bool)
	buckets := make(map[featureKey][]ScenarioReport)
	durations := make(map[featureKey]time.Duration)

	for _, rep := range reports {
		key := featureKey{file: rep.File, name: rep.FeatureName}
		if !seen[key] {
			seen[key] = true
			ordered = append(ordered, key)
		}
		buckets[key] = append(buckets[key], rep)
		durations[key] += rep.Duration
	}

	features := make([]featureReport, 0, len(ordered))
	for _, key := range ordered {
		features = append(features, featureReport{
			File:       key.file,
			Name:       key.name,
			Duration:   formatDuration(durations[key]),
			ScenarioCt: len(buckets[key]),
			Scenarios:  buildJSONScenarios(buckets[key]),
		})
	}
	return features
}

func buildJSONScenarios(reports []ScenarioReport) []jsonScenario {
	scenarios := make([]jsonScenario, 0, len(reports))
	for _, rep := range reports {
		item := jsonScenario{
			File:         rep.File,
			Feature:      rep.FeatureName,
			Scenario:     rep.ScenarioName,
			ScenarioLine: rep.ScenarioLine,
			Tags:         rep.Tags,
			Status:       scenarioStatus(rep),
			Duration:     formatDuration(rep.Duration),
			ExitCode:     rep.ExitCode,
			Stdout:       rep.Stdout,
			Stderr:       rep.Stderr,
			Assertions:   rep.AssertionResults,
		}
		if rep.Error != nil {
			item.Error = rep.Error.Error()
		}
		scenarios = append(scenarios, item)
	}
	return scenarios
}

func scenarioStatus(rep ScenarioReport) string {
	switch {
	case rep.Error != nil:
		return "error"
	case rep.Passed:
		return "passed"
	default:
		return "failed"
	}
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Millisecond {
		return d.Round(time.Microsecond).String()
	}
	return d.Round(time.Millisecond).String()
}

func blockIndent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// isTerminal returns true when w is os.Stdout and stdout is a terminal.
func isTerminal(w io.Writer) bool {
	if w != os.Stdout {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

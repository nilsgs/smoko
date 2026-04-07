package reporter

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/nskut/smoko/internal/assertions"
)

// ScenarioReport holds the result of running one scenario.
type ScenarioReport struct {
	FeatureName      string
	ScenarioName     string
	Passed           bool
	AssertionResults []assertions.Result // results of all Then/And assertions
	Stdout           string
	Stderr           string
	Error            error // non-nil for infrastructure errors (docker, parse, etc.)
}

// Reporter collects and prints scenario results.
type Reporter struct {
	w        io.Writer
	verbose  bool
	color    bool
	mu       sync.Mutex
	reports  []ScenarioReport
}

// New creates a Reporter that writes to w.
func New(w io.Writer, verbose bool) *Reporter {
	color := isTerminal(w) && os.Getenv("NO_COLOR") == ""
	return &Reporter{w: w, verbose: verbose, color: color}
}

// Add records a scenario result and immediately prints the one-line status.
// Safe for concurrent use.
func (r *Reporter) Add(rep ScenarioReport) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.reports = append(r.reports, rep)

	if rep.Error != nil {
		r.printLine(false, rep.FeatureName, rep.ScenarioName)
		fmt.Fprintf(r.w, "    Error: %v\n", rep.Error)
		return
	}

	r.printLine(rep.Passed, rep.FeatureName, rep.ScenarioName)

	if !rep.Passed {
		for _, f := range rep.AssertionResults {
			if !f.Pass {
				fmt.Fprintf(r.w, "    ✗ %s\n", f.Message)
			}
		}
	}

	if r.verbose || !rep.Passed {
		if rep.Stdout != "" {
			fmt.Fprintf(r.w, "    stdout: %s\n", indent(rep.Stdout))
		}
		if rep.Stderr != "" {
			fmt.Fprintf(r.w, "    stderr: %s\n", indent(rep.Stderr))
		}
	}
}

// PrintSummary writes the final summary line and returns true if all tests passed.
func (r *Reporter) PrintSummary() bool {
	total := len(r.reports)
	passed := 0
	for _, rep := range r.reports {
		if rep.Passed && rep.Error == nil {
			passed++
		}
	}
	failed := total - passed

	fmt.Fprintln(r.w)
	if failed == 0 {
		fmt.Fprintf(r.w, "%s%d passed (%d total)%s\n", r.green(), passed, total, r.reset())
	} else {
		fmt.Fprintf(r.w, "%s%d passed, %d failed (%d total)%s\n", r.red(), passed, failed, total, r.reset())
	}
	return failed == 0
}

func (r *Reporter) printLine(passed bool, feature, scenario string) {
	if passed {
		fmt.Fprintf(r.w, "  %s✓%s %s / %s\n", r.green(), r.reset(), feature, scenario)
	} else {
		fmt.Fprintf(r.w, "  %s✗%s %s / %s\n", r.red(), r.reset(), feature, scenario)
	}
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

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if i > 0 {
			lines[i] = "           " + l
		}
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

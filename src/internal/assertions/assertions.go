package assertions

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nskut/smoko/internal/docker"
	"github.com/nskut/smoko/internal/executor"
	"github.com/nskut/smoko/internal/parser"
)

// Result is the outcome of a single assertion.
type Result struct {
	Pass    bool
	Message string // failure message; empty on pass
}

func pass() Result { return Result{Pass: true} }
func fail(format string, args ...interface{}) Result {
	return Result{Pass: false, Message: fmt.Sprintf(format, args...)}
}

// Evaluate evaluates a Then/And step against the captured WhenResult and
// the container filesystem.
func Evaluate(ctx context.Context, step parser.Step, wr *executor.WhenResult, dc *docker.Client, containerID string) Result {
	text := step.Text

	// --- exit code ---
	if m := reExitCodeIs.FindStringSubmatch(text); m != nil {
		expected, _ := strconv.Atoi(m[1])
		if wr.ExitCode != expected {
			return fail("exit code: expected %d, got %d", expected, wr.ExitCode)
		}
		return pass()
	}
	if m := reExitCodeIsNot.FindStringSubmatch(text); m != nil {
		expected, _ := strconv.Atoi(m[1])
		if wr.ExitCode == expected {
			return fail("exit code: expected not %d, but got %d", expected, wr.ExitCode)
		}
		return pass()
	}

	// --- output contains ---
	if m := reOutputContains.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not contain")
		needle := unescapeString(m[1])
		haystack := combined(wr, text)
		has := strings.Contains(haystack, needle)
		if negate && has {
			return fail("output unexpectedly contains %q", needle)
		}
		if !negate && !has {
			return fail("output does not contain %q\nActual output:\n%s", needle, haystack)
		}
		return pass()
	}

	// --- output matches pattern ---
	if m := reOutputMatches.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not match")
		pattern := unescapeString(m[1])
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fail("invalid regex %q: %v", pattern, err)
		}
		haystack := combined(wr, text)
		matches := re.MatchString(haystack)
		if negate && matches {
			return fail("output unexpectedly matches pattern %q", pattern)
		}
		if !negate && !matches {
			return fail("output does not match pattern %q\nActual output:\n%s", pattern, haystack)
		}
		return pass()
	}

	// --- file exists ---
	if m := reFileExists.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not exist")
		path := m[1]
		exists, err := dc.FileExists(ctx, containerID, path)
		if err != nil {
			return fail("file exists check error: %v", err)
		}
		if negate && exists {
			return fail("file %q unexpectedly exists", path)
		}
		if !negate && !exists {
			return fail("file %q does not exist", path)
		}
		return pass()
	}

	// --- directory exists ---
	if m := reDirExists.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not exist")
		path := unescapeString(m[1])
		exists, err := dc.DirExists(ctx, containerID, path)
		if err != nil {
			return fail("directory exists check error: %v", err)
		}
		if negate && exists {
			return fail("directory %q unexpectedly exists", path)
		}
		if !negate && !exists {
			return fail("directory %q does not exist", path)
		}
		return pass()
	}

	// --- file contains (block form) ---
	if m := reFileContainsBlock.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not contain")
		path := unescapeString(m[1])
		needle := step.Block
		content, err := dc.ReadFile(ctx, containerID, path)
		if err != nil {
			return fail("read file %q: %v", path, err)
		}
		if negate && strings.Contains(content, needle) {
			return fail("file %q unexpectedly contains %q", path, needle)
		}
		if !negate && !strings.Contains(content, needle) {
			return fail("file %q does not contain %q\nActual content:\n%s", path, needle, content)
		}
		return pass()
	}

	// --- file contains (inline form) ---
	if m := reFileContains.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not contain")
		path := unescapeString(m[1])
		needle := unescapeString(m[2])
		content, err := dc.ReadFile(ctx, containerID, path)
		if err != nil {
			return fail("read file %q: %v", path, err)
		}
		if negate && strings.Contains(content, needle) {
			return fail("file %q unexpectedly contains %q", path, needle)
		}
		if !negate && !strings.Contains(content, needle) {
			return fail("file %q does not contain %q\nActual content:\n%s", path, needle, content)
		}
		return pass()
	}

	return fail("unknown Then assertion: %q", text)
}

// unescapeString replaces \" with " and \\ with \ in a captured assertion string.
func unescapeString(s string) string {
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

// combined returns the appropriate output stream based on the step text keyword.
func combined(wr *executor.WhenResult, text string) string {
	if strings.HasPrefix(text, "stdout ") {
		return wr.Stdout
	}
	if strings.HasPrefix(text, "stderr ") {
		return wr.Stderr
	}
	return wr.CombinedOutput()
}

var (
	reExitCodeIs    = regexp.MustCompile(`^exit code is (\d+)$`)
	reExitCodeIsNot = regexp.MustCompile(`^exit code is not (\d+)$`)

	// output contains / stdout contains / stderr contains / does not contain
	// Capture group allows \" escape sequences.
	reOutputContains = regexp.MustCompile(`^(?:output|stdout|stderr)(?: does not)? contains? "((?:[^"\\]|\\.)*)"`)

	// output matches pattern / does not match pattern
	reOutputMatches = regexp.MustCompile(`^(?:output)(?: does not)? matches? pattern "((?:[^"\\]|\\.)*)"`)

	// file "X" exists / does not exist
	reFileExists = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"`)

	// directory "X" exists / does not exist
	reDirExists = regexp.MustCompile(`^(?:the )?directory "((?:[^"\\]|\\.)*)"(?:(?: does not)? exist[s]?)`)

	// file "X" [does not] contains: <block>
	reFileContainsBlock = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"` + `(?: does not)? contains:$`)

	// file "X" [does not] contains "Y"
	reFileContains = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"` + `(?: does not)? contains "((?:[^"\\]|\\.)*)"`)
)

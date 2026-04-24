package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/theory/jsonpath"

	"github.com/nskut/smoko/internal/docker"
	"github.com/nskut/smoko/internal/executor"
	"github.com/nskut/smoko/internal/hints"
	"github.com/nskut/smoko/internal/parser"
)

type dockerReader interface {
	FileExists(ctx context.Context, containerID, path string) (bool, error)
	DirExists(ctx context.Context, containerID, path string) (bool, error)
	ReadFile(ctx context.Context, containerID, path string) (string, error)
	BatchFSCheck(ctx context.Context, containerID string, checks []docker.FSCheck) ([]docker.FSResult, error)
}

// regexCache caches compiled user-provided regex patterns.
var regexCache sync.Map // string -> *regexp.Regexp

// jsonPathCache caches compiled JSONPath expressions.
var jsonPathCache sync.Map // string -> *jsonpath.Path

// compileRegex compiles a regex pattern, returning a cached version if available.
func compileRegex(pattern string) (*regexp.Regexp, error) {
	if cached, ok := regexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCache.Store(pattern, re)
	return re, nil
}

func compileJSONPath(path string) (*jsonpath.Path, error) {
	if cached, ok := jsonPathCache.Load(path); ok {
		return cached.(*jsonpath.Path), nil
	}
	compiled, err := jsonpath.Parse(path)
	if err != nil {
		return nil, err
	}
	jsonPathCache.Store(path, compiled)
	return compiled, nil
}

// Result is the outcome of a single assertion.
type Result struct {
	Pass    bool
	Message string // failure message; empty on pass
}

func pass() Result { return Result{Pass: true} }
func fail(format string, args ...interface{}) Result {
	return Result{Pass: false, Message: fmt.Sprintf(format, args...)}
}

// expandVars replaces $VAR and ${VAR} references in s using the KEY=VALUE
// pairs in env. Variables that are not present are replaced with empty string.
func expandVars(s string, env []string) string {
	if len(env) == 0 || !strings.ContainsRune(s, '$') {
		return s
	}
	lookup := make(map[string]string, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq >= 0 {
			lookup[kv[:eq]] = kv[eq+1:]
		}
	}
	return os.Expand(s, func(name string) string {
		return lookup[name]
	})
}

// EvaluateAll evaluates all Then steps, batching filesystem checks into a single
// docker exec for performance. Steps that don't require filesystem access are
// evaluated locally.
func EvaluateAll(ctx context.Context, steps []parser.Step, wr *executor.WhenResult, dc dockerReader, containerID string, env []string) []Result {
	results := make([]Result, len(steps))

	type checkRef struct {
		stepIdx      int
		globalIdx    int
		checkKind    string
		path         string
		needle       string
		pattern      string
		negate       bool
		jsonPath     string
		expectedJSON string
	}

	var allChecks []docker.FSCheck
	var refs []checkRef

	for i, step := range steps {
		if step.ResolvedType != parser.StepThen {
			continue
		}
		text := step.Text

		if m := reFileExists.FindStringSubmatch(text); m != nil {
			negate := strings.Contains(text, "does not exist")
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:   i,
				globalIdx: len(allChecks),
				checkKind: "file-exists",
				path:      path,
				negate:    negate,
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckFileExists, Path: path})
			continue
		}

		if m := reDirExists.FindStringSubmatch(text); m != nil {
			negate := strings.Contains(text, "does not exist")
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:   i,
				globalIdx: len(allChecks),
				checkKind: "dir-exists",
				path:      path,
				negate:    negate,
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckDirExists, Path: path})
			continue
		}

		if m := reFileContainsBlock.FindStringSubmatch(text); m != nil {
			negate := strings.Contains(text, "does not contain")
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:   i,
				globalIdx: len(allChecks),
				checkKind: "file-contains",
				path:      path,
				needle:    step.Block,
				negate:    negate,
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckReadFile, Path: path})
			continue
		}

		if m := reFileContains.FindStringSubmatch(text); m != nil {
			negate := strings.Contains(text, "does not contain")
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:   i,
				globalIdx: len(allChecks),
				checkKind: "file-contains",
				path:      path,
				needle:    unescapeString(m[2]),
				negate:    negate,
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckReadFile, Path: path})
			continue
		}

		if m := reFileJSONExists.FindStringSubmatch(text); m != nil {
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:   i,
				globalIdx: len(allChecks),
				checkKind: "file-json-exists",
				path:      path,
				jsonPath:  unescapeString(m[2]),
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckReadFile, Path: path})
			continue
		}

		if m := reFileJSONEqualsBlock.FindStringSubmatch(text); m != nil {
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:      i,
				globalIdx:    len(allChecks),
				checkKind:    "file-json-equals",
				path:         path,
				jsonPath:     unescapeString(m[2]),
				expectedJSON: step.Block,
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckReadFile, Path: path})
			continue
		}

		if m := reFileJSONEqualsInline.FindStringSubmatch(text); m != nil {
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:      i,
				globalIdx:    len(allChecks),
				checkKind:    "file-json-equals",
				path:         path,
				jsonPath:     unescapeString(m[2]),
				expectedJSON: strings.TrimSpace(m[3]),
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckReadFile, Path: path})
			continue
		}

		if m := reFileMatches.FindStringSubmatch(text); m != nil {
			negate := strings.Contains(text, "does not match")
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:   i,
				globalIdx: len(allChecks),
				checkKind: "file-matches",
				path:      path,
				pattern:   unescapeString(m[2]),
				negate:    negate,
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckReadFile, Path: path})
			continue
		}

		if m := reFileEquals.FindStringSubmatch(text); m != nil {
			negate := strings.Contains(text, "does not equal")
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:   i,
				globalIdx: len(allChecks),
				checkKind: "file-equals",
				path:      path,
				needle:    unescapeString(m[2]),
				negate:    negate,
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckReadFile, Path: path})
			continue
		}

		if m := reFileEmpty.FindStringSubmatch(text); m != nil {
			negate := m[2] == "not "
			path := expandVars(unescapeString(m[1]), env)
			refs = append(refs, checkRef{
				stepIdx:   i,
				globalIdx: len(allChecks),
				checkKind: "file-empty",
				path:      path,
				negate:    negate,
			})
			allChecks = append(allChecks, docker.FSCheck{Kind: docker.FSCheckReadFile, Path: path})
			continue
		}
	}

	var fsResults []docker.FSResult
	if len(allChecks) > 0 {
		var err error
		fsResults, err = dc.BatchFSCheck(ctx, containerID, allChecks)
		if err != nil {
			for i, step := range steps {
				if step.ResolvedType != parser.StepThen {
					continue
				}
				results[i] = Evaluate(ctx, step, wr, dc, containerID, env)
			}
			return results
		}
	}

	batchedSteps := make(map[int]bool)
	for _, ref := range refs {
		batchedSteps[ref.stepIdx] = true
		fr := fsResults[ref.globalIdx]

		switch ref.checkKind {
		case "file-exists":
			results[ref.stepIdx] = evaluateExistsResult("file", ref.path, ref.negate, fr)
		case "dir-exists":
			results[ref.stepIdx] = evaluateExistsResult("directory", ref.path, ref.negate, fr)
		case "file-contains":
			results[ref.stepIdx] = evaluateFileContainsResult(ref.path, ref.needle, ref.negate, fr)
		case "file-json-exists":
			if fr.Err != nil {
				results[ref.stepIdx] = fail("read file %q: %v", ref.path, fr.Err)
			} else {
				results[ref.stepIdx] = evaluateJSONExists(fmt.Sprintf("file %q", ref.path), fr.Content, ref.jsonPath)
			}
		case "file-json-equals":
			if fr.Err != nil {
				results[ref.stepIdx] = fail("read file %q: %v", ref.path, fr.Err)
			} else {
				results[ref.stepIdx] = evaluateJSONEquals(fmt.Sprintf("file %q", ref.path), fr.Content, ref.jsonPath, ref.expectedJSON)
			}
		case "file-matches":
			if fr.Err != nil {
				results[ref.stepIdx] = fail("read file %q: %v", ref.path, fr.Err)
			} else {
				re, err := compileRegex(ref.pattern)
				if err != nil {
					results[ref.stepIdx] = fail("invalid regex %q: %v", ref.pattern, err)
				} else {
					matches := re.MatchString(fr.Content)
					if ref.negate && matches {
						results[ref.stepIdx] = fail("file %q unexpectedly matches pattern %q", ref.path, ref.pattern)
					} else if !ref.negate && !matches {
						results[ref.stepIdx] = fail("file %q does not match pattern %q\nActual content:\n%s", ref.path, ref.pattern, fr.Content)
					} else {
						results[ref.stepIdx] = pass()
					}
				}
			}
		case "file-equals":
			if fr.Err != nil {
				results[ref.stepIdx] = fail("read file %q: %v", ref.path, fr.Err)
			} else {
				eq := strings.TrimSpace(fr.Content) == strings.TrimSpace(ref.needle)
				if ref.negate && eq {
					results[ref.stepIdx] = fail("file %q unexpectedly equals %q", ref.path, ref.needle)
				} else if !ref.negate && !eq {
					results[ref.stepIdx] = fail("file %q does not equal %q\nActual content:\n%s", ref.path, ref.needle, fr.Content)
				} else {
					results[ref.stepIdx] = pass()
				}
			}
		case "file-empty":
			if fr.Err != nil {
				results[ref.stepIdx] = fail("read file %q: %v", ref.path, fr.Err)
			} else {
				empty := strings.TrimSpace(fr.Content) == ""
				if ref.negate && empty {
					results[ref.stepIdx] = fail("file %q is empty but expected not empty", ref.path)
				} else if !ref.negate && !empty {
					results[ref.stepIdx] = fail("file %q is not empty\nActual content:\n%s", ref.path, fr.Content)
				} else {
					results[ref.stepIdx] = pass()
				}
			}
		}
	}

	for i, step := range steps {
		if step.ResolvedType != parser.StepThen {
			continue
		}
		if batchedSteps[i] {
			continue
		}
		results[i] = Evaluate(ctx, step, wr, dc, containerID, env)
	}

	return results
}

// Evaluate evaluates a Then/And step against the captured WhenResult and
// the container filesystem.
func Evaluate(ctx context.Context, step parser.Step, wr *executor.WhenResult, dc dockerReader, containerID string, env []string) Result {
	text := step.Text

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

	if m := reOutputContains.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not contain") || strings.Contains(text, "should not contain")
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

	if m := reOutputMatches.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not match")
		pattern := unescapeString(m[1])
		re, err := compileRegex(pattern)
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

	if m := reOutputJSONExists.FindStringSubmatch(text); m != nil {
		source := m[1]
		return evaluateJSONExists(source, selectOutputJSONSource(wr, source), unescapeString(m[2]))
	}

	if m := reOutputJSONEqualsBlock.FindStringSubmatch(text); m != nil {
		source := m[1]
		return evaluateJSONEquals(source, selectOutputJSONSource(wr, source), unescapeString(m[2]), step.Block)
	}

	if m := reOutputJSONEqualsInline.FindStringSubmatch(text); m != nil {
		source := m[1]
		return evaluateJSONEquals(source, selectOutputJSONSource(wr, source), unescapeString(m[2]), strings.TrimSpace(m[3]))
	}

	if m := reFileExists.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not exist")
		path := expandVars(unescapeString(m[1]), env)
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

	if m := reDirExists.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not exist")
		path := expandVars(unescapeString(m[1]), env)
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

	if m := reFileContainsBlock.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not contain")
		path := expandVars(unescapeString(m[1]), env)
		content, err := dc.ReadFile(ctx, containerID, path)
		if err != nil {
			return fail("read file %q: %v", path, err)
		}
		return evaluateFileContainsContent(path, content, step.Block, negate)
	}

	if m := reFileContains.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not contain")
		path := expandVars(unescapeString(m[1]), env)
		content, err := dc.ReadFile(ctx, containerID, path)
		if err != nil {
			return fail("read file %q: %v", path, err)
		}
		return evaluateFileContainsContent(path, content, unescapeString(m[2]), negate)
	}

	if m := reFileJSONExists.FindStringSubmatch(text); m != nil {
		filePath := expandVars(unescapeString(m[1]), env)
		content, err := dc.ReadFile(ctx, containerID, filePath)
		if err != nil {
			return fail("read file %q: %v", filePath, err)
		}
		return evaluateJSONExists(fmt.Sprintf("file %q", filePath), content, unescapeString(m[2]))
	}

	if m := reFileJSONEqualsBlock.FindStringSubmatch(text); m != nil {
		filePath := expandVars(unescapeString(m[1]), env)
		content, err := dc.ReadFile(ctx, containerID, filePath)
		if err != nil {
			return fail("read file %q: %v", filePath, err)
		}
		return evaluateJSONEquals(fmt.Sprintf("file %q", filePath), content, unescapeString(m[2]), step.Block)
	}

	if m := reFileJSONEqualsInline.FindStringSubmatch(text); m != nil {
		filePath := expandVars(unescapeString(m[1]), env)
		content, err := dc.ReadFile(ctx, containerID, filePath)
		if err != nil {
			return fail("read file %q: %v", filePath, err)
		}
		return evaluateJSONEquals(fmt.Sprintf("file %q", filePath), content, unescapeString(m[2]), strings.TrimSpace(m[3]))
	}

	if m := reFileMatches.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not match")
		filePath := expandVars(unescapeString(m[1]), env)
		pattern := unescapeString(m[2])
		content, err := dc.ReadFile(ctx, containerID, filePath)
		if err != nil {
			return fail("read file %q: %v", filePath, err)
		}
		re, err := compileRegex(pattern)
		if err != nil {
			return fail("invalid regex %q: %v", pattern, err)
		}
		matches := re.MatchString(content)
		if negate && matches {
			return fail("file %q unexpectedly matches pattern %q", filePath, pattern)
		}
		if !negate && !matches {
			return fail("file %q does not match pattern %q\nActual content:\n%s", filePath, pattern, content)
		}
		return pass()
	}

	if m := reFileEquals.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not equal")
		filePath := expandVars(unescapeString(m[1]), env)
		expected := unescapeString(m[2])
		content, err := dc.ReadFile(ctx, containerID, filePath)
		if err != nil {
			return fail("read file %q: %v", filePath, err)
		}
		eq := strings.TrimSpace(content) == strings.TrimSpace(expected)
		if negate && eq {
			return fail("file %q unexpectedly equals %q", filePath, expected)
		}
		if !negate && !eq {
			return fail("file %q does not equal %q\nActual content:\n%s", filePath, expected, content)
		}
		return pass()
	}

	if m := reFileEmpty.FindStringSubmatch(text); m != nil {
		negate := m[2] == "not "
		filePath := expandVars(unescapeString(m[1]), env)
		content, err := dc.ReadFile(ctx, containerID, filePath)
		if err != nil {
			return fail("read file %q: %v", filePath, err)
		}
		empty := strings.TrimSpace(content) == ""
		if negate && empty {
			return fail("file %q is empty but expected not empty", filePath)
		}
		if !negate && !empty {
			return fail("file %q is not empty\nActual content:\n%s", filePath, content)
		}
		return pass()
	}

	if m := reOutputEquals.FindStringSubmatch(text); m != nil {
		negate := strings.Contains(text, "does not equal")
		expected := unescapeString(m[1])
		haystack := combined(wr, text)
		eq := strings.TrimSpace(haystack) == strings.TrimSpace(expected)
		if negate && eq {
			return fail("output unexpectedly equals %q", expected)
		}
		if !negate && !eq {
			return fail("output does not equal %q\nActual output:\n%s", expected, haystack)
		}
		return pass()
	}

	if m := reOutputEmpty.FindStringSubmatch(text); m != nil {
		negate := m[1] == "not "
		haystack := combined(wr, text)
		empty := strings.TrimSpace(haystack) == ""
		if negate && empty {
			return fail("output is empty but expected not empty")
		}
		if !negate && !empty {
			return fail("output is not empty\nActual output:\n%s", haystack)
		}
		return pass()
	}

	if suggestion := hints.Suggest(text, knownThenPatterns); suggestion != "" {
		return fail("unknown Then assertion: %q\n  → did you mean: %q?", text, suggestion)
	}
	return fail("unknown Then assertion: %q", text)
}

var knownThenPatterns = []string{
	`exit code is 0`,
	`exit code is not 0`,
	`output contains "text"`,
	`output does not contain "text"`,
	`stdout contains "text"`,
	`stderr contains "text"`,
	`stdout does not contain "text"`,
	`stderr does not contain "text"`,
	`output matches pattern "regex"`,
	`stdout matches pattern "regex"`,
	`stderr matches pattern "regex"`,
	`output does not match pattern "regex"`,
	`stdout does not match pattern "regex"`,
	`stderr does not match pattern "regex"`,
	`output equals "text"`,
	`stdout equals "text"`,
	`stderr equals "text"`,
	`output does not equal "text"`,
	`output is empty`,
	`output is not empty`,
	`stdout is empty`,
	`stderr is empty`,
	`file "path" exists`,
	`file "path" does not exist`,
	`directory "path" exists`,
	`file "path" contains "text"`,
	`file "path" does not contain "text"`,
	`file "path" matches pattern "regex"`,
	`file "path" equals "text"`,
	`file "path" is empty`,
	`file "path" is not empty`,
	`output as JSON at path "$.field" exists`,
	`stdout as JSON at path "$.field" exists`,
	`output as JSON at path "$.field" equals "value"`,
	`file "path" as JSON at path "$.field" exists`,
	`file "path" as JSON at path "$.field" equals "value"`,
}

func evaluateExistsResult(kind, path string, negate bool, fr docker.FSResult) Result {
	if fr.Err != nil {
		return fail("%s exists check error: %v", kind, fr.Err)
	}
	if negate && fr.Exists {
		return fail("%s %q unexpectedly exists", kind, path)
	}
	if !negate && !fr.Exists {
		return fail("%s %q does not exist", kind, path)
	}
	return pass()
}

func evaluateFileContainsResult(path, needle string, negate bool, fr docker.FSResult) Result {
	if fr.Err != nil {
		return fail("read file %q: %v", path, fr.Err)
	}
	return evaluateFileContainsContent(path, fr.Content, needle, negate)
}

func evaluateFileContainsContent(path, content, needle string, negate bool) Result {
	if negate && strings.Contains(content, needle) {
		return fail("file %q unexpectedly contains %q", path, needle)
	}
	if !negate && !strings.Contains(content, needle) {
		return fail("file %q does not contain %q\nActual content:\n%s", path, needle, content)
	}
	return pass()
}

func evaluateJSONExists(sourceLabel, rawJSON, pathExpr string) Result {
	value, err := parseJSONValue(rawJSON)
	if err != nil {
		return fail("%s is not valid JSON: %v", sourceLabel, err)
	}
	path, err := compileJSONPath(pathExpr)
	if err != nil {
		return fail("invalid JSONPath %q: %v", pathExpr, err)
	}
	nodes := path.Select(value)
	if len(nodes) == 0 {
		return fail("%s JSONPath %q did not match any value", sourceLabel, pathExpr)
	}
	return pass()
}

func evaluateJSONEquals(sourceLabel, rawJSON, pathExpr, expectedRaw string) Result {
	value, err := parseJSONValue(rawJSON)
	if err != nil {
		return fail("%s is not valid JSON: %v", sourceLabel, err)
	}
	path, err := compileJSONPath(pathExpr)
	if err != nil {
		return fail("invalid JSONPath %q: %v", pathExpr, err)
	}
	nodes := path.Select(value)
	if len(nodes) == 0 {
		return fail("%s JSONPath %q did not match any value", sourceLabel, pathExpr)
	}
	if len(nodes) != 1 {
		return fail("%s JSONPath %q matched %d values; equals requires exactly one", sourceLabel, pathExpr, len(nodes))
	}

	expected, err := parseJSONValue(expectedRaw)
	if err != nil {
		return fail("expected JSON value is invalid: %v", err)
	}

	actual := nodes[0]
	if !reflect.DeepEqual(actual, expected) {
		return fail("%s JSONPath %q: expected %s, got %s", sourceLabel, pathExpr, formatJSON(expected), formatJSON(actual))
	}
	return pass()
}

func parseJSONValue(raw string) (any, error) {
	var value any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func formatJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func selectOutputJSONSource(wr *executor.WhenResult, source string) string {
	switch source {
	case "stdout":
		return wr.Stdout
	case "stderr":
		return wr.Stderr
	default:
		return wr.CombinedOutput()
	}
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

	reOutputContains = regexp.MustCompile(`^(?:the )?(?:output|stdout|stderr)(?: should not| does not| should)? contains? "((?:[^"\\]|\\.)*)"$`)
	reOutputMatches  = regexp.MustCompile(`^(?:output|stdout|stderr)(?: does not)? match(?:es)? pattern "((?:[^"\\]|\\.)*)"$`)
	reOutputEquals   = regexp.MustCompile(`^(?:output|stdout|stderr)(?: does not)? equals? "((?:[^"\\]|\\.)*)"$`)
	reOutputEmpty    = regexp.MustCompile(`^(?:output|stdout|stderr) is (not )?empty$`)

	reOutputJSONExists       = regexp.MustCompile(`^(output|stdout|stderr) as JSON at path "((?:[^"\\]|\\.)*)" exists$`)
	reOutputJSONEqualsBlock  = regexp.MustCompile(`^(output|stdout|stderr) as JSON at path "((?:[^"\\]|\\.)*)" equals:$`)
	reOutputJSONEqualsInline = regexp.MustCompile(`^(output|stdout|stderr) as JSON at path "((?:[^"\\]|\\.)*)" equals (.+)$`)

	reFileExists = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"(?:(?: does not)? exist[s]?)$`)
	reDirExists  = regexp.MustCompile(`^(?:the )?directory "((?:[^"\\]|\\.)*)"(?:(?: does not)? exist[s]?)$`)

	reFileContainsBlock = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"(?: does not)? contain(?:s)?:$`)
	reFileContains      = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"(?: does not)? contain(?:s)? "((?:[^"\\]|\\.)*)"$`)
	reFileMatches       = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"(?: does not)? match(?:es)? pattern "((?:[^"\\]|\\.)*)"$`)
	reFileEquals        = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"(?: does not)? equals? "((?:[^"\\]|\\.)*)"$`)
	reFileEmpty         = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)" is (not )?empty$`)

	reFileJSONExists       = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)" as JSON at path "((?:[^"\\]|\\.)*)" exists$`)
	reFileJSONEqualsBlock  = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)" as JSON at path "((?:[^"\\]|\\.)*)" equals:$`)
	reFileJSONEqualsInline = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)" as JSON at path "((?:[^"\\]|\\.)*)" equals (.+)$`)
)

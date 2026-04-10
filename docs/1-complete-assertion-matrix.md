# 1 — Complete the Assertion Matrix

## Problem

The assertion support is inconsistent across output streams and file content:

- `matches pattern` only works with `output`, not `stdout` or `stderr` individually
- No `matches pattern` for file content
- No `equals` (exact match) for output or files
- No `is empty` / `is not empty` for output or files

This forces users into workarounds like `output does not contain "..."` with guessed strings, or `matches pattern "^exact$"` instead of a direct equals check.

## Desired State

Every assertion type works uniformly across all sources:

| Assertion              | output | stdout | stderr | file |
|------------------------|:------:|:------:|:------:|:----:|
| contains               |   ✅   |   ✅   |   ✅   |  ✅  |
| does not contain       |   ✅   |   ✅   |   ✅   |  ✅  |
| matches pattern        |   ✅   |   ✅   |   ✅   |  ✅  |
| does not match pattern |   ✅   |   ✅   |   ✅   |  ✅  |
| equals                 |   ✅   |   ✅   |   ✅   |  ✅  |
| does not equal         |   ✅   |   ✅   |   ✅   |  ✅  |
| is empty               |   ✅   |   ✅   |   ✅   |  ✅  |
| is not empty           |   ✅   |   ✅   |   ✅   |  ✅  |
| as JSON                |   ✅   |   ✅   |   ✅   |  ✅  |
| exists / does not exist|   —    |   —    |   —    |  ✅  |

## Syntax

```gherkin
# matches pattern — extend to stdout/stderr/file
Then stdout matches pattern "v\d+\.\d+"
Then stderr does not match pattern "panic:"
Then file "output.log" matches pattern "^OK \d+ tests$"

# equals — exact full-content comparison (trimmed)
Then output equals "hello world"
Then file "VERSION" equals "1.3.0"
Then stderr does not equal "something"

# is empty / is not empty
Then output is empty
Then stderr is not empty
Then file "out.txt" is empty
```

`equals` trims leading/trailing whitespace before comparing (both sides). This avoids trailing-newline mismatches that would frustrate users.

## Implementation

### Step 1 — Fix `matches pattern` regex (one-line change)

File: `internal/assertions/assertions.go`, line 538.

Change:
```go
reOutputMatches = regexp.MustCompile(`^(?:output)(?: does not)? matches? pattern "((?:[^"\\]|\\.)*)"`)
```
To:
```go
reOutputMatches = regexp.MustCompile(`^(?:output|stdout|stderr)(?: does not)? matches? pattern "((?:[^"\\]|\\.)*)"`)
```

Update the match handler (around line 280) to use `combined(wr, text)` — it already does this, but verify it selects the correct stream based on the keyword.

### Step 2 — Add file `matches pattern`

Add regex:
```go
reFileMatches = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"(?: does not)? matches? pattern "((?:[^"\\]|\\.)*)"`)
```

Add handler before the `unknown Then assertion` fallback:
```go
if m := reFileMatches.FindStringSubmatch(text); m != nil {
    negate := strings.Contains(text, "does not match")
    path := unescapeString(m[1])
    content, err := dc.ReadFile(ctx, containerID, path)
    if err != nil {
        return fail("read file %q: %v", path, err)
    }
    pattern := unescapeString(m[2])
    re, err := compilePattern(pattern)
    if err != nil {
        return fail("invalid regex %q: %v", pattern, err)
    }
    matches := re.MatchString(content)
    if negate && matches {
        return fail("file %q unexpectedly matches pattern %q", path, pattern)
    }
    if !negate && !matches {
        return fail("file %q does not match pattern %q\nFile content:\n%s", path, pattern, content)
    }
    return pass()
}
```

### Step 3 — Add output `equals`

Add regexes:
```go
reOutputEquals = regexp.MustCompile(`^(?:output|stdout|stderr)(?: does not)? equals? "((?:[^"\\]|\\.)*)"`)
```

Handler: compare `strings.TrimSpace(haystack) == strings.TrimSpace(expected)`.

### Step 4 — Add file `equals`

Add regex:
```go
reFileEquals = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)"(?: does not)? equals? "((?:[^"\\]|\\.)*)"`)
```

Handler: read file, trim, compare.

### Step 5 — Add `is empty` / `is not empty`

Add regexes:
```go
reOutputEmpty = regexp.MustCompile(`^(?:output|stdout|stderr) is (not )?empty$`)
reFileEmpty   = regexp.MustCompile(`^file "((?:[^"\\]|\\.)*)" is (not )?empty$`)
```

Handler: check `strings.TrimSpace(content) == ""`.

### Step 6 — Tests

Add test cases to `internal/assertions/assertions_test.go` for each new assertion type. Follow the existing pattern (`TestUnknownAssertion`, etc.):

- `TestStdoutMatchesPattern` / `TestStderrMatchesPattern`
- `TestFileMatchesPattern` / `TestFileDoesNotMatchPattern`
- `TestOutputEquals` / `TestOutputDoesNotEqual`
- `TestFileEquals` / `TestFileDoesNotEqual`
- `TestOutputIsEmpty` / `TestOutputIsNotEmpty`
- `TestFileIsEmpty` / `TestFileIsNotEmpty`

### Step 7 — Integration specs

Add `specs/assertions.smoko` covering the new assertions with real container execution:
```gherkin
Feature: Extended Assertions
  Image: alpine:latest

  Scenario: stdout matches pattern
    When I run "echo 'version 1.2.3'"
    Then stdout matches pattern "version \d+\.\d+\.\d+"

  Scenario: file equals
    Given a file "VERSION" with content:
      1.3.0
    When I run "cat VERSION"
    Then file "VERSION" equals "1.3.0"

  Scenario: stderr is not empty
    When I run "sh -c 'echo error >&2'"
    Then stderr is not empty
```

### Step 8 — Update documentation

Update `README.md` DSL Reference and `skills/smoko/SKILL.md` Then section with the new assertion types.

## Batch-ability with `EvaluateAll`

The new file assertions (`file matches pattern`, `file equals`, `file is empty`) read file content. They should integrate with the existing `BatchFSCheck` optimization in `EvaluateAll` — batch the file reads into a single `docker exec` call rather than N individual reads. Follow the same pattern used by `reFileContains`.

# 2 — Variable Capture from Given Steps

## Problem

When test scenarios need to chain setup commands where a later command depends on output from an earlier one, users are forced into shell-script workarounds:

```gherkin
# What users have to write today (bira — 30+ occurrences):
Given a file "run.sh" with content:
  #!/bin/sh
  set -e
  bira init --name task-feat > /dev/null
  FID=$(bira feature add my-feat --json | grep -o '"id": "[^"]*"' | head -1 | sed 's/"id": "\(.*\)"/\1/')
  bira task add feat-task --feature "$FID" --json
When I run "sh run.sh"
```

This is hard to read, error-prone, and requires shell expertise inside what should be a declarative test DSL.

## Desired State

```gherkin
# What users should be able to write:
Given I run "bira init --name task-feat"
Given I run "bira feature add my-feat --json"
  And I save JSON path "$.id" as $FID
When I run "bira task add feat-task --feature $FID --json"
```

Three capture mechanisms, covering all real-world cases:

```gherkin
And I save output as $VAR                    # whole stdout, trimmed
And I save JSON path "$.field" as $VAR       # JSONPath extraction
And I save pattern "regex (group)" as $VAR   # first regex capture group
```

## Design Decisions

### Variable storage

Captured variables are injected as **environment variables** into the container's `.smoko_env` file. This means:
- `$VAR` works naturally in subsequent `Given I run` and `When I run` shell commands
- Consistent with existing `Given environment variable "X" is set to "Y"` mechanism
- No new interpolation engine needed — the shell handles it

### Scope

Variables are scoped to the **scenario**. Each scenario runs in a fresh container, so variables from one scenario never leak into another. Background steps can define variables that are available to all scenarios.

### `And` step binding

`And I save ...` is an `And` step following a `Given I run`. Per smoko's existing parser rules, `And` inherits the step type of the preceding keyword (`Given` in this case). The `And` step operates on the stdout captured from the immediately preceding `Given I run` step.

### Variable naming

Variables use `$NAME` syntax (uppercase convention, no braces required). The name is stored without the `$` prefix internally and exported as an environment variable.

## Syntax

```gherkin
# Save entire stdout (trimmed)
Given I run "git rev-parse HEAD"
  And I save output as $SHA

# Save a value extracted via JSONPath
Given I run "bira feature add my-feat --json"
  And I save JSON path "$.id" as $FID

# Save first capture group from a regex
Given I run "app --version"
  And I save pattern "v(\d+\.\d+\.\d+)" as $VER

# Use in subsequent steps
Given I run "bira task add my-task --feature $FID"
When I run "deploy --version $VER --ref $SHA"
```

## Implementation

### Step 1 — Track Given I run stdout

Currently, `Given I run` executes the command and discards stdout (it only checks the exit code). Change `RunGivenSteps` and `RunGiven` to **return stdout** from `Given I run` steps, stored as a field on the executor or passed alongside the step result.

File: `internal/executor/executor.go`

The `givenAction` struct needs to store the captured stdout after execution. Add a per-scenario `lastGivenStdout string` that is updated after each `Given I run` step.

### Step 2 — Parse capture steps

Add three new regex patterns for `And` steps that follow `Given I run`:

```go
reSaveOutput   = regexp.MustCompile(`^I save output as \$([A-Za-z_][A-Za-z0-9_]*)$`)
reSaveJSONPath = regexp.MustCompile(`^I save JSON path "((?:[^"\\]|\\.)*)" as \$([A-Za-z_][A-Za-z0-9_]*)$`)
reSavePattern  = regexp.MustCompile(`^I save pattern "((?:[^"\\]|\\.)*)" as \$([A-Za-z_][A-Za-z0-9_]*)$`)
```

These are recognized as `Given`-type steps (since `And` inherits from the preceding `Given`).

### Step 3 — Add capture action kind

Extend the `givenKind` enum:

```go
const (
    givenFile givenKind = iota
    givenDir
    givenRun
    givenNoop
    givenSaveOutput    // new
    givenSaveJSONPath  // new
    givenSavePattern   // new
)
```

### Step 4 — Implement extraction logic

In the Given step execution path, after recognizing a save step:

**save output**: `value = strings.TrimSpace(lastGivenStdout)`

**save JSON path**: Reuse the existing JSONPath evaluation from the assertions package. Parse `lastGivenStdout` as JSON, evaluate the path, convert the result to a string.

**save pattern**: Compile the regex, find the first match, extract capture group 1. Error if no match or no capture group.

### Step 5 — Inject as environment variable

After extraction, append `NAME=value` to the scenario's environment variables and re-write `.smoko_env` in the container. This reuses the existing `WriteEnvFile` function.

The capture step needs access to the container ID and the docker client to update `.smoko_env`. This is already available in the `RunGivenSteps` execution context.

### Step 6 — Validate error cases

- `And I save ...` without a preceding `Given I run` → error: "save step must follow a Given I run step"
- JSONPath doesn't match → error: "JSON path '$.id' not found in output"
- Regex has no capture group → error: "pattern must contain at least one capture group"
- Regex doesn't match → error: "pattern 'X' did not match output"

### Step 7 — Tests

Unit tests in `internal/executor/executor_test.go`:
- `TestSaveOutput` — captures trimmed stdout
- `TestSaveJSONPath` — extracts field from JSON
- `TestSavePattern` — extracts regex capture group
- `TestSaveWithoutPrecedingRun` — error case
- `TestSaveJSONPathNotFound` — error case
- `TestSavePatternNoMatch` — error case

### Step 8 — Integration specs

Add `specs/capture.smoko`:
```gherkin
Feature: Variable Capture
  Image: alpine:latest

  Scenario: Save output captures stdout
    Given I run "echo hello-world"
      And I save output as $GREETING
    When I run "echo got $GREETING"
    Then exit code is 0
    Then output contains "got hello-world"

  Scenario: Save JSON path extracts field
    Given I run "echo '{\"id\": \"abc-123\", \"name\": \"test\"}'"
      And I save JSON path "$.id" as $ID
    When I run "echo the-id-is-$ID"
    Then exit code is 0
    Then output contains "the-id-is-abc-123"

  Scenario: Save pattern extracts capture group
    Given I run "echo 'version: v1.2.3-beta'"
      And I save pattern "v([0-9]+\.[0-9]+\.[0-9]+)" as $VER
    When I run "echo version=$VER"
    Then exit code is 0
    Then output contains "version=1.2.3"
```

### Step 9 — Update documentation

Update `README.md` Given Steps section, `skills/smoko/SKILL.md` Given section, and add a "Variable Capture" pattern example.

## Notes

- The JSONPath extraction should reuse the same library already used by Then JSON assertions (`PaesslerAG/jsonpath` or similar — check go.mod)
- The regex cache (`sync.Map`) used in assertions can be reused for capture patterns
- Background steps with capture should work: the variable is set in the container's `.smoko_env` before scenario-specific Given steps run

# Smoko — Implementation Plan

## Overview

Smoko is a platform-agnostic smoke testing tool for CLI applications. Tests are written in a Gherkin-inspired BDD DSL (`.smoko` files) and executed in isolated Docker containers.

This plan covers the minimal viable implementation in Go.

---

## Key Design Decisions

| Topic | Decision | Rationale |
|---|---|---|
| Language | Go | Cross-platform, single binary distribution, strong Docker SDK support |
| DSL keywords | `Feature` / `Background` / `Scenario` / `Given` / `When` / `Then` / `And` / `But` | Full Gherkin-compatible readability |
| Multi-line blocks | Indentation-based (content ends at same/lower indent as enclosing keyword) | Matches DESIGN.md spec |
| Regex dialect | Go `regexp` (RE2) | Consistent with implementation language |
| File creation in `Given` | Write files inside container via `docker exec` — no host mounts | Keeps each scenario fully isolated |
| Timeout | 30s default per `When` step; configurable | Prevents test hangs |
| Project config file | `.smokorc` | Avoids `filepath.Glob("*.smoko")` matching the config file |
| Docker image resolution | Precedence: `--image` CLI flag → `Image:` in `.smoko` file → `.smokorc` | Most specific wins |
| Output format | Coloured text (default); ANSI degrades gracefully without a TTY | MVP only; TAP/JSON deferred |

---

## DSL Specification Additions (beyond DESIGN.md)

### `And` / `But` keywords

`And` and `But` are aliases that inherit the type of the preceding step keyword:

```
Given a file "input.txt" with content:
  hello world
And a file "config.json" with content:
  {"debug": true}
When I run "mytool --config config.json input.txt"
Then exit code is 0
And output contains "hello world"
But output does not contain "error"
```

### `Background:` section

Runs before every `Scenario` in the feature. The background is not reported as a separate step — its steps are merged into each scenario's setup:

```
Feature: My Tool
  Background:
    Given a file "config.json" with content:
      {"version": 1}

  Scenario: Normal run
    When I run "mytool"
    Then exit code is 0
```

### Per-feature image declaration

```
Feature: My Tool
  Image: myimage:latest

  Scenario: ...
```

The `Image:` line must appear in the feature description block (before any `Scenario:`).

### `.smokorc` project config (TOML)

```toml
image   = "myimage:latest"
timeout = 60   # seconds per When step
```

---

## Architecture

### Components

```
cmd/smoko/main.go
    └── internal/config/        — .smokorc loader
    └── internal/parser/        — DSL parser (lexer + AST)
    └── internal/docker/        — Docker SDK wrapper
    └── internal/executor/      — Scenario runner (Given/When coordination)
    └── internal/assertions/    — Then/And assertion evaluator
    └── internal/reporter/      — Text output and summary
```

### Execution flow per scenario

```
Load .smokorc + resolve image
       ↓
Parse *.smoko → []Feature{[]Scenario{[]Step}}
       ↓
For each Scenario:
  1. docker create container (with image, working dir)
  2. Run Background steps (if any) as Given steps
  3. Run Scenario Given steps → set up files, dirs, env vars inside container
  4. Run When step → exec command, capture stdout/stderr/exit code
  5. Evaluate Then/And assertions against captured output + container filesystem
  6. docker rm container
  7. Record pass/fail + diagnostics
       ↓
Reporter: print results + summary, exit 1 if any failure
```

---

## Project Structure

```
smoko/
├── cmd/
│   └── smoko/
│       └── main.go                   # cobra root + run command
├── internal/
│   ├── config/
│   │   └── config.go                 # .smokorc loader; Config{Image, Timeout}
│   ├── parser/
│   │   ├── types.go                  # AST: Feature, Scenario, Step, StepType, Block
│   │   ├── lexer.go                  # line tokeniser
│   │   └── parser.go                 # produce []Feature from tokens
│   ├── docker/
│   │   └── docker.go                 # create / exec / copyIn / remove
│   ├── executor/
│   │   ├── executor.go               # orchestrate scenario run
│   │   └── given.go                  # Given step handlers
│   ├── assertions/
│   │   └── assertions.go             # Then/And evaluators; Result{Pass, Message}
│   └── reporter/
│       └── reporter.go               # coloured text output + summary
├── testdata/
│   └── *.smoko                       # integration test fixtures
├── .smokorc                          # example project config
├── go.mod
├── go.sum
├── DESIGN.md
└── PLAN.md
```

---

## CLI Interface

### Commands

```
smoko run <path>              # path is a .smoko file or directory
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--image` | (none) | Docker image to use; overrides .smokorc and inline Image: |
| `--timeout` | 30 | Seconds to wait for each When step command |
| `--verbose` | false | Print stdout/stderr of passing scenarios too |
| `--fail-fast` | false | Stop after the first failed scenario |

### Exit codes

| Code | Meaning |
|---|---|
| 0 | All scenarios passed |
| 1 | One or more scenarios failed |
| 2 | Parse error or configuration error |

---

## MVP Assertion Reference

### Exit code

```
Then exit code is 0
Then exit code is 1
Then exit code is not 0
Then exit code is N
```

### Output

```
Then output contains "text"
Then stdout contains "text"
Then stderr contains "text"
Then output does not contain "text"
Then output matches pattern "regex"
Then output does not match pattern "regex"
```

(`output` = stdout + stderr combined)

### File system

```
Then file "path" exists
Then file "path" does not exist
Then file "path" contains "text"
Then directory "path" exists
```

---

## Given Step Reference (MVP)

```
Given a file "path" with content:
  <multi-line content>
Given a file "path" exists
Given the directory "path" exists
Given an empty working directory
Given environment variable "NAME" is set to "value"
```

---

## When Step Reference (MVP)

```
When I run "command arg1 arg2"
When I run "command" with input "stdin data"
When I run "command" expecting exit code N
```

---

## Todos (Implementation Order)

### Phase 1 — Foundation

**`project-init`**
- `go mod init github.com/nskut/smoko` (or appropriate module path)
- Add dependencies: `github.com/spf13/cobra`, `github.com/docker/docker/client`, `github.com/stretchr/testify`
- Scaffold `cmd/smoko/main.go` with a no-op `run` command
- Verify `go build ./...`

**`config-loader`** _(no deps)_
- Parse `.smokorc` TOML file from the working directory
- Fields: `image string`, `timeout int`
- Return zero-value Config when file is absent (not an error)
- Unit tests: file present, file absent, invalid TOML

### Phase 2 — Parser

**`parser-types`** _(no deps)_
- `StepType`: `Given`, `When`, `Then`, `And`, `But`
- `Step`: `Type StepType`, `Text string`, `Block string` (multi-line content)
- `Scenario`: `Name string`, `Steps []Step`
- `Feature`: `Name string`, `Description string`, `Image string`, `Background []Step`, `Scenarios []Scenario`

**`parser-lexer`** _(needs parser-types)_
- Line-by-line tokeniser
- Recognise keyword lines (`Feature:`, `Scenario:`, `Background:`, `Given`, `When`, `Then`, `And`, `But`, `Image:`, `#`)
- Detect indented continuation lines (multi-line block content)
- Strip trailing whitespace; skip blank lines and comment lines

**`parser-parser`** _(needs parser-lexer)_
- Consume token stream → `[]Feature`
- Handle `Background:` steps merged into each scenario at execution time
- Resolve `And`/`But` to the preceding non-And/But step type
- Multi-line block: collect indented lines following a step until indent level drops
- Clear error messages with file name and line number
- Unit tests covering: basic feature, background, multi-line content, And/But, comments, Image:

### Phase 3 — Docker

**`docker-interface`** _(no parser dep)_
- Wrap `github.com/docker/docker/client` SDK
- `CreateContainer(image string, env []string) (containerID string, error)`
- `ExecCommand(containerID, workdir, command string, stdin string, timeout time.Duration) (stdout, stderr string, exitCode int, error)`
- `WriteFile(containerID, path, content string) error` — write a file into the container at the given path
- `MakeDir(containerID, path string) error`
- `ReadFile(containerID, path string) (string, error)` — for file assertion checks
- `FileExists(containerID, path string) (bool, error)`
- `DirExists(containerID, path string) (bool, error)`
- `RemoveContainer(containerID string) error`

### Phase 4 — Executor

**`executor-given`** _(needs parser-parser, docker-interface)_
- Map each `Given` step text to docker operations:
  - `a file "X" with content:` → `WriteFile`
  - `a file "X" exists` → `WriteFile("")`
  - `the directory "X" exists` → `MakeDir`
  - `an empty working directory` → no-op (container starts empty)
  - `environment variable "X" is set to "Y"` → accumulate env var list for container creation
- Return error if step text doesn't match any known pattern

**`executor-when`** _(needs parser-parser, docker-interface)_
- Parse step text → command string + optional stdin + optional expected exit code
- Call `ExecCommand` with parsed values and configured timeout
- Store `stdout`, `stderr`, `exitCode`, `expectedExitCode` in `ScenarioResult`

### Phase 5 — Assertions

**`assertions-engine`** _(needs parser-parser)_
- `Evaluate(step Step, result ScenarioResult, docker DockerClient, containerID string) AssertionResult`
- `AssertionResult{Pass bool, Message string}` (message non-empty on failure)
- Implement all MVP assertion patterns (exit code, output, file system)
- Negation handled by detecting `does not` / `is not` in step text
- Pattern matching via `regexp.MustCompile`
- File content assertions: call `docker.ReadFile` then check content

### Phase 6 — Reporter

**`reporter`** _(needs assertions-engine)_
- `Reporter` collects results as scenarios complete
- On each scenario: print `  ✓ Scenario name` or `  ✗ Scenario name`
- On failure: print assertion failure message, expected vs actual
- `--verbose`: also print stdout/stderr on pass
- Final summary line: `3 passed, 1 failed (4 total)`
- ANSI colours: green for pass, red for fail; detect `NO_COLOR` / non-TTY

### Phase 7 — Wiring

**`cli-run-command`** _(needs all above)_
- `smoko run <path>` cobra command
- Discover `*.smoko` files (skip dotfiles) in directory, or accept single file
- Load `.smokorc` config → resolve image (flag > feature Image: > config)
- For each file: parse → for each scenario: create container → given → when → assert → remove container
- Feed results to reporter; exit 0 or 1

### Phase 8 — Integration Tests

**`integration-tests`** _(needs cli-run-command)_
- Use `alpine` as test image (available everywhere, has sh)
- Fixture files in `testdata/`:
  - `basic.smoko` — hello world echo, exit code check
  - `files.smoko` — file creation, content assertion
  - `envvars.smoko` — env var setting, output assertion
  - `failure.smoko` — intentionally failing scenario (test that Smoko reports failure correctly)
  - `background.smoko` — Background: shared setup
- Go test file that runs `smoko run testdata/` and checks exit code + stdout

---

## Dependencies Deferred to Future Iterations

- JSON / JSONPath assertions
- `--parallel N` execution
- `--format json` / `--format tap` output
- `--feature` / `--scenario` filtering
- Scenario tags (`@smoke`, `@slow`)
- File permission assertions (`has permissions "755"`)
- `When I wait N seconds`
- `smoko init` scaffolding command
- Test report trending / CI integration

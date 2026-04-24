# Smoko

Smoke testing tool for CLI apps. BDD-style `.smoko` DSL executed in isolated Docker containers. Module: `github.com/nskut/smoko`.

## Commands

```bash
make build           # binary → repo root
make install         # → GOPATH/bin
make test-local      # unit tests (no Docker)
make test            # unit tests in Docker
```

## Architecture Overview

### Core Components

**Parser** (`src/internal/parser/`)
- Lexer: Line-by-line tokenization with support for multi-line indented blocks
- Parser: Recursive-descent parser that produces an AST of Feature/Scenario/Step objects
- Handles Gherkin-like syntax: Feature, Background, Scenario, Given/When/Then/And/But
- Special handling: `And`/`But` inherit step type from preceding step; `#` inside indented blocks is treated as content (not comments)
- CLI validation enforces strict scenario structure before `--list` or Docker execution: Background contains only Given steps; each scenario has Given setup, exactly one When, then one or more Then assertions

**Docker** (`src/internal/docker/`)
- Wraps Docker SDK (`github.com/docker/docker`)
- Container lifecycle: create (with env vars), exec (with timeout errors), write files (via tar), read files
- Each scenario runs in a fresh container for isolation
- Working directory: `/smoko-work` inside container
- `WriteFiles(ctx, containerID, []FileEntry)` uploads multiple files in a single tar archive
- `BatchFSCheck(ctx, containerID, []FSCheck)` runs multiple filesystem checks in one `docker exec`
- `PullIfMissing` caches results in `sync.Map` — same image is only inspected once per run

**Executor** (`src/internal/executor/`)
- Coordinates scenario execution:
  1. Collects environment variables from Given steps
  2. Creates container with those env vars
  3. Runs Given steps — **batched** via `RunGivenSteps`: file writes go into one tar upload, dir creation uses separate exec only when needed
  4. Tracks the effective working directory (starts at `/smoko-work`; updated by `Given the working directory is` steps)
  5. Captures stdout from `Given I run` steps and writes captured variables to `.smoko_env`
  6. Runs When step from the effective working directory (captures stdout/stderr/exit code)
  7. Evaluates Then/And assertions
- `RunGivenSteps(ctx, dc, containerID, steps, timeout, env)` returns `(workdir string, error)` — the effective working directory after all Given steps; preferred API
- `RunGiven` (single step) is kept for compatibility, also returns `(string, error)`
- `RunWhen(ctx, dc, containerID, step, workdir, timeout)` accepts the effective workdir returned by `RunGivenSteps`
- Variable capture: `And I save output/JSON path/pattern as $VAR` steps append to `.smoko_env`
- File operations are done via Docker exec/tar, not host mounts
- Fuzzy hints: unknown Given/When steps suggest closest known pattern via `src/internal/hints`

**Assertions** (`src/internal/assertions/`)
- Evaluates Then/And steps against captured WhenResult and container filesystem
- Regex pattern matching via Go's `regexp` (RE2 dialect); patterns cached in `sync.Map`
- `EvaluateAll(ctx, steps, wr, dc, containerID)` is the preferred API — batches all filesystem checks into one docker exec before evaluating
- `Evaluate(ctx, step, wr, dc, containerID)` handles a single step (used as fallback)
- Supports: exit codes, output contains/matches/equals/empty, file/directory existence, file content, JSON path assertions
- Fuzzy hints: unknown Then steps suggest closest known pattern via `src/internal/hints`

**Reporter** (`src/internal/reporter/`)
- Collects scenario results and prints colored output
- Per-scenario: `✓ feature / scenario` or `✗ feature / scenario`
- Failure details: assertion failures, actual vs expected
- Summary line: `N passed, M failed (total)`
- ANSI colors detected (NO_COLOR env var, non-TTY detection)
- Thread-safe: `Add()` is guarded by a `sync.Mutex` for use with `--parallel`

**Config** (`src/internal/config/`)
- Loads `.smokorc` (TOML format)
- Fields: `image` (string), `timeout` (int seconds), `build` (string — command to build Docker image)
- Image resolution precedence: `--image` flag > `Image:` in .smoko > `.smokorc` default
- When `build` is set and `--no-build` is not passed, the build command runs before image pull; `--list` skips the build command

**Hints** (`src/internal/hints/`)
- `Suggest(text, patterns []string) string` — Levenshtein distance on normalised step text
- Used by executor and assertions to suggest the closest known step pattern on unknown input

### Data Flow

```
.smoko files
    ↓
Parse → []Feature{[]Scenario{[]Step}}
    ↓
Run build command (if .smokorc has build = "..." and --no-build not set, except for --list)
    ↓
Pull unique images (cached via sync.Map)
    ↓
For each Scenario (optionally parallel via --parallel N):
  1. Collect env vars from Given steps
  2. docker.CreateContainer(image, env)
  3. Batch all Given file writes → docker.WriteFiles (single tar; setup paths are confined to `/smoko-work`)
  4. Run mkdir for explicit directory steps (also confined to `/smoko-work`)
  5. On "Given the working directory is": resolve under `/smoko-work`, exec `test -d` in container, update effective workdir
  6. Run Given I run steps (using effective workdir); for each I save step → append var to .smoko_env
  7. Run Scenario When step (using effective workdir) → capture WhenResult
  8. Batch Then filesystem checks → docker.BatchFSCheck (single exec)
  9. Evaluate all Then steps against WhenResult + FSCheck results
  10. docker.RemoveContainer
    ↓
Reporter aggregates & prints results (thread-safe)
    ↓
Exit code: 0 (pass), 1 (fail), 2 (error)
```

## Code Organization

### Key Files

- `src/cmd/smoko/main.go`: CLI entry point (Cobra), orchestrates test discovery, strict scenario validation, parallel execution, container lifecycle, reporting; `--list` flag validates and prints scenarios without running the build command or Docker; defaults to `specs/` path
- `src/internal/parser/types.go`: AST types (StepType, Step, Scenario, Feature)
- `src/internal/parser/lexer.go`: Tokenization with stateful block detection
- `src/internal/parser/parser.go`: Recursive-descent parser
- `src/internal/docker/docker.go`: Docker SDK wrapper; includes `WriteFiles` (batched tar upload), `BatchFSCheck` (batched filesystem checks), `PullIfMissing` (with sync.Map cache)
- `src/internal/executor/executor.go`: Given/When handlers; `RunGivenSteps` batches file writes, handles variable capture, tracks effective workdir and returns it; `RunWhen` accepts workdir; `knownGivenPatterns`/`knownWhenPatterns` for fuzzy hints
- `src/internal/assertions/assertions.go`: Then assertion evaluators; `EvaluateAll` batches filesystem checks; regex cache via sync.Map; `knownThenPatterns` for fuzzy hints
- `src/internal/hints/hints.go`: `Suggest()` function using Levenshtein distance on normalised step text
- `src/internal/reporter/reporter.go`: Output formatting with colors; thread-safe `Add()` for parallel use

### Testing

- `src/internal/parser/parser_test.go`: Parser unit tests
- `src/internal/config/config_test.go`: Config unit tests
- `specs/*.smoko`: Integration fixtures (basic.smoko, files.smoko, workdir.smoko, etc.)

## Common Patterns

### Adding a New Given Step

1. Define a regex pattern in `executor.go`: `var reNewPattern = regexp.MustCompile(...)`
2. Add a case in `classifyGivenStep()` to match and return a `givenAction` with the appropriate `givenKind`
3. If the kind requires a new op type, add a `givenOpKind` constant and handle it in `buildGivenOps()` and `RunGivenSteps()`
4. Add the step pattern string to `knownGivenPatterns` for fuzzy hints
5. Example: `Given the directory "X" exists` → `givenDir` kind → `givenOpMakeDir` → `dc.MakeDir(ctx, containerID, path)`

### Adding a New Then Assertion

1. Define a regex pattern in `assertions.go`: `var reNewAssertion = regexp.MustCompile(...)`
2. Add a case in `Evaluate()` to match and check
3. Return `pass()` on success, `fail(format, args...)` on failure
4. Support negation by checking `strings.Contains(text, "does not")`

### Handling Multi-line Content

- Parser collects lines indented deeper than the keyword line
- Blank lines end the block
- Lexer treats indented content as `TokBlock` (even if it starts with `#`)
- Step.Block contains the joined content

### Working with Docker Containers

- Given file/directory setup and working-directory paths are confined to `/smoko-work`; relative paths resolve under `/smoko-work`, absolute setup paths must already be under `/smoko-work`, and `..` path segments are rejected
- Then file/directory assertion paths resolve relative paths under `/smoko-work`; absolute assertion paths are allowed for read/check use cases, but `..` path segments are rejected
- Commands are wrapped: `source .smoko_env; <user-command>` to inject env vars
- File writes use tar archives (no host mounts for isolation)
- Exit codes are captured; timed-out execs return an error; timeouts default to 1s (configurable)

## Testing Guidelines

### Unit Tests
- Keep in same package as code
- Use `*_test.go` files
- Use `github.com/stretchr/testify/assert` and `require`

### Integration Tests
- Use `.smoko` files in `specs/`
- Run via `go test` or direct invocation: `smoko run specs/`
- Use Alpine Linux image for fast pulls

### Docker Dependency
- Tests requiring Docker must handle absence gracefully
- Assume Docker daemon is running on localhost
- Clean up containers aggressively (use defer RemoveContainer)

## Common Issues & Fixes

**Issue:** Parser doesn't recognize my step
- Check step format matches the regex pattern
- Ensure it's indented under a Scenario

**Issue:** File content includes `#` and it's treated as a comment
- This is correct if not indented deeper than the step keyword
- Indent it further: `Given a file "X" with content:` (colon triggers block mode)

**Issue:** Container times out
- Increase `--timeout` or set `timeout` in `.smokorc`
- Check if image is pulling (use `--verbose`)
- Timed-out setup/action commands fail the scenario; deferred container cleanup removes the container

**Issue:** Environment variables not visible in When step
- Verify syntax: `Given environment variable "NAME" is set to "value"`
- Check that the container has `/smoko-work/.smoko_env` written

**Issue:** CLI tool can't find project root / must run from a subdirectory
- Use `Given the working directory is "path/to/subdir"` before the `When` step
- The directory must already exist; create it first with `Given the directory "..." exists`
- The working directory must stay under `/smoko-work`; use environment variables such as `TMPDIR=/smoko-work/tmp` when a CLI would otherwise write to `/tmp`

## Performance Considerations

- Each scenario creates a fresh container (adds ~100-500ms per test)
- **Given file writes are batched**: all files in a scenario are uploaded in one tar archive via `WriteFiles`, avoiding N individual exec+copy round-trips
- **Then filesystem assertions are batched**: `EvaluateAll` groups file-exists/dir-exists/read-file checks into a single `docker exec` per scenario
- **Image pull verification is cached**: `PullIfMissing` uses a `sync.Map` on `docker.Client` so the same image is only inspected once per run
- **Regex patterns are cached**: user-provided patterns in `output matches pattern` assertions are compiled once and reused via `sync.Map` in the assertions package
- **Parallel execution**: use `--parallel N` (or `--parallel 0` for auto) to run scenarios concurrently; each scenario is already isolated in its own container

## Style & Conventions

- Go code: Standard `gofmt` formatting
- Error messages: Include context (file name, line number, actual vs expected)
- Function names: Descriptive, no underscores
- Comments: Only on non-obvious logic
- Regex patterns: Anchored where possible, use named groups sparingly

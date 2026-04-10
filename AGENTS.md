# Smoko

Smoke testing tool for CLI apps. BDD-style `.smoko` DSL executed in isolated Docker containers. Module: `github.com/nskut/smoko`. For DSL usage and examples, see [`skills/smoko/SKILL.md`](skills/smoko/SKILL.md).

## Commands

```bash
make build           # binary → repo root
make install         # → GOPATH/bin
make test-local      # unit tests (no Docker)
make test            # unit tests in Docker
```

## Architecture Overview

### Core Components

**Parser** (`internal/parser/`)
- Lexer: Line-by-line tokenization with support for multi-line indented blocks
- Parser: Recursive-descent parser that produces an AST of Feature/Scenario/Step objects
- Handles Gherkin-like syntax: Feature, Background, Scenario, Given/When/Then/And/But
- Special handling: `And`/`But` inherit step type from preceding step; `#` inside indented blocks is treated as content (not comments)

**Docker** (`internal/docker/`)
- Wraps Docker SDK (`github.com/docker/docker`)
- Container lifecycle: create (with env vars), exec (with timeout), write files (via tar), read files
- Each scenario runs in a fresh container for isolation
- Working directory: `/smoko-work` inside container
- `WriteFiles(ctx, containerID, []FileEntry)` uploads multiple files in a single tar archive
- `BatchFSCheck(ctx, containerID, []FSCheck)` runs multiple filesystem checks in one `docker exec`
- `PullIfMissing` caches results in `sync.Map` — same image is only inspected once per run

**Executor** (`internal/executor/`)
- Coordinates scenario execution:
  1. Collects environment variables from Given steps
  2. Creates container with those env vars
  3. Runs Given steps — **batched** via `RunGivenSteps`: file writes go into one tar upload, dir creation uses separate exec only when needed
  4. Captures stdout from `Given I run` steps and writes captured variables to `.smoko_env`
  5. Runs When step (captures stdout/stderr/exit code)
  6. Evaluates Then/And assertions
- `RunGivenSteps(ctx, dc, containerID, steps, timeout, env)` is the preferred API; `RunGiven` (single step) is kept for compatibility
- Variable capture: `And I save output/JSON path/pattern as $VAR` steps append to `.smoko_env`
- File operations are done via Docker exec/tar, not host mounts
- Fuzzy hints: unknown Given/When steps suggest closest known pattern via `internal/hints`

**Assertions** (`internal/assertions/`)
- Evaluates Then/And steps against captured WhenResult and container filesystem
- Regex pattern matching via Go's `regexp` (RE2 dialect); patterns cached in `sync.Map`
- `EvaluateAll(ctx, steps, wr, dc, containerID)` is the preferred API — batches all filesystem checks into one docker exec before evaluating
- `Evaluate(ctx, step, wr, dc, containerID)` handles a single step (used as fallback)
- Supports: exit codes, output contains/matches/equals/empty, file/directory existence, file content, JSON path assertions
- Fuzzy hints: unknown Then steps suggest closest known pattern via `internal/hints`

**Reporter** (`internal/reporter/`)
- Collects scenario results and prints colored output
- Per-scenario: `✓ feature / scenario` or `✗ feature / scenario`
- Failure details: assertion failures, actual vs expected
- Summary line: `N passed, M failed (total)`
- ANSI colors detected (NO_COLOR env var, non-TTY detection)
- Thread-safe: `Add()` is guarded by a `sync.Mutex` for use with `--parallel`

**Config** (`internal/config/`)
- Loads `.smokorc` (TOML format)
- Fields: `image` (string), `timeout` (int seconds), `build` (string — command to build Docker image)
- Image resolution precedence: `--image` flag > `Image:` in .smoko > `.smokorc` default
- When `build` is set and `--no-build` is not passed, the build command runs before image pull

**Hints** (`internal/hints/`)
- `Suggest(text, patterns []string) string` — Levenshtein distance on normalised step text
- Used by executor and assertions to suggest the closest known step pattern on unknown input

### Data Flow

```
.smoko files
    ↓
Parse → []Feature{[]Scenario{[]Step}}
    ↓
Run build command (if .smokorc has build = "..." and --no-build not set)
    ↓
Pull unique images (cached via sync.Map)
    ↓
For each Scenario (optionally parallel via --parallel N):
  1. Collect env vars from Given steps
  2. docker.CreateContainer(image, env)
  3. Batch all Given file writes → docker.WriteFiles (single tar)
  4. Run mkdir for explicit directory steps
  5. Run Given I run steps; for each I save step → append var to .smoko_env
  6. Run Scenario When step → capture WhenResult
  7. Batch Then filesystem checks → docker.BatchFSCheck (single exec)
  8. Evaluate all Then steps against WhenResult + FSCheck results
  9. docker.RemoveContainer
    ↓
Reporter aggregates & prints results (thread-safe)
    ↓
Exit code: 0 (pass), 1 (fail), 2 (error)
```

## Code Organization

### Key Files

- `cmd/smoko/main.go`: CLI entry point (Cobra), orchestrates test discovery, parallel execution, container lifecycle, reporting; `--list` flag prints scenarios without running Docker; defaults to `specs/` path
- `internal/parser/types.go`: AST types (StepType, Step, Scenario, Feature)
- `internal/parser/lexer.go`: Tokenization with stateful block detection
- `internal/parser/parser.go`: Recursive-descent parser
- `internal/docker/docker.go`: Docker SDK wrapper; includes `WriteFiles` (batched tar upload), `BatchFSCheck` (batched filesystem checks), `PullIfMissing` (with sync.Map cache)
- `internal/executor/executor.go`: Given/When handlers; `RunGivenSteps` batches file writes and handles variable capture; `knownGivenPatterns`/`knownWhenPatterns` for fuzzy hints
- `internal/assertions/assertions.go`: Then assertion evaluators; `EvaluateAll` batches filesystem checks; regex cache via sync.Map; `knownThenPatterns` for fuzzy hints
- `internal/hints/hints.go`: `Suggest()` function using Levenshtein distance on normalised step text
- `internal/reporter/reporter.go`: Output formatting with colors; thread-safe `Add()` for parallel use

### Testing

- `internal/parser/parser_test.go`: Parser unit tests
- `internal/config/config_test.go`: Config unit tests
- `specs/*.smoko`: Integration fixtures (basic.smoko, files.smoko, etc.)

## Common Patterns

### Adding a New Given Step

1. Define a regex pattern in `executor.go`: `var reNewPattern = regexp.MustCompile(...)`
2. Add a case in `RunGiven()` to match and execute
3. Example: `Given the directory "X" exists` → `dc.MakeDir(ctx, containerID, path)`

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

- All paths are absolute from `/smoko-work` (or prefixed with it)
- Commands are wrapped: `source .smoko_env; <user-command>` to inject env vars
- File writes use tar archives (no host mounts for isolation)
- Exit codes are captured; timeouts default to 1s (configurable)

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

**Issue:** Environment variables not visible in When step
- Verify syntax: `Given environment variable "NAME" is set to "value"`
- Check that the container has `/smoko-work/.smoko_env` written

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

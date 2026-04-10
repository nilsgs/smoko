---
name: smoko
description: "Write, review, and debug Smoko `.smoko` smoke tests for CLI applications. Use when the agent needs to create or update Given/When/Then scenarios, explain supported DSL clauses, add assertions, structure shared setup with Background, or troubleshoot Smoko test failures running in Docker containers."
---

# Smoko

Use this guide to write correct Smoko scenarios and stay within the DSL the tool actually supports.

## Core model

- Treat each `Scenario` as one isolated test run in a fresh Docker container.
- Use `Background` for setup shared by every scenario in the feature.
- Expect the working directory inside the container to be `/smoko-work`.
- Execute `Given` steps in source order.
- Use a single `When` step as the action under test.
- Use `Then` and inherited `And` or `But` steps for assertions. `And`/`But` inherit their type from the preceding keyword (`Given`, `When`, or `Then`).

## Supported structure

```gherkin
Feature: Feature Name
  Optional description

  Image: alpine:latest

  Background:
    Given a file "config.txt" with content:
      default configuration

  Scenario: Scenario name
    Given environment variable "MODE" is set to "test"
    When I run "my-cli"
    Then exit code is 0
```

Image resolution precedence:
1. `--image`
2. `Image:` in the `.smoko` file
3. `.smokorc`

## Given

Use `Given` for setup only.

### Create a file with content

```gherkin
Given a file "path/to/file.txt" with content:
  line 1
  line 2
```

### Create an empty file

```gherkin
Given a file "path/to/file.txt" exists
```

### Create a directory

```gherkin
Given the directory "path/to/dir" exists
```

### Set the working directory

```gherkin
Given the working directory is "path/to/subdir"
```

Behavior:
- Changes the working directory for all subsequent `Given I run` and `When I run` steps in the scenario.
- The path is relative to the scenario root (`/smoko-work`); resolved as `/smoko-work/<path>`.
- The directory must already exist; if not, the scenario fails immediately with a clear error.
- Resets to `/smoko-work` automatically at the start of each new scenario.
- `Then` file and directory assertions always use paths relative to `/smoko-work`, regardless of this step.

Use this step when the CLI under test needs to run from a subdirectory (e.g., a tool that walks up to find a project root):

```gherkin
Scenario: Detects repo root from nested directory
  Given the directory "src/App" exists
  Given a file "src/App/App.csproj" with content:
    <Project Sdk="Microsoft.NET.Sdk" />
  Given the working directory is "src/App"
  When I run "mycli status"
  Then exit code is 0
  Then file "src/App/App.csproj" exists
```

### Set an environment variable

```gherkin
Given environment variable "VAR_NAME" is set to "value"
```

### Run a setup command

```gherkin
Given I run "cp source.txt target.txt"
```

Behavior:
- Run the command in `/smoko-work`.
- Source `.smoko_env` first if it exists.
- Fail the scenario immediately if the command exits non-zero.
- Use this for imperative setup, not for the main behavior under test.

### Capture output into a variable

Immediately after a `Given I run` step, save the output (or part of it) into an environment variable for use in subsequent steps.

```gherkin
# Save trimmed stdout as a variable
Given I run "my-cli version"
And I save output as $VERSION

# Save a JSON field from stdout
Given I run "my-cli info --json"
And I save JSON path "$.version" as $VERSION

# Save a regex capture group (first group)
Given I run "my-cli version"
And I save pattern "v([0-9.]+)" as $VERSION
```

The variable is written to `.smoko_env` immediately, making it available to subsequent `Given I run`, `When I run`, and file content steps via shell expansion.

Save steps must immediately follow a `Given I run` step. Multiple saves after the same run are allowed:

```gherkin
Given I run "my-cli info --json"
And I save JSON path "$.name" as $APP_NAME
And I save JSON path "$.version" as $APP_VERSION
```

## When

Use exactly one `When` step per scenario.

### Run a command

```gherkin
When I run "command arg1 arg2"
```

### Run a command with stdin

```gherkin
When I run "command" with input "stdin data"
```

### Run a command with an expected exit code annotation

```gherkin
When I run "command" expecting exit code 1
```

`When` captures stdout, stderr, and exit code.

## Then

### Exit code

```gherkin
Then exit code is 0
Then exit code is not 1
```

### Output contains text

```gherkin
Then output contains "expected text"
Then output does not contain "error"
Then stdout contains "expected stdout text"
Then stderr contains "expected stderr text"
```

### Output matches a regex pattern

```gherkin
Then output matches pattern "version \\d+\\.\\d+\\.\\d+"
Then stdout matches pattern "v\\d+\\.\\d+"
Then stderr does not match pattern "panic:"
Then file "output.log" matches pattern "^OK \\d+ tests$"
```

Use Go `regexp` syntax (RE2). Both `match` and `matches` are accepted.

### Output equals (exact match)

```gherkin
Then output equals "exact value"
Then stdout equals "hello"
Then stderr does not equal "something"
```

Trims leading/trailing whitespace before comparing, so trailing newlines are ignored.

### Empty / not empty

```gherkin
Then output is empty
Then stderr is empty
Then stdout is not empty
Then file "out.txt" is empty
Then file "out.txt" is not empty
```

### JSONPath assertions

```gherkin
Then output as JSON at path "$.user.name" exists
Then stdout as JSON at path "$.ok" equals true
Then file "result.json" as JSON at path "$.items[0].id" equals 123
Then file "result.json" as JSON at path "$.items" equals:
  [1, 2, 3]
```

Rules:
- Use dollar-style JSONPath such as `$.user.name`.
- `equals` compares parsed JSON values, not stringified text.
- Use JSON literals inline for scalars and compact values.
- Use block JSON after `equals:` for arrays or objects.
- `equals` requires the JSONPath to resolve to exactly one value.

### File existence

```gherkin
Then file "path/to/file.txt" exists
Then file "path/to/file.txt" does not exist
```

### File content

```gherkin
Then file "path/to/file.txt" contains "expected content"
Then file "path/to/file.txt" does not contain "unexpected text"
Then file "path/to/file.txt" matches pattern "^\\d+\\.\\d+\\.\\d+$"
Then file "path/to/file.txt" equals "exact content"
```

Block form is also supported:

```gherkin
Then file "config.json" contains:
  "enabled": true
```

### Directory existence

```gherkin
Then directory "path/to/dir" exists
Then directory "path/to/dir" does not exist
```

## Patterns

### Working directory for directory-aware CLIs

Use `Given the working directory is "..."` instead of `sh -c 'cd ... && ...'` in the `When` step:

```gherkin
# Before — embeds shell logic in the action step, POSIX-only:
When I run "sh -c 'cd src/App && mycli bump --major'"

# After — clean Given/When/Then separation:
Given the working directory is "src/App"
When I run "mycli bump --major"
```

`Then` file paths remain relative to `/smoko-work` (the scenario root), not the working directory.

### Sequential setup with variable capture

Use `Given I run` + `And I save` to chain setup steps that depend on each other's output:

```gherkin
Scenario: Task added to a feature
  Given I run "mycli init --name my-project"
  Given I run "mycli feature add my-feature --json"
    And I save JSON path "$.id" as $FID
  When I run "mycli task add my-task --feature $FID --json"
  Then exit code is 0
  Then output as JSON at path "$.title" equals "my-task"
```

Don't wrap sequential commands in `sh -c` chains when `Given I run` handles it directly.

### Prefer JSONPath over substring matching for structured output

```gherkin
# Prefer this:
Then output as JSON at path "$.title" equals "my-task"
Then output as JSON at path "$.status" equals "todo"

# Over this:
Then output contains "\"title\": \"my-task\""
Then output contains "\"status\": \"todo\""
```

JSONPath is whitespace-independent, validates structure, and is more readable.

### Always check both exit code and message for error cases

```gherkin
Scenario: Rejects invalid input
  When I run "mycli process --format invalid"
  Then exit code is not 0
  Then stderr contains "unsupported format"
```

Checking only the exit code can mask wrong-reason failures.

### Helper scripts in Docker images

For complex test utilities, bake a helper script into the image rather than inlining shell logic in specs:

```dockerfile
# In Dockerfile.test
COPY specs/helpers/seed.sh /usr/local/bin/seed
```

```gherkin
Given I run "seed init-repo myrepo"
```

This keeps specs readable and moves shell complexity into a maintainable script.

### Shared setup in Background

```gherkin
Feature: Configured CLI
  Background:
    Given a file "config.json" with content:
      {"mode":"test"}
    Given I run "cp config.json config.local.json"
```

### Imperative setup before the main action

```gherkin
Scenario: CLI consumes generated artifact
  Given a file "input.txt" with content:
    hello from setup
  Given I run "cp input.txt output.txt"
  When I run "cat output.txt"
  Then exit code is 0
  Then output contains "hello from setup"
```

### Environment-dependent behavior

```gherkin
Scenario: CLI respects environment variables
  Given environment variable "DEBUG" is set to "true"
  When I run "my-cli start"
  Then exit code is 0
  Then output contains "Debug mode enabled"
```

## Debugging guidance

- If a `Given the working directory is` step fails, the directory does not yet exist in the container — add a `Given the directory "..." exists` step before it.
- If a `Given` step fails before `When`, inspect the setup command or path assumptions first.
- If a file assertion fails, remember paths are relative to `/smoko-work` unless explicitly absolute.
- If regex assertions fail, verify the step uses `matches pattern`, not just `matches`.
- If a JSON assertion fails, check whether the source is valid JSON, whether the JSONPath is valid, and whether `equals` matched exactly one node.
- If shared setup is repeated across scenarios, move it into `Background`.
- If the setup is imperative shell work, prefer `Given I run "..."` over abusing `When`.
- If a scenario times out, remember the default timeout is `1` second and increase `--timeout` or `.smokorc` only for the slow path.

## Performance

- Prefer `smoko run specs/ --parallel 0` for normal runs so Smoko auto-sizes concurrency.
- Keep the default `1` second timeout unless the command or image is genuinely slow.
- Use `Background` for repeated setup instead of duplicating expensive `Given` steps in every scenario.
- Prefer file-based setup steps over long shell setup sequences when both express the same intent.

## Commands

```bash
smoko run test.smoko
smoko run specs/
smoko run             # defaults to specs/ directory
smoko run specs/ --parallel 0
smoko run test.smoko --image alpine:latest
smoko run test.smoko --verbose
smoko run test.smoko --fail-fast
smoko run specs/ --list    # list scenarios without running
smoko run specs/ --no-build   # skip build step even if .smokorc has build = "..."
```

`timeout` in `.smokorc` or `--timeout` applies to setup and action commands. The built-in default is `1` second.

## .smokorc

```toml
image   = "myimage:latest"
timeout = 5
build   = "docker build -f Dockerfile.test -t myimage:latest ."
```

When `build` is set, smoko runs the command before pulling or running any scenarios. Build output streams to the terminal. Use `--no-build` to skip when the image is already current.

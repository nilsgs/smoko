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
- Use `Then` and inherited `And` or `But` steps for assertions.

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

### Declare an empty working directory

```gherkin
Given an empty working directory
```

This is effectively a no-op because each scenario already starts in a fresh container.

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
Then output does not match pattern "panic:"
```

Use Go `regexp` syntax (RE2).

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

### JSON output assertions

```gherkin
Scenario: CLI emits nested JSON
  Given a file "stdout.json" with content:
    {"user":{"name":"Alice","active":true}}
  When I run "cat stdout.json"
  Then exit code is 0
  Then output as JSON at path "$.user.name" equals "Alice"
  Then output as JSON at path "$.user.active" equals true
```

### JSON file assertions

```gherkin
Scenario: CLI writes a JSON file
  Given a file "result.json" with content:
    {"items":[1,2,3]}
  When I run "cat result.json"
  Then exit code is 0
  Then file "result.json" as JSON at path "$.items" equals:
    [1, 2, 3]
```

## Debugging guidance

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
smoko run specs/ --parallel 0
smoko run test.smoko --image alpine:latest
smoko run test.smoko --verbose
smoko run test.smoko --fail-fast
```

`timeout` in `.smokorc` or `--timeout` applies to setup and action commands. The built-in default is `1` second.

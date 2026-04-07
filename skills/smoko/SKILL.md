---
name: smoko
description: "Teaches agents how to write, understand, and execute smoko BDD-style smoke tests for CLI applications. Covers DSL syntax, step types, assertions, and common testing patterns. Smoko is a platform-agnostic tool that runs tests in isolated Docker containers."
compatibility: "Requires smoko CLI and Docker"
metadata:
  category: "testing"
  tags: "smoke-tests, bdd, gherkin, cli-testing, docker, testing-tools"
---

# Smoko User Guide

Smoko is a platform-agnostic smoke testing tool for CLI applications. It allows you to write BDD-style test specifications in `.smoko` files and execute them in isolated Docker containers with comprehensive assertions.

## What is Smoko?

**Core capabilities:**
- Write human-readable BDD-style tests using Gherkin-inspired syntax
- Test any CLI tool in any language
- Execute tests in isolated Docker containers for consistent, repeatable results
- Comprehensive assertions: exit codes, output matching (regex), file system checks
- Works consistently on Windows, macOS, and Linux

**Why use Smoko?**
- **Readable**: Tests read like documentation (Given/When/Then structure)
- **Isolated**: Each scenario runs in a fresh Docker container, no side effects
- **Comprehensive**: Assert on exit codes, stdout, stderr, file content, directory structure
- **Language-agnostic**: Test any CLI tool regardless of language
- **Flexible image support**: Use any Docker image that contains your CLI tool

## Core Concepts

### Feature
A `.smoko` file begins with a Feature declaration that groups related scenarios.

```
Feature: Feature Name
  Optional description of what this feature tests
  
  Image: docker-image:tag
```

The optional `Image:` line specifies which Docker image to use as the container for all scenarios in the feature. This can be overridden via CLI flags or `.smokorc` configuration.

### Background (Optional)
A Background section defines setup steps that run before each scenario in the feature. All Background steps are treated as Given steps (setup).

```
Background:
  Given environment variable "VAR" is set to "value"
  Given a file "config.txt" with content:
    default configuration
```

Background is useful for common setup shared across multiple scenarios.

### Scenario
A scenario is a single test case that follows the Given/When/Then structure:

```
Scenario: Scenario description
  Given [one or more setup steps]
  When [single action step]
  Then [one or more assertion steps]
```

Each scenario:
1. Runs in a fresh Docker container (isolated from other scenarios)
2. Has its own working directory (`/smoko-work` inside the container)
3. Executes Given steps in order to set up the environment
4. Executes the When step and captures output, exit code, and stderr
5. Evaluates Then steps as assertions against the captured output

### Steps
Steps are the building blocks of scenarios. Each step has a type (Given, When, Then) and text that defines the action or assertion.

**Step types:**
- **Given** — Setup: create files, set environment variables, create directories, etc.
- **When** — Action: run a command and capture its output, exit code, and stderr
- **Then/And** — Assertion: verify the result (exit code, output, files, etc.)

**Step modifiers:**
- **And** — Continues the previous step type (And after Given is a Given; And after Then is a Then)
- **But** — Negation modifier (but rare in practice)

### Multi-line Content
Given steps that create files support multi-line indented content:

```
Given a file "script.sh" with content:
  #!/bin/bash
  echo "Hello"
```

All indented lines following the step are treated as the file content. Comments (`#`) inside indented blocks are treated as content, not as comments.

## DSL Reference

### Given Steps (Setup)

#### Create a file with content
```
Given a file "path/to/file.txt" with content:
  multiline
  content
  here
```

Creates a file at the specified path inside the container with the provided content.

#### Create an empty file
```
Given a file "path/to/file.txt" exists
```

Creates an empty file at the specified path.

#### Create a directory
```
Given the directory "path/to/dir" exists
```

Creates a directory (including parents) at the specified path.

#### Set environment variable
```
Given environment variable "VAR_NAME" is set to "value"
```

Sets an environment variable that will be available when the When step runs.

#### Empty working directory
```
Given an empty working directory
```

Clears any existing files in the container's `/smoko-work` directory (rarely needed, as each scenario gets a fresh container).

### When Steps (Action)

#### Run a command
```
When I run "command arg1 arg2"
```

Executes the command in the container and captures stdout, stderr, and the exit code. The entire command string is executed as-is in the container's shell.

Only one When step is allowed per scenario. The When step captures:
- **stdout** — All output written to standard output
- **stderr** — All output written to standard error
- **exit code** — The command's exit code (0 = success, non-zero = failure)

### Then/And Steps (Assertions)

#### Assert exit code
```
Then exit code is 0
Then exit code is not 1
```

Checks that the command's exit code matches (or doesn't match) the specified value.

#### Assert output contains text
```
Then output contains "expected text"
```

Checks that stdout contains the exact specified text as a substring.

#### Assert output matches regex
```
Then output matches "^[a-zA-Z0-9]+@[a-z]+\.[a-z]+$"
```

Checks that stdout matches the provided regex pattern (Go `regexp` syntax, which is RE2 dialect). The regex must match the entire output.

Tip: Use `(?s:.*)` for multiline matching when needed.

#### Assert output line contains text
```
Then output line contains "text"
```

Checks that at least one line in stdout contains the specified text.

#### Assert stderr contains text
```
Then stderr contains "error message"
```

Checks that stderr contains the specified text as a substring.

#### Assert file exists
```
Then file "path/to/file.txt" exists
```

Checks that the specified file exists in the container.

#### Assert file content
```
Then file "path/to/file.txt" contains "expected content"
Then file "path/to/file.txt" does not contain "unexpected text"
```

Checks that the file contains (or does not contain) the specified text as a substring.

For content with double quotes, escape them with `\"`:
```
Then file "config.json" contains "\"name\": \"value\""
```

For multi-line content, use the block form with a trailing `:`:
```
Then file ".curriculum" contains:
  "dependencies": [
    { "name": "dummy-skill" }
  ]
```

Negation works with the block form too:
```
Then file ".curriculum" does not contain:
  "version": "1.0.0"
```

#### Assert directory exists
```
Then the directory "path/to/dir" exists
Then the directory "path/to/dir" does not exist
```

Checks that the specified directory exists (or does not exist) in the container.

## Common Patterns

### Testing CLI Output

```
Scenario: CLI produces correct output
  When I run "my-cli greet Alice"
  Then exit code is 0
  Then output contains "Hello, Alice"
```

### Testing Exit Codes

```
Scenario: CLI fails on invalid input
  When I run "my-cli invalid"
  Then exit code is not 0
  Then stderr contains "Invalid argument"
```

### Testing File Operations

```
Scenario: CLI creates output file
  When I run "my-cli generate output.txt"
  Then exit code is 0
  Then file "output.txt" exists
  Then file "output.txt" contains "Generated content"
```

### Testing with Environment Variables

```
Scenario: CLI respects environment variables
  Given environment variable "DEBUG" is set to "true"
  When I run "my-cli start"
  Then exit code is 0
  Then output contains "Debug mode enabled"
```

### Testing Configuration Files

```
Scenario: CLI reads config file
  Given a file "config.json" with content:
    {
      "timeout": 30,
      "retries": 3
    }
  When I run "my-cli --config config.json"
  Then exit code is 0
```

## Workflow

### Running Tests

#### Run a single file
```bash
smoko run test.smoko
```

#### Run all tests in a directory
```bash
smoko run specs/
smoko run .
```

#### Run with specific Docker image
```bash
smoko run test.smoko --image myimage:latest
```

#### Run with verbose output
```bash
smoko run test.smoko --verbose
```

Shows detailed output for each step, useful for debugging failing tests.

#### Run with fail-fast
```bash
smoko run test.smoko --fail-fast
```

Stops after the first failing scenario instead of running all tests.

#### Run scenarios in parallel
```bash
smoko run specs/ --parallel 4
smoko run specs/ --parallel 0
```

Runs up to N scenarios concurrently. `--parallel 0` auto-detects based on available CPU cores (`GOMAXPROCS`). Default is `1` (sequential). Since each scenario runs in its own Docker container, parallelism is safe. Useful for large test suites where Docker overhead dominates.

> **Tip:** Combine with `--fail-fast` to stop as soon as any parallel scenario fails.

### Image Resolution

Smoko resolves the Docker image to use in this order (highest to lowest priority):
1. `--image` flag on the command line
2. `Image:` declaration inside the `.smoko` file
3. `image` setting in `.smokorc` (TOML format in the project root)
4. Error if no image is specified

Example `.smokorc`:
```toml
image = "alpine:latest"
timeout = 30
```

### Test Organization

Best practices for organizing tests:
- **By feature**: One `.smoko` file per feature being tested
- **By command**: One file per CLI command or subcommand
- **Fixtures**: Place test data files in a `specs/` directory, referenced relative to the container's `/smoko-work` directory

Example structure:
```
project/
├── specs/
│   ├── basic.smoko
│   ├── files.smoko
│   ├── envvars.smoko
│   └── advanced/
│       └── complex.smoko
└── .smokorc
```

## Best Practices

### Test Isolation
Each scenario runs in a fresh Docker container. Avoid assumptions about file system state or previous scenarios. Use explicit Given steps to set up all required files and environment.

### Meaningful Assertions
Write assertions that actually verify the behavior, not just that the command succeeded:

```
# Good
Then output contains "User created successfully"
Then file "users.db" exists

# Vague
Then exit code is 0
```

### Regex Patterns
When using regex assertions, remember:
- Go `regexp` syntax (RE2 dialect, no backreferences)
- The pattern must match the entire output (anchors: `^` start, `$` end)
- Use `(?s:.*)` for matching across newlines

Example:
```
Then output matches "^Usage: my-cli.*OPTIONS.*$"
```

### Environment Variables
Use environment variables for configuration that varies across test runs:

```
Given environment variable "API_KEY" is set to "test-key"
Given environment variable "DEBUG" is set to "1"
```

### File Content in Assertions
For complex file content assertions, use `contains` (substring match) rather than exact matches. This makes tests more maintainable.

Escape double quotes inside assertion strings with `\"`:
```
Then file "output.json" contains "\"status\": \"success\""
```

For multi-line content (e.g. JSON blocks), use the block form:
```
Then file ".curriculum" contains:
  "dependencies": [
    { "name": "dummy-skill" }
  ]
```

To assert absence of content:
```
Then file ".curriculum" does not contain "\"version\""
Then output does not contain "error"
```

### Organizing Fixtures
Store test data files in `specs/` and reference them from Given steps:

```
Given a file "input.csv" with content:
  [content here]
```

Or copy from fixtures in the container using shell commands:

```
When I run "cp /fixtures/template.conf config.conf"
```

(Note: Fixtures would need to be baked into your test Docker image)

## Architecture Notes for Agent

When an agent is asked to:
- **Write a smoko test** — Use this guide to construct correct Given/When/Then syntax
- **Debug a failing test** — Check exit codes, output matching (exact vs regex), file paths, and environment variables
- **Add assertions** — Refer to the "Then/And Steps" section for available assertion types
- **Optimize tests** — Consolidate shared setup into Background, use environment variables for configuration
- **Test new CLI behavior** — Start with simple output assertions, then add file/exit code checks

The underlying engine handles Docker lifecycle, file operations, and regex matching—just focus on describing the test scenario in DSL.

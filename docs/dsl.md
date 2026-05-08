# Smoko DSL Reference

Smoko specs use a small Gherkin-style format.

## Feature

```gherkin
Feature: Feature Name
  Optional description.

  Image: alpine:latest
```

`Image:` is optional when an image is supplied by `.smokorc` or `--image`.

## Scenario

```gherkin
Scenario: Scenario name
  Given setup steps
  When I run "command"
  Then assertions
```

Each scenario must have exactly one `When` step and at least one `Then`
assertion. All `Given` setup steps must appear before `When`; all `Then`
assertions must appear after `When`. `And` and `But` inherit the previous step
type.

## Background

```gherkin
Background:
  Given a file "config.json" with content:
    {"enabled":true}
```

`Background` may contain only `Given` setup steps. It runs before each scenario
in the feature.

## Given Steps

```gherkin
Given a file "path/to/file.txt" with content:
  file content
Given a file "path/to/file.txt" exists
Given the directory "path/to/dir" exists
Given the working directory is "path/to/dir"
Given I run "setup command"
Given an empty working directory
Given environment variable "NAME" is set to "value"
```

Setup paths are confined to `/smoko-work`. Relative paths resolve under
`/smoko-work`. Paths containing `..` are rejected.

## Capturing Variables

After `Given I run`, save command output for later steps:

```gherkin
Given I run "mycli version"
And I save output as $VERSION

Given I run "mycli info --json"
And I save JSON path "$.version" as $VERSION

Given I run "mycli version"
And I save pattern "v([0-9.]+)" as $VERSION
```

Captured variables are written to `.smoko_env` and are available to later
commands. Smoko also expands captured variables in `Then` file and directory
path arguments.

## When Steps

```gherkin
When I run "command arg1 arg2"
When I run "command" with input "stdin data"
When I run "command" expecting exit code 1
```

`expecting exit code N` records an assertion on the action. If the command exits
with another code, the scenario is marked failed and subsequent assertions are
still evaluated.

## Then Assertions

Exit code assertions:

```gherkin
Then exit code is 0
Then exit code is not 1
```

Output assertions:

```gherkin
Then output contains "text"
Then output does not contain "error"
Then stdout contains "message"
Then stderr contains "error"
Then output matches pattern "regex.*pattern"
Then stdout matches pattern "v\d+\.\d+"
Then stderr does not match pattern "panic:"
Then output equals "exact value"
Then stdout equals "exact value"
Then output is empty
Then stderr is not empty
```

JSON assertions:

```gherkin
Then output as JSON at path "$.user.name" equals "Alice"
Then stdout as JSON at path "$.ok" equals true
Then file "result.json" as JSON at path "$.items[0].id" equals 123
Then file "result.json" as JSON at path "$.items" equals:
  [1, 2, 3]
```

File system assertions:

```gherkin
Then file "path/to/file" exists
Then file "path/to/file" does not exist
Then file "path/to/file" contains "content"
Then file "path/to/file" matches pattern "^\d+\.\d+\.\d+$"
Then file "path/to/file" equals "exact content"
Then file "path/to/file" is empty
Then file "path/to/file" is not empty
Then directory "path/to/dir" exists
```

File and directory assertion paths resolve relative to `/smoko-work` unless an
absolute path is provided. Paths containing `..` are rejected.

`equals` trims leading and trailing whitespace before comparing. JSON `equals`
compares parsed JSON values.

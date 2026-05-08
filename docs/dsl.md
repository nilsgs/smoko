# Smoko DSL Reference

Smoko specs use a small Gherkin-style format.

## Feature

```gherkin
@cli @linux
Feature: Feature Name
  Optional description.

  Image: alpine:latest
```

`Image:` is optional when an image is supplied by `.smokorc` or `--image`.

## Scenario

```gherkin
@smoke
Scenario: Scenario name
  Given setup steps
  When I run "command"
  Then assertions
```

Each scenario must have exactly one `When` step and at least one `Then`
assertion. All `Given` setup steps must appear before `When`; all `Then`
assertions must appear after `When`. `And` and `But` inherit the previous step
type.

## Tags

Tags are metadata labels used for filtering and discovery.

```gherkin
@cli @requires-docker
Feature: Repo commands

  @git
  @dirty
  Scenario: Reports dirty worktree
    When I run "mycli status"
    Then exit code is 0
```

Feature tags apply to every scenario in that feature. Scenario tags apply only
to that scenario. Effective scenario tags are the union of feature and scenario
tags.

Tag rules:

- Tag lines may appear only immediately before `Feature:` or `Scenario:`.
- Comments and blank lines are allowed between tag lines and the tagged item.
- Tags in spec files must start with `@`.
- Tag names must match `[A-Za-z0-9][A-Za-z0-9_-]*`.
- Tag matching is case-sensitive; prefer lowercase kebab-case such as
  `@requires-docker`.
- Tags before `Background:` or steps are invalid.

Use `--tag` and `--skip-tag` to filter scenarios:

```sh
smoko run specs/ --tag git
smoko run specs/ --tag git --tag cli
smoko run specs/ --skip-tag slow
smoko run specs/ --tag git --skip-tag slow
```

Multiple `--tag` values are ORed. `--skip-tag` excludes matching scenarios and
wins over includes. CLI tag values may be written with or without `@`.

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
Given a git repository "repo" exists
Given git repository "repo" has committed file "README.md" with content:
  committed content
Given git repository "repo" has untracked file "scratch.txt" with content:
  untracked content
Given git repository "repo" has modified file "README.md" with content:
  modified content
Given git repository "repo" is on branch "feature/name"
```

Setup paths are confined to `/smoko-work`. Relative paths resolve under
`/smoko-work`. Paths containing `..` are rejected.

Git fixture steps create disposable repositories inside `/smoko-work` and
require `git` on `PATH` in the test image. New repositories use `main` and an
empty initial commit. `committed file` creates the repo if needed and commits
only that file. `modified file` requires the file to already be tracked. File
paths inside a Git repository must be relative to the repository root and must
not contain `..`.

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

Git assertions:

```gherkin
Then git repository "repo" is clean
Then git repository "repo" is dirty
Then git repository "repo" has branch "feature/name"
```

Git assertion repository paths resolve under `/smoko-work`, are confined there,
and expand captured variables.

# Smoko Usage

Smoko runs `.smoko` smoke specs for CLI applications in isolated Docker
containers.

## Minimal Spec

```gherkin
Feature: Hello World CLI
  Scenario: Print greeting
    When I run "echo Hello, World!"
    Then exit code is 0
    Then output contains "Hello, World!"
```

Run it with an explicit image:

```sh
smoko run hello.smoko --image alpine:latest
```

## Project Setup

A typical project layout:

```text
myproject/
  .smokorc
  Dockerfile.test
  specs/
    init.smoko
    commands.smoko
    errors.smoko
  src/
```

The tested CLI must be available on `PATH` inside the container. Use a
`Dockerfile.test` to build and package it.

Example:

```dockerfile
FROM golang:1.25 AS builder
WORKDIR /build
COPY src/ .
RUN go build -o /usr/local/bin/mycli .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends bash && rm -rf /var/lib/apt/lists/*
COPY --from=builder /usr/local/bin/mycli /usr/local/bin/mycli
WORKDIR /smoko-work
```

## Running Specs

Run the default `specs/` directory:

```sh
smoko run
```

Run a file or directory:

```sh
smoko run specs/init.smoko
smoko run specs/
```

Run with JSON output:

```sh
smoko run specs/ --output json
```

Run scenarios in parallel:

```sh
smoko run specs/ --parallel 0
```

Skip the configured build command:

```sh
smoko run specs/ --no-build
```

List scenarios without building or running:

```sh
smoko run specs/ --list
```

## Output

Default text output is intended for terminal inspection. It prints one status
line per scenario, feature durations, and a final summary.

Use JSON output for agents and automation:

```sh
smoko run specs/ --output json
```

Build and Docker readiness messages are written to stderr so JSON stdout stays
parseable.

## Common Patterns

Set up files before the action:

```gherkin
Scenario: Reads a config file
  Given a file "config.json" with content:
    {"enabled":true}
  When I run "mycli read config.json"
  Then exit code is 0
  Then output contains "enabled"
```

Run from a subdirectory:

```gherkin
Scenario: Finds project root
  Given the directory "src/App" exists
  Given the working directory is "src/App"
  When I run "mycli status"
  Then exit code is 0
```

Capture output for later assertions:

```gherkin
Scenario: Uses generated output path
  Given I run "mycli init --json"
  And I save JSON path "$.outputDir" as $OUTDIR
  When I run "mycli generate"
  Then directory "$OUTDIR" exists
```

Set up a disposable Git repository:

```gherkin
Scenario: Reports dirty working tree
  Given git repository "repo" has committed file "README.md" with content:
    hello
  Given git repository "repo" has modified file "README.md" with content:
    changed
  When I run "mycli status repo"
  Then exit code is 0
  Then git repository "repo" is dirty
```

Git fixture steps require `git` on `PATH` in the test image. They are intended
for local repository state, not remotes, credentials, submodules, or hosted Git
provider behavior.

## More Reference

- [DSL reference](dsl.md)
- [Configuration](configuration.md)

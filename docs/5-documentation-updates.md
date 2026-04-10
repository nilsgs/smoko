# 5 — Documentation Updates

STATUS: DONE

## Problem

The smoko skill and README lack practical guidance for adopting smoko in a new project. Every consumer repo has independently invented the same setup pattern. The skill also doesn't document common idioms, leading to suboptimal test writing (e.g., using `sh -c` chains where `Given I run` already works).

## Scope

Two additions to both `skills/smoko/SKILL.md` and `README.md`:

1. **Project Setup Guide** — how to adopt smoko in a CLI project
2. **Common Idioms** — patterns for effective test writing

## 1. Project Setup Guide

Add a new section after the Quick Start / before the DSL Reference.

### Content

#### Dockerfile.test Template

```dockerfile
# Build stage — compile your CLI
FROM golang:1.22 AS builder
WORKDIR /build
COPY src/ .
RUN go build -o /usr/local/bin/mycli .

# Runtime stage — minimal image with your CLI
FROM debian:bookworm-slim
# Install any runtime dependencies (git, bash, etc.)
RUN apt-get update && apt-get install -y --no-install-recommends bash && rm -rf /var/lib/apt/lists/*
COPY --from=builder /usr/local/bin/mycli /usr/local/bin/mycli
WORKDIR /smoko-work
```

Note: Adjust the build stage for your language (dotnet, node, rust, etc.). The key requirements:
- CLI binary must be on `PATH` inside the container
- Working directory must be `/smoko-work`
- Include any tools your CLI depends on at runtime (git, bash, etc.)

#### .smokorc Configuration

```toml
image   = "mycli-test:latest"
timeout = 5
build   = "docker build -f Dockerfile.test -t mycli-test:latest ."
```

#### Directory Structure

```
myproject/
├── .smokorc
├── Dockerfile.test
├── Makefile
├── specs/
│   ├── init.smoko
│   ├── commands.smoko
│   └── errors.smoko
└── src/
    └── ...
```

Organize specs by command or feature area. One `.smoko` file per CLI command is a good starting point.

#### Makefile Integration

```makefile
smoko:
	smoko run

# Or without build integration in .smokorc:
smoko: smoko-image
	smoko run specs/

smoko-image:
	docker build -f Dockerfile.test -t mycli-test:latest .
```

## 2. Common Idioms

Add a "Patterns" or "Common Idioms" section to the skill.

### Sequential Setup with Given I run

Use multiple `Given I run` steps for sequential setup. Each runs in order and fails the scenario on non-zero exit:

```gherkin
Scenario: Task added to a feature
  Given I run "mycli init --name my-project"
  Given I run "mycli feature add my-feature --json"
    And I save JSON path "$.id" as $FID
  When I run "mycli task add my-task --feature $FID --json"
  Then exit code is 0
  Then output as JSON at path "$.title" equals "my-task"
```

Don't wrap sequential commands in `sh -c` or write shell script files when `Given I run` handles it directly.

### Helper Scripts in Docker Images

For complex test utilities, bake a helper script into the Docker image rather than inlining shell logic in specs:

```dockerfile
# In Dockerfile.test
COPY specs/helpers/test-helper.sh /usr/local/bin/test-helper
```

```gherkin
# In specs
Given I run "test-helper init-repo myrepo"
```

This keeps specs readable and moves shell complexity into a maintainable script.

### Background for Shared Setup

Use Background for setup common to all scenarios in a feature:

```gherkin
Feature: Task Management
  Background:
    Given I run "mycli init --name test-project"
    Given environment variable "LOG_LEVEL" is set to "quiet"

  Scenario: Add a task
    When I run "mycli task add my-task"
    Then exit code is 0
```

Don't duplicate the same Given steps across every scenario.

### JSONPath Over String Matching for Structured Output

Prefer JSONPath assertions over substring matching with escaped quotes:

```gherkin
# Prefer this:
Then output as JSON at path "$.title" equals "my-task"
Then output as JSON at path "$.status" equals "todo"

# Over this:
Then output contains "\"title\": \"my-task\""
Then output contains "\"status\": \"todo\""
```

JSONPath is more robust (whitespace-independent, validates structure) and more readable.

### Testing Error Cases

Always verify both the exit code and the error message:

```gherkin
Scenario: Rejects invalid input
  When I run "mycli process --format invalid"
  Then exit code is not 0
  Then stderr contains "unsupported format"
```

Checking only the exit code can mask wrong-reason failures.

## Implementation

### Step 1 — Update skills/smoko/SKILL.md

Add the project setup section after the "Core model" section. Add the idioms as a "Patterns" section (extending the existing patterns section with the new content).

### Step 2 — Update README.md

Add a "Setting Up Smoko for Your Project" section after the Quick Start. Reference the Dockerfile.test template, .smokorc, and specs/ convention.

### Step 3 — Update AGENTS.md

Reference the new documentation sections. Ensure the architecture notes stay in sync with any new features (capture, new assertions, build integration).

## Notes

- These documentation updates should be done **after** the feature work (assertions, capture, build integration) so the docs reflect the final state.
- The skill file should document all three capture variants once feature #2 is implemented.
- Review and update the idioms section as new patterns emerge from real usage.

# Smoko - Smoke Test Tool Design Document

## 1. Overview

### What is Smoko?

Smoko is a platform-agnostic smoke testing tool designed for CLI applications. It allows developers to write test specifications in a human-readable BDD-style DSL (Domain Specific Language) and execute them in isolated Docker containers. Smoko enables consistent testing of CLI tools regardless of the underlying operating system, programming language, or runtime environment.

### Why Smoko?

Modern CLI development involves many languages and frameworks. Testing these CLIs typically requires:
- Language-specific test frameworks (Go's testing, Python's pytest, Node's Jest, etc.)
- OS-specific test runners and setup
- Complex environment management
- Maintenance of test suites in multiple languages

**Smoko solves this by**:
- **Unified testing approach**: Write tests once, run on any OS in Docker
- **Language agnostic**: Test any CLI regardless of implementation language
- **Human-readable specs**: Use BDD-style syntax (Given/When/Then) that non-technical stakeholders understand
- **Complete isolation**: Docker containers ensure tests don't interfere with host system
- **Easy maintenance**: Test specs live alongside CLI code without language-specific boilerplate

### Philosophy

Smoko embraces three principles:
1. **Simplicity**: Tests should be easy to write and understand
2. **Universality**: Tests should work for any CLI tool in any language
3. **Clarity**: Test results should clearly indicate what passed, failed, or errored

---

## 2. DSL Specification

### Overview

Smoko tests are written in a Gherkin-inspired DSL. Each test file defines features and scenarios that describe how your CLI behaves. The syntax is designed to be human-readable and self-documenting.

### File Format

Test files use `.smoko` extension and contain feature definitions. A single repository may have multiple `.smoko` files, typically one per CLI command or feature area.

### Syntax Structure

#### Feature
A feature file contains a feature definition and one or more scenarios.

```
Feature: Brief description of what this feature tests
  Description of the feature context and purpose

  Scenario: Specific behavior being tested
    Given [setup condition]
    When [action taken]
    Then [expected outcome]
```

#### Scenario
Each scenario is an isolated test case within a feature. Scenarios follow a Given-When-Then structure:

- **Given**: Set up the preconditions and context
- **When**: Perform the action to test
- **Then**: Assert the expected outcomes

### Step Types

#### Given (Setup Steps)

Given steps establish the initial state before testing. Common patterns:

```
Given the CLI is installed
Given an empty working directory
Given a file "config.json" with content:
  {
    "key": "value"
  }
Given environment variable "MY_VAR" is set to "test_value"
Given the file "data.txt" exists
Given the directory "output" exists
```

#### When (Action Steps)

When steps execute the CLI or perform test actions. The primary form:

```
When I run "command arg1 arg2"
When I run "command" with input "stdin data"
When I run "command" and capture output
```

Special forms:

```
When I run "command" expecting exit code 1
When I run "command" in the "output" directory
When I wait 5 seconds
When I run "script.sh"
```

#### Then (Assertion Steps)

Then steps verify the outcome. Multiple assertions can follow a single When step.

**Output Assertions:**
```
Then output contains "expected text"
Then output matches pattern "regex"
Then stdout contains "message"
Then stderr contains "error"
Then exit code is 0
Then exit code is not 0
```

**File System Assertions:**
```
Then file "output.txt" exists
Then file "output.txt" does not exist
Then file "output.txt" contains "content"
Then file "output.txt" matches pattern "regex"
Then directory "results" exists
Then file "output.txt" has permissions "755"
```

**JSON Output Assertions:**
```
Then output contains JSON:
  {
    "status": "success",
    "count": 5
  }
Then output JSON at "$.data[0].id" equals "123"
Then output JSON has path "$.results"
```

**Negation:**
All Then assertions can be negated:
```
Then output does not contain "error"
Then file "backup.txt" does not exist
Then output does not match pattern "failed.*"
```

### Multi-line Content

For Given steps with file content or Then steps with JSON payloads, content can span multiple lines using indentation:

```
Given a file "script.sh" with content:
  #!/bin/bash
  echo "Hello"
  exit 0

Then output contains JSON:
  {
    "name": "test",
    "values": [1, 2, 3]
  }
```

### Comments

Lines starting with `#` are comments and are ignored:

```
# This scenario tests the error path
Scenario: Handle missing input file
  Given a file "config.json" exists
  # We don't create the input file intentionally
  When I run "tool --config config.json --input missing.txt"
  Then exit code is 1
```

---

## 3. Supported Assertions

### Categories

Smoko supports assertions across four categories:

#### 3.1 Exit Code Assertions
Verify the CLI's exit status:
- `exit code is 0` - Successful execution
- `exit code is 1` - Generic failure
- `exit code is not 0` - Any failure status
- `exit code is N` - Specific exit code

#### 3.2 Output Assertions
Validate stdout and stderr:
- `output contains "text"` - Any output contains exact string
- `stdout contains "text"` - Only check stdout
- `stderr contains "text"` - Only check stderr
- `output matches pattern "regex"` - Regex pattern matching
- `output does not contain "text"` - Negation

#### 3.3 File System Assertions
Check files and directories:
- `file "path" exists` - File present
- `file "path" does not exist` - File absent
- `file "path" contains "text"` - File content check
- `file "path" matches pattern "regex"` - Regex on file content
- `directory "path" exists` - Directory present
- `file "path" has permissions "755"` - Unix permission verification

#### 3.4 Structured Output Assertions
Validate JSON and structured data:
- `output contains JSON: {...}` - Verify JSON structure/values
- `output JSON at "$.path" equals "value"` - JSONPath assertions
- `output JSON has path "$.field"` - Path existence check

All assertions support negation with "does not", "is not", etc.

---

## 4. Architecture

### High-Level Flow

```
User writes *.smoko files
         ↓
    Smoko reads and parses files
         ↓
    For each scenario:
      - Create Docker container
      - Execute Given steps (setup)
      - Execute When step (action)
      - Execute Then steps (assertions)
      - Report results
      - Clean up container
         ↓
    Generate test report
```

### Components

**Specification Parser**
- Reads `.smoko` files
- Validates syntax
- Extracts features, scenarios, and steps
- Reports parse errors clearly

**Test Executor**
- Manages Docker container lifecycle
- Executes setup steps
- Runs CLI commands in container
- Captures stdout, stderr, exit codes
- Manages file system state

**Assertion Engine**
- Evaluates Then step assertions
- Compares actual vs expected outcomes
- Supports regex, JSON, file system checks
- Provides clear failure diagnostics

**Test Reporter**
- Tracks pass/fail status
- Generates human-readable output
- Shows detailed failure information
- Provides summary statistics

**Docker Interface**
- Handles container creation from specified image
- Manages working directory and file mounts
- Isolates each scenario execution
- Cleans up after tests complete

### Container Execution Model

Each scenario runs in a fresh Docker container:
- Container starts fresh (no state from previous scenarios)
- Current working directory contains mounted test files
- CLI tool and dependencies are installed/present in image
- Container is destroyed after scenario completes
- Next scenario starts with a clean container

This isolation ensures:
- Tests don't interfere with each other
- File system changes don't persist across scenarios
- Environment is fully controlled
- Results are reproducible

---

## 5. CLI Usage

### Basic Invocation

```bash
smoko run path/to/test.smoko
smoko run path/to/tests/
```

### Common Options

```bash
# Specify Docker image to use
smoko run test.smoko --image myimage:latest

# Run specific feature or scenario
smoko run test.smoko --feature "Feature name"
smoko run test.smoko --scenario "Scenario name"

# Output formats
smoko run test.smoko --format json
smoko run test.smoko --format tap
smoko run test.smoko --format text

# Verbose output
smoko run test.smoko --verbose
smoko run test.smoko --show-all-output

# Stop after N failures
smoko run test.smoko --fail-fast
smoko run test.smoko --fail-after 3

# Parallel execution
smoko run tests/ --parallel 4
```

### Output

Standard output shows:
- Feature name and description
- Each scenario name and result (✓ or ✗)
- For failures: expected vs actual, relevant context
- Summary: total tests, passed, failed

---

## 6. Examples

### Example 1: Basic CLI Test

```
Feature: Hello World CLI
  Testing the hello-world command

  Scenario: Print greeting
    When I run "hello-world"
    Then exit code is 0
    Then output contains "Hello, World!"

  Scenario: Accept name parameter
    When I run "hello-world --name Alice"
    Then exit code is 0
    Then output contains "Hello, Alice!"
```

### Example 2: File Processing CLI

```
Feature: Data Processing Tool
  The data processor reads a CSV file and produces JSON output

  Scenario: Convert CSV to JSON
    Given a file "input.csv" with content:
      name,age
      Alice,30
      Bob,25

    When I run "process --input input.csv --output output.json"

    Then exit code is 0
    Then file "output.json" exists
    Then output contains JSON:
      [
        {"name": "Alice", "age": 30},
        {"name": "Bob", "age": 25}
      ]

  Scenario: Handle missing input file
    When I run "process --input missing.csv --output output.json"

    Then exit code is 1
    Then stderr contains "File not found"
    Then file "output.json" does not exist
```

### Example 3: Environment and Configuration

```
Feature: Configuration Management
  Testing config file loading and env var handling

  Scenario: Load config from file
    Given a file "app.conf" with content:
      server_port=8080
      debug=true

    When I run "myapp --config app.conf"

    Then exit code is 0
    Then output contains "Server running on port 8080"

  Scenario: Override with environment variable
    Given environment variable "DEBUG" is set to "false"
    Given a file "app.conf" with content:
      debug=true

    When I run "myapp --config app.conf"

    Then output matches pattern "Debug.*false"
```

### Example 4: Complex Workflow

```
Feature: Build and Deploy Pipeline
  End-to-end testing of deployment workflow

  Scenario: Build and verify artifacts
    Given an empty working directory

    When I run "build --source src/ --output dist/"

    Then exit code is 0
    Then directory "dist" exists
    Then file "dist/app.bin" exists
    Then file "dist/manifest.json" contains "version"

  Scenario: Validate build output
    Given an empty working directory

    When I run "build --source src/ --output dist/ --config debug"

    Then file "dist/manifest.json" contains JSON:
      {
        "mode": "debug",
        "optimized": false
      }
```

---

## 7. Implementation Assumptions

### Docker Container Requirements

Each scenario runs in a Docker container. The container image:
- Must have the CLI tool installed and available in PATH, OR
- The tool will be mounted/installed as part of test setup
- Should have basic utilities (bash, standard Unix tools)
- Must support the operations defined in Given steps

### Working Directory

- Tests execute in a working directory inside the container
- Files created in Given steps or by the CLI are accessible in Then assertions
- The directory is isolated per scenario

### Environment

- Each scenario starts with a clean environment
- Environment variables set in Given steps are available during When/Then
- No state is shared between scenarios

---

## 8. Future Considerations

This design document defines the core feature set. Future enhancements might include:
- Test setup/teardown hooks
- Parameterized scenarios (data-driven testing)
- Custom step definitions
- Integration with CI/CD systems
- Test report generation and trending
- Plugins for custom assertion types

These are out of scope for the current design but noted for future iterations.

# Smoko — Smoke Test Tool

A platform-agnostic smoke testing tool for CLI applications. Write tests in a Gherkin-inspired BDD-style DSL and execute them in isolated Docker containers.

## Features

- **Language-agnostic**: Test any CLI tool in any language
- **BDD-style syntax**: Human-readable Given/When/Then specifications
- **Docker isolation**: Each test scenario runs in a fresh container
- **Comprehensive assertions**: Exit codes, output matching (regex), file system checks
- **Cross-platform**: Works consistently on Windows, macOS, and Linux

## Quick Start

### Installation

**Unix (bash/zsh/fish):**

```bash
bash install.sh
```

**Windows (PowerShell):**

```powershell
.\install.ps1
```

Both scripts build the binary from source, install it to `~/.smoko/bin` (or `%USERPROFILE%\.smoko\bin` on Windows), and add it to your `PATH`.

**Or build manually with Make:**

```bash
make build      # → smoko.exe in the repo root
make install    # → installs to GOPATH/bin via go install
```

### Writing Tests

Create a `.smoko` file:

```gherkin
Feature: Hello World CLI
  Testing basic CLI functionality

  Image: alpine:latest

  Scenario: Print greeting
    When I run "echo 'Hello, World!'"
    Then exit code is 0
    Then output contains "Hello, World!"
```

### Running Tests

```bash
# Run a single test file
smoko run test.smoko

# Run all tests in a directory
smoko run specs/

# With options
smoko run test.smoko --image myimage:latest --verbose --fail-fast
```

## DSL Reference

### Feature Declaration

```gherkin
Feature: Feature Name
  Optional description of what this feature tests
  
  Image: docker-image:tag   # Optional; can also use --image flag or .smokorc
```

### Scenario

```gherkin
Scenario: Scenario description
  Given [setup steps]
  When [action]
  Then [assertions]
```

### Given Steps (Setup)

```gherkin
Given a file "path/to/file.txt" with content:
  multiline content here
Given a file "path/to/file.txt" exists
Given the directory "path/to/dir" exists
Given an empty working directory
Given environment variable "VAR" is set to "value"
```

### When Steps (Action)

```gherkin
When I run "command arg1 arg2"
When I run "command" with input "stdin data"
When I run "command" expecting exit code 1
```

### Then Steps (Assertions)

```gherkin
# Exit code
Then exit code is 0
Then exit code is not 1

# Output matching
Then output contains "text"
Then output does not contain "error"
Then output matches pattern "regex.*pattern"
Then stdout contains "message"
Then stderr contains "error"

# File system
Then file "path/to/file" exists
Then file "path/to/file" does not exist
Then file "path/to/file" contains "content"
Then directory "path/to/dir" exists
```

### Background (Optional)

Shared setup steps that run before each scenario:

```gherkin
Feature: With Background
  Background:
    Given a file "config.json" with content:
      {"key": "value"}
  
  Scenario: First test
    When I run "app --config config.json"
    Then exit code is 0
```

## Configuration

Create a `.smokorc` file in the project root:

```toml
image   = "myimage:latest"  # Default Docker image
timeout = 30                # Seconds per When step
```

Or use CLI flags to override:

```bash
smoko run test.smoko --image ubuntu:latest --timeout 60 --verbose --fail-fast
```

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--image` | (none) | Docker image to use (overrides .smokorc and inline Image:) |
| `--timeout` | 30 | Seconds to wait for each When step |
| `--verbose` | false | Print stdout/stderr even for passing scenarios |
| `--fail-fast` | false | Stop after the first failed scenario |

## Project Structure

```
smoko/
├── README.md
├── VERSION                # Current version (e.g. 0.1.0)
├── Makefile
├── install.sh             # Unix installer
├── install.ps1            # Windows installer
├── .gitignore
├── .copilot-instructions.md
└── src/
    ├── go.mod
    ├── go.sum
    ├── cmd/
    │   └── smoko/
    │       └── main.go
    └── internal/
        ├── config/
        ├── parser/
        ├── docker/
        ├── executor/
        ├── assertions/
        └── reporter/
```

## Development

### Build

```bash
make build        # build for current platform → smoko.exe
make install      # go install to GOPATH/bin
make cross        # cross-compile for all platforms → dist/
make clean        # remove build artifacts
```

### Test

```bash
make test-local   # run unit tests locally
make test         # run unit tests inside a Docker container
```

### Version

The version is read from `VERSION` at build time and embedded in the binary via `-ldflags`:

```
$ smoko --version
0.1.0+3dd1ab4
```

To release a new version, update `VERSION` and rebuild.

### Architecture

- **Parser**: Lexer and recursive-descent parser for `.smoko` files
- **Docker**: SDK wrapper managing container lifecycle
- **Executor**: Runs Given/When steps, coordinates test execution
- **Assertions**: Evaluates Then/And assertions
- **Reporter**: Formats and prints test results with colors

## Examples

### Example 1: File Processing

```gherkin
Feature: CSV to JSON Converter
  Scenario: Convert CSV to JSON
    Given a file "input.csv" with content:
      name,age
      Alice,30
      Bob,25
    When I run "convert --input input.csv --output output.json"
    Then exit code is 0
    Then file "output.json" exists
    Then file "output.json" contains "Alice"
```

### Example 2: Environment Configuration

```gherkin
Feature: Configuration Management
  Scenario: Load config with env override
    Given a file ".env" with content:
      DEBUG=true
      PORT=8080
    Given environment variable "DEBUG" is set to "false"
    When I run "app --config .env"
    Then exit code is 0
    Then output matches pattern "debug.*false"
```

## License

See repository for license information.

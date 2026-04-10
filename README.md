![Banner](img/banner.png)

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
Given I run "cp source.txt target.txt"
Given an empty working directory
Given environment variable "VAR" is set to "value"
```

`Given I run "..."` executes inside `/smoko-work`, sources `.smoko_env` if present, and fails the scenario immediately on a non-zero exit code.

#### Capturing output as variables

After a `Given I run "..."` step, you can save the output (or a part of it) into an environment variable. The variable is then available in subsequent steps via `.smoko_env`.

```gherkin
# Save trimmed stdout as a variable
Given I run "my-cli version"
And I save output as $VERSION

# Save a JSON field
Given I run "my-cli info --json"
And I save JSON path "$.version" as $VERSION

# Save a regex capture group
Given I run "my-cli version"
And I save pattern "v([0-9.]+)" as $VERSION
```

The variable becomes part of the environment for subsequent steps — you can reference it in later `Given I run` commands, `When I run`, or file content blocks using `$VERSION` (shell expansion).

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

# Output matching (output = stdout+stderr combined; or use stdout / stderr individually)
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

# JSON assertions
Then output as JSON at path "$.user.name" exists
Then stdout as JSON at path "$.ok" equals true
Then file "result.json" as JSON at path "$.items[0].id" equals 123
Then file "result.json" as JSON at path "$.items" equals:
  [1, 2, 3]

# File system
Then file "path/to/file" exists
Then file "path/to/file" does not exist
Then file "path/to/file" contains "content"
Then file "path/to/file" matches pattern "^\d+\.\d+\.\d+$"
Then file "path/to/file" equals "exact content"
Then file "path/to/file" is empty
Then file "path/to/file" is not empty
Then directory "path/to/dir" exists
```

`equals` trims leading/trailing whitespace before comparing (both sides), so trailing newlines are ignored.
JSON `equals` compares parsed JSON values, not strings. Use JSON literals such as `"Alice"`, `3`, `true`, `null`, or block JSON for arrays and objects.

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
timeout = 1                 # Seconds per setup/action command
build   = "docker build -f Dockerfile.test -t myimage:latest ."  # Optional: build image before running tests
```

When `build` is set, smoko runs the command (from the `.smokorc` directory) before pulling images or running any scenarios. Build output streams to the terminal in real-time. Use `--no-build` to skip the build step when the image is already current.

Or use CLI flags to override:

```bash
smoko run test.smoko --image ubuntu:latest --parallel 0 --timeout 5 --verbose --fail-fast
smoko run specs/ --no-build   # skip image build
```

Defaults are tuned for faster feedback: `--parallel` uses auto mode by default and `--timeout` defaults to `1` second unless `.smokorc` or an explicit flag overrides it.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--image` | (none) | Docker image to use (overrides .smokorc and inline Image:) |
| `--timeout` | 1 | Seconds to wait for each setup/action command |
| `--verbose` | false | Print stdout/stderr even for passing scenarios |
| `--fail-fast` | false | Stop after the first failed scenario |
| `--parallel` | 0 | Number of scenarios to run concurrently (0 = auto) |
| `--no-build` | false | Skip the build step defined in .smokorc |

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

### Example 3: JSON Output Assertions

```gherkin
Feature: JSON API CLI
  Scenario: Read nested JSON output
    Given a file "stdout.json" with content:
      {"user":{"name":"Alice","active":true}}
    When I run "cat stdout.json"
    Then exit code is 0
    Then output as JSON at path "$.user.name" equals "Alice"
    Then output as JSON at path "$.user.active" equals true
```

### Example 4: JSON File Assertions

```gherkin
Feature: JSON File Generation
  Scenario: Inspect generated JSON file
    Given a file "result.json" with content:
      {"items":[1,2,3]}
    When I run "cat result.json"
    Then exit code is 0
    Then file "result.json" as JSON at path "$.items" equals:
      [1, 2, 3]
```

## License

See repository for license information.

![Banner](img/banner.png)

# Smoko

Smoko is a platform-agnostic smoke testing CLI for command-line applications.

Write tests in a small BDD-style `.smoko` format and run each scenario in an
isolated Docker container.

## Install

Prerequisites:

- Go 1.25+
- Git

Linux / macOS:

```sh
git clone https://github.com/nilsgs/smoko.git
cd smoko
./install.sh
```

Windows PowerShell:

```powershell
git clone https://github.com/nilsgs/smoko.git
cd smoko
.\install.ps1
```

The installer builds from source, copies `smoko` to `~/.smoko/bin`, and updates
your user `PATH` where supported.

## Quick Start

Create `hello.smoko`:

```gherkin
Feature: Hello World CLI
  Scenario: Print greeting
    When I run "echo Hello, World!"
    Then exit code is 0
    Then output contains "Hello, World!"
```

Run it:

```sh
smoko run hello.smoko --image alpine:latest
```

## Usage

Use `--help` for the full command surface:

```sh
smoko --help
smoko run --help
```

Common examples:

```sh
smoko run
smoko run specs/
smoko run specs/ --output json
smoko run specs/ --parallel 0
smoko run specs/ --tag git
smoko run specs/ --skip-tag slow
smoko run specs/ --no-build
```

Projects can use `.smokorc` to set the default image, timeout, and optional
image build command. When `build` is configured, Smoko builds the image before
running scenarios unless `--no-build` is passed.

## Docs

- [Expanded usage](docs/usage.md)
- [DSL reference](docs/dsl.md)
- [Configuration](docs/configuration.md)
- [Agent guidance](skills/smoko/SKILL.md)

## Development

Prerequisites:

- Go 1.25+
- Task v3: <https://taskfile.dev/docs/installation>
- Docker or Podman for `task smoke` and `task ci`

Common tasks:

```sh
task test     # run native Go tests
task build    # build the local binary into dist/
task install  # build and copy the binary to the user install directory
task smoke    # run specs with the local dist binary
task ci       # run test, build, and smoke
task cross    # build the full OS/architecture matrix into dist/
task clean    # remove dist/
```

The version is read from `VERSION` and stamped into the binary at build time.

## License

MIT. See [LICENSE](LICENSE).

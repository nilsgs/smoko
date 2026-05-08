# Smoko Configuration

Smoko can read defaults from a `.smokorc` file in the project root.

## Example

```toml
image = "mycli-test:latest"
timeout = 5
build = "docker build -f Dockerfile.test -t mycli-test:latest ."
```

## Fields

`image` sets the default Docker image for scenarios.

`timeout` sets the timeout, in seconds, for each setup or action command.

`build` sets an optional command that runs before image checks and scenario
execution. The command runs from the directory containing `.smokorc`.

## Image Resolution

Image precedence:

1. `--image` flag
2. `Image:` in the `.smoko` feature
3. `image` in `.smokorc`

If no image is resolved, the run fails before executing scenarios.

## Build Behavior

When `build` is set, Smoko runs the build command before pulling images or
executing scenarios.

```sh
smoko run specs/
```

Skip the build step when the image is already current:

```sh
smoko run specs/ --no-build
```

List scenarios without building or running:

```sh
smoko run specs/ --list
```

Successful build output is hidden by default. Use `--verbose` to stream
successful build output. Failed builds print the captured build output.

## Common Project Layout

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

## Dockerfile Requirements

The test image should:

- Put the CLI under test on `PATH`.
- Set the working directory to `/smoko-work`.
- Include runtime dependencies needed by the CLI or the spec commands.

Minimal Go example:

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

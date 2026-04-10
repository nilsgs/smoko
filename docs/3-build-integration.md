# 3 — Build Integration

STATUS: DONE

## Problem

Every smoko consumer follows the same two-step workflow:

```bash
docker build -f Dockerfile.test -t myimage:latest .    # step 1: build image
smoko run specs/                                        # step 2: run tests
```

This is always wired via Makefile:
```makefile
# bira
smoko: smoko-image
	smoko run specs/
smoko-image:
	docker build -f Dockerfile.test -t bira-test:latest .

# bump
test-smoko: build-test-image
	smoko run specs/

# curriculum, graft — same pattern
```

The image build is a prerequisite that lives outside smoko. If someone changes code and forgets to rebuild, they get confusing test failures from a stale image. Bringing the build step into smoko eliminates this class of errors and simplifies the workflow to a single command.

## Desired State

```toml
# .smokorc
image   = "bira-test:latest"
timeout = 5
build   = "docker build -f Dockerfile.test -t bira-test:latest ."
```

```bash
smoko run specs/             # builds image, then runs tests
smoko run specs/ --no-build  # skip build (image already current)
```

## Design Decisions

### Always build when `build` is set

When `.smokorc` has a `build` field, smoko runs it before pulling/running scenarios. Docker layer caching makes repeat builds fast, and the whole point is to prevent stale images.

### `--no-build` to skip

For quick re-runs where the image hasn't changed, `--no-build` skips the build step. This is the only flag needed — no `--build` opt-in.

### Build runs from the `.smokorc` directory

The build command executes with the working directory set to the directory containing `.smokorc`. This matches how Docker contexts work — `docker build ... .` refers to the project root.

### Build failure = exit 2

If the build command fails (non-zero exit), smoko exits with code 2 (error), same as parse errors or Docker connection failures. No scenarios are attempted.

### Output handling

Build output is streamed to the terminal in real-time (not captured and hidden). Builds can be slow and users need to see progress. Use `--verbose` or a default behavior that shows build output always (since it's a one-time step per run).

## Implementation

### Step 1 — Extend Config struct

File: `internal/config/config.go`

```go
type Config struct {
    Image   string `toml:"image"`
    Timeout int    `toml:"timeout"`
    Build   string `toml:"build"`   // new
}
```

No migration needed — existing `.smokorc` files without `build` will simply have an empty string.

### Step 2 — Add `--no-build` flag

File: `cmd/smoko/main.go`

Add flag to `runCmd()`:
```go
var noBuild bool
cmd.Flags().BoolVar(&noBuild, "no-build", false, "Skip the build step defined in .smokorc")
```

### Step 3 — Execute build command

File: `cmd/smoko/main.go`, in `runTests()` before image pulling.

```go
if cfg.Build != "" && !noBuild {
    fmt.Fprintf(os.Stderr, "Building image: %s\n", cfg.Build)
    buildCmd := exec.Command("sh", "-c", cfg.Build)  // or platform-appropriate shell
    buildCmd.Dir = cfgDir  // directory containing .smokorc
    buildCmd.Stdout = os.Stdout
    buildCmd.Stderr = os.Stderr
    if err := buildCmd.Run(); err != nil {
        return fmt.Errorf("build failed: %w", err)
    }
}
```

**Windows consideration**: Use `cmd /C` on Windows, `sh -c` on Unix. Or use Go's `os/exec` with shell detection. Since the build command is user-provided shell syntax (e.g., `docker build ...`), it needs a shell interpreter.

### Step 4 — Tests

Unit test in `internal/config/config_test.go`:
- `TestLoadConfigWithBuild` — verify `Build` field is parsed from TOML

Integration test: create a `.smokorc` with a `build` field pointing to a simple echo command, verify it runs before scenarios.

### Step 5 — Update documentation

Update `.smokorc` section in README.md and skill with the new `build` field.

Update CLI flags table with `--no-build`.

## Example .smokorc Files

```toml
# bira
image   = "bira-test:latest"
timeout = 5
build   = "docker build -f Dockerfile.test -t bira-test:latest ."

# graft
image   = "graft-test:latest"
timeout = 10
build   = "docker build -f Dockerfile.test -t graft-test:latest ."

# bump (currently has no .smokorc — would add one)
image   = "bump-test:latest"
build   = "docker build -f Dockerfile.test -t bump-test:latest ."
```

After adoption, the Makefile targets simplify from:
```makefile
smoko: smoko-image
	smoko run specs/
smoko-image:
	docker build -f Dockerfile.test -t bira-test:latest .
```
To:
```makefile
smoko:
	smoko run specs/
```

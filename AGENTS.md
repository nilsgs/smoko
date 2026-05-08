# Smoko Agent Notes

## Layout

- `src/cmd/smoko/main.go`: Cobra CLI entry point and run orchestration.
- `src/internal/parser/`: `.smoko` lexer, parser, and AST types.
- `src/internal/config/`: `.smokorc` loading.
- `src/internal/docker/`: Docker SDK wrapper and container operations.
- `src/internal/executor/`: Given and When execution.
- `src/internal/assertions/`: Then assertion evaluation.
- `src/internal/reporter/`: text and JSON reporting.
- `src/internal/hints/`: fuzzy hints for unknown steps.
- `specs/`: Smoko specs for Smoko itself.
- `skills/smoko/`: agent-facing guidance for writing Smoko specs.

## Build And Test

Use Task targets for normal validation:

```sh
task test
task build
task smoke
task ci
```

Raw Go fallback for focused debugging:

```sh
cd src
go test ./... -v -count=1
```

## Smoke Tests

`task smoke` builds the local `dist/smoko` binary and runs `specs/` with that
binary.

Smoko's own smoke tests should exercise real CLI behavior. Keep scenarios small,
with one action per scenario.

## DSL Constraints

- A scenario has exactly one `When` step.
- `Background` contains only `Given` setup steps.
- `And` and `But` inherit the previous step type.
- Setup paths are confined to `/smoko-work`.
- Prefer first-class file, directory, JSON, and output assertions over shell
  assertions embedded in `When`.
- Build and Docker readiness messages go to stderr so JSON stdout stays
  parseable.

## Documentation

- Keep `README.md` as the concise user and developer front door.
- Put expanded usage in `docs/usage.md`.
- Put step syntax in `docs/dsl.md`.
- Put `.smokorc` and Docker image setup in `docs/configuration.md`.
- Keep agent-specific spec-writing guidance in `skills/smoko/SKILL.md`.
- Keep Markdown docs ASCII-only unless a non-ASCII character is deliberate.

## Versioning

The version comes from `VERSION` and is stamped into builds by the Task build
scripts.

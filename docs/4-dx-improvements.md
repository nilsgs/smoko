# 4 — DX Improvements

Three small, independent improvements to developer experience.

---

## 4a — Default `specs/` Path

### Problem

Every consumer uses `specs/` as the test directory, but you always have to type `smoko run specs/`. Running `smoko run` with no arguments produces an error.

### Desired State

```bash
smoko run          # looks for specs/ in cwd
smoko run specs/   # explicit path (still works)
smoko run test.smoko  # single file (still works)
```

### Implementation

File: `cmd/smoko/main.go`

Change:
```go
Args: cobra.ExactArgs(1),
```
To:
```go
Args: cobra.MaximumNArgs(1),
```

In the `RunE` function, default to `"specs"` when no argument is provided:
```go
path := "specs"
if len(args) > 0 {
    path = args[0]
}
// Verify path exists before proceeding
if _, err := os.Stat(path); err != nil {
    return fmt.Errorf("path %q not found (run 'smoko run <path>' or create a specs/ directory)", path)
}
```

### Tests

- Integration: run `smoko run` in a directory with `specs/` → should work
- Integration: run `smoko run` in a directory without `specs/` → clear error message

---

## 4b — `--list` Flag

### Problem

No way to see what scenarios exist without running them. Useful for:
- CI validation ("did someone add specs for the new command?")
- Orientation in unfamiliar test suites
- Quick check after editing spec files

### Desired State

```
$ smoko run specs/ --list
specs/init.smoko
  Feature: bira init
    · creates project config
    · fails on re-init

specs/task.smoko
  Feature: bira task
    · Task add creates a task
    · Task add --json returns structured output
    · Task add to a specific feature
    ...

2 features, 5 scenarios
```

### Implementation

File: `cmd/smoko/main.go`

Add flag:
```go
var list bool
cmd.Flags().BoolVar(&list, "list", false, "List scenarios without running them")
```

In `runTests()`, after parsing all features, if `list` is set:
```go
if list {
    totalFeatures := 0
    totalScenarios := 0
    for _, file := range files {
        features, err := parseFile(file)
        // ...
        for _, f := range features {
            totalFeatures++
            fmt.Printf("%s\n  Feature: %s\n", file, f.Name)
            for _, s := range f.Scenarios {
                totalScenarios++
                fmt.Printf("    · %s\n", s.Name)
            }
            fmt.Println()
        }
    }
    fmt.Printf("%d features, %d scenarios\n", totalFeatures, totalScenarios)
    return nil
}
```

No Docker connection needed. No image pulling. Just parse and print.

### Tests

- Unit: parse a known .smoko file, verify list output format
- Verify `--list` does not require Docker

---

## 4c — Fuzzy Error Hints for Unrecognized Steps

### Problem

When a step doesn't match any known pattern, smoko prints:
```
unknown Then assertion: "stderr match pattern 'error'"
```

The user has to go read docs to figure out what they got wrong. Common mistakes:
- `match` instead of `matches`
- `equal` instead of `equals`
- Missing quotes
- Wrong keyword order

### Desired State

```
✗ unknown Then assertion: "stderr match pattern 'error'"
  → did you mean: "stderr matches pattern \"...\""?
```

### Implementation

#### Step 1 — Define canonical patterns

Create a list of human-readable pattern templates:

```go
var knownThenPatterns = []string{
    `exit code is <N>`,
    `exit code is not <N>`,
    `output contains "<text>"`,
    `output does not contain "<text>"`,
    `stdout contains "<text>"`,
    `stderr contains "<text>"`,
    `output matches pattern "<regex>"`,
    `stdout matches pattern "<regex>"`,
    `stderr matches pattern "<regex>"`,
    `output equals "<text>"`,
    `file "<path>" exists`,
    `file "<path>" does not exist`,
    `file "<path>" contains "<text>"`,
    `file "<path>" matches pattern "<regex>"`,
    `file "<path>" equals "<text>"`,
    `output is empty`,
    `output as JSON at path "<jsonpath>" equals <value>`,
    // ... etc
}
```

Similarly for Given and When patterns.

#### Step 2 — Levenshtein distance (MVP)

Implement a simple Levenshtein distance function. Go's standard library doesn't include one, but it's ~20 lines:

```go
func levenshtein(a, b string) int {
    // standard dynamic programming implementation
}
```

#### Step 3 — Find closest match

When an unknown step is encountered, normalize both the input and each pattern (lowercase, collapse whitespace, strip quoted content), compute distance, and suggest the closest match if the distance is below a threshold (e.g., distance < len(input)/3):

```go
func suggestStep(text string, patterns []string) string {
    // normalize: strip quoted values, lowercase
    normalized := normalizeForMatch(text)
    best := ""
    bestDist := math.MaxInt
    for _, p := range patterns {
        d := levenshtein(normalized, normalizeForMatch(p))
        if d < bestDist {
            bestDist = d
            best = p
        }
    }
    if bestDist <= len(normalized)/3 {
        return best
    }
    return ""
}
```

#### Step 4 — Integrate into error messages

In `assertions.go` line 396:
```go
if suggestion := suggestStep(text, knownThenPatterns); suggestion != "" {
    return fail("unknown Then assertion: %q\n  → did you mean: %q?", text, suggestion)
}
return fail("unknown Then assertion: %q", text)
```

Same for `executor.go` lines 111 and 273.

### Tests

- `TestSuggestStepTypo` — "stderr match pattern" → suggests "stderr matches pattern"
- `TestSuggestStepNoMatch` — completely unrelated text → no suggestion
- `TestSuggestGivenTypo` — "a file exists at 'path'" → suggests `a file "..." exists`

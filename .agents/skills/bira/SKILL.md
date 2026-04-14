---
name: bira
description: Lightweight local CLI for capturing ideas, bugs, and features in AI-assisted development workflows. Agent-driven with CLI and MCP server support.
---

# bira skill

bira is a lightweight local CLI for capturing **ideas**, **bugs**, and **features** in AI-assisted development workflows. It is designed to be driven by agents — either via the CLI or via an MCP server — alongside normal human use.

---

## Interface selection

| Interface | When to use |
|---|---|
| **MCP server** | IDE-embedded agents (Copilot in VS Code, Cursor, Claude Desktop). Structured tool calls, no shell required. |
| **CLI** | Shell-based agents, scripts, terminal workflows. Add `--json` for machine-readable output. |

Both interfaces read and write the same `~/.bira` store — they can be used interchangeably and their output is immediately consistent.

**MCP config** (`mcp.json` or VS Code settings):
```json
{
  "mcpServers": {
    "bira": {
      "command": "bira",
      "args": ["mcp"]
    }
  }
}
```

---

## When to invoke bira

Use bira when asked to (or when it makes sense to):

- "Capture this idea / I just had an idea…"
- "File a bug against `<tool>`"
- "Triage this idea / bug / feature"
- "Check what's triaged / what's ready to work on"
- "Start working on a feature / bug"
- "Mark this bug as fixed / feature as done"
- "Add a note to this bug/feature"
- "Write a plan for this feature"

---

## Entity types & lifecycles

### Idea
A raw, quickly-captured thought. No required fields beyond a title.

```
inbox → triaged → promoted  (idea becomes a Feature)
               → rejected
```

Triage fields: `priority` (low / medium / high), `tags`

### Bug
A defect filed against a project, potentially from a different repo.

```
open → triaged → in-progress → fixed
                             → wont-fix
```

Triage fields: `criticality` (low / medium / high / critical), `tags`

### Feature
A well-defined unit of work, directly created or promoted from an Idea.

```
proposed → triaged → in-progress → done
                                 → rejected
```

Triage fields: `impact` (low / medium / high), `complexity` (low / medium / high), `tags`

---

## Workflow recipes — CLI

All commands accept `--json` for clean machine-readable output. Errors always go to **stderr**; stdout is pure data.

### 1. Quick idea capture
```sh
bira idea add "My idea title" [--desc "detail"] [--tags "tag1,tag2"]
```

### 2. Refine an idea (write a plan sidecar)
```sh
# Read current state
bira idea show <id> --json

# Write a plan (agent writes markdown)
echo "## Approach\n..." | bira idea plan <id> --stdin

# Read back the plan
bira idea plan <id>
```

### 3. Triage an idea (cooperative loop)
```sh
bira idea show <id> --json
# Agent proposes values, explains reasoning to human, then:
bira idea triage <id> --priority high [--tags "ux,perf"] --json
```

### 4. Promote idea → feature
```sh
bira idea promote <id> --json
# Returns the new Feature. Plan sidecar is carried over if present.
```

### 5. File a cross-project bug
```sh
# -p accepts a project name slug (lowercase, hyphenated) or exact project ID
bira bug create -p other-tool "Parser crashes on empty input" \
  --desc "Steps: run parser with empty string. Stack trace: ..." \
  --reported-by "copilot@my-tool" --json
```

### 6. Triage a bug
```sh
bira bug triage <id> --criticality high [--tags "parser,crash"] --json
```

### 7. Fix a bug
```sh
bira bug start <id> --json
# ...implement fix...
bira bug done <id> --json
# or: bira bug wont-fix <id> --note "Out of scope" --json
```

### 8. Add an investigation plan to a bug
```sh
echo "## Reproduction\n...\n## Fix plan\n..." | bira bug plan <id> --stdin
```

### 9. List and pick a feature to implement
```sh
bira feature list --status triaged --json
bira feature show <id> --json | jq -r '.plan'   # read plan sidecar
```

### 10. Implement a feature
```sh
bira feature start <id> --json
# Write/update plan as work progresses:
echo "## Plan\n..." | bira feature plan <id> --stdin
# Mark done:
bira feature done <id> --json
```

### 11. Project context
```sh
bira context --json              # counts by entity type
bira context --full --json       # breakdown by status for each type
bira project list --json         # list all known projects (useful for cross-project filing)
```

---

## Workflow recipes — MCP tools

The MCP server exposes the same operations as structured tool calls. Use `project_id` (8-char hex) for all tools except `create_bug` which also accepts a name slug.

### Capture an idea
```
create_idea(project_id, title, description?, tags?)
```

### Triage an idea
```
triage_idea(project_id, id, priority?, tags?)
```

### Promote idea to feature
```
promote_idea(project_id, id)
→ returns the new Feature object
```

### File a cross-project bug
```
create_bug(project_id, title, description?, reported_by?, tags?)
# project_id can be a name slug (e.g. "my-other-tool") or an exact ID
```

### Triage a bug
```
triage_bug(project_id, id, criticality?, tags?)
```

### Fix a bug
```
start_bug(project_id, id)
done_bug(project_id, id)
```

### Work on a feature
```
list_features(project_id, status?)          → pick "triaged" items
get_feature_plan(project_id, id)            → read plan sidecar
start_feature(project_id, id)              → mark in-progress
set_feature_plan(project_id, id, content)  → update plan as work progresses
done_feature(project_id, id)               → mark complete
```

### Read/write plan sidecars
```
get_idea_plan(project_id, id)
set_idea_plan(project_id, id, content)
get_bug_plan(project_id, id)
set_bug_plan(project_id, id, content)
get_feature_plan(project_id, id)
set_feature_plan(project_id, id, content)
```

### Project overview
```
list_projects()
get_context(project_id)
```

### Full MCP tool list

| Tool | Description |
|---|---|
| `list_projects` | List all bira projects |
| `get_context` | Idea/bug/feature counts for a project |
| `create_idea` | Capture a new idea |
| `list_ideas` | List ideas (filter by status) |
| `get_idea` | Get idea details + plan |
| `triage_idea` | Set priority/tags, status → triaged |
| `promote_idea` | Promote to feature, carry over plan |
| `get_idea_plan` | Read plan sidecar |
| `set_idea_plan` | Write plan sidecar |
| `create_bug` | File a bug report |
| `list_bugs` | List bugs (filter by status/criticality) |
| `get_bug` | Get bug details + plan |
| `triage_bug` | Set criticality/tags, status → triaged |
| `start_bug` | Status → in-progress |
| `done_bug` | Status → fixed |
| `get_bug_plan` | Read plan sidecar |
| `set_bug_plan` | Write plan sidecar |
| `create_feature` | Create a feature directly |
| `list_features` | List features (filter by status) |
| `get_feature` | Get feature details + plan |
| `triage_feature` | Set impact/complexity/tags, status → triaged |
| `start_feature` | Status → in-progress |
| `done_feature` | Status → done |
| `get_feature_plan` | Read plan sidecar |
| `set_feature_plan` | Write plan sidecar |

---

## Triage guide

Triage is a **cooperative loop** — the agent proposes values and the human confirms or adjusts. An agent should:

1. Read the current item (`bira <type> show <id> --json` or `get_<type>`)
2. Analyse the title, description, and any plan sidecar content
3. Propose triage values and explain the reasoning to the human
4. Apply the agreed values, then proceed (or wait for human direction)

| Entity | Fields | What they enable |
|---|---|---|
| Idea | `priority` low/medium/high | Sortable queue for promotion decisions |
| Bug | `criticality` low/medium/high/critical | Agents filter by criticality; critical bugs surface first |
| Feature | `impact` low/medium/high + `complexity` low/medium/high | Agents can rank by impact/effort ratio |

---

## Plan sidecars

Every idea, bug, and feature has an **optional** Markdown plan sidecar stored as `<id>.plan.md` alongside the JSON data file. Write to it at any time.

- **Idea plan** — early thinking, research notes, rough sketches before promotion
- **Bug plan** — reproduction steps, investigation findings, proposed fix
- **Feature plan** — full implementation plan the agent will execute

The `plan` field is always included in `--json` / `get_*` output (empty string if no sidecar).

When an idea is promoted to a feature, its plan sidecar is **automatically copied** to the new feature as a starting point.

---

## JSON output contract (CLI)

- Every command accepts `--json` → clean JSON to stdout
- Errors go to **stderr** only — stdout is always pure data
- Exit codes: `0` success · `1` error · `2` not found
- Empty arrays serialize as `[]`, never `null`
- `show --json` always includes a `plan` field (raw Markdown, `""` if absent)
- IDs are 8-character hex strings

---

## Cross-project bug filing

The `-p` flag on `bira bug create` (CLI) and the `project_id` parameter on `create_bug` (MCP) accept:

- An **exact project ID** (8-char hex) — e.g. `a3f1c9b2`
- A **name slug** — project name lowercased, spaces/underscores replaced with hyphens — e.g. `my-other-tool`

To discover available projects:
```sh
bira project list --json          # CLI
list_projects()                   # MCP
```

---
name: curriculum
description: "Manage agentic skills using cur CLI. Use when publishing skills to a central repository, consuming skills in a repo, managing .curriculum manifests, or working with agentskills.io-compliant skill directories."
compatibility: "Requires cur CLI installed (Go 1.26+)"
metadata:
  category: "agent-skills"
  tags: "curriculum, skills, agentskills.io, packaging"
---

# curriculum

**curriculum** teaches agents how to use the `cur` CLI tool to manage agentic skills following the [agentskills.io specification](https://agentskills.io/specification).

Use this skill when:
- Publishing skills from a repository to a central skill store
- Consuming (installing) skills into a project
- Managing `.curriculum` manifests and skill declarations
- Working with agentskills.io-compliant skill directories
- Versioning skills with semver

---

## Core Concepts

### What is a Skill?

A skill is a reusable unit of instruction for agents. It follows the [agentskills.io spec](https://agentskills.io/specification):

```
skill-name/
├── SKILL.md          (Required: YAML frontmatter + instructions)
├── scripts/          (Optional: executable code)
├── references/       (Optional: additional docs)
├── assets/           (Optional: templates, images)
└── ...               (Any additional files)
```

**SKILL.md format:**
```yaml
---
name: skill-name              # lowercase, alphanumeric + hyphens, ≤64 chars
description: "..."            # What it teaches, when to use it (≤1024 chars)
license: "MIT"                 # (optional)
compatibility: "Node.js 16+"  # (optional) Environment requirements
metadata:                      # (optional) Custom key-value data
  category: "type"
  tags: "tag1, tag2"
---

# Skill Title

Detailed instructions for agents...
```

### `.curriculum` Manifest

Every repo that produces or consumes skills has a `.curriculum` file:

```json
{
  "version": "1.0.0",
  "skills": [
    { "name": "my-skill" },
    { "name": "other", "path": "custom/location/other" }
  ],
  "dependencies": [
    { "name": "external-skill", "version": "1.2.0" },
    { "name": "any-skill" }
  ]
}
```

- **`version`** — Applied to all skills when pushing (bump to publish new versions)
- **`skills`** — Skills this repo *provides* (source: `skills/<name>/` by default)
- **`dependencies`** — Skills this repo *consumes* (installed to `.agents/skills/`)

### Central Repository

Skills are stored in `~/.curriculum/repository/`:

```
~/.curriculum/repository/
├── my-skill/
│   ├── 1.0.0/        (complete copy of skill directory)
│   │   ├── SKILL.md
│   │   ├── scripts/
│   │   └── ...
│   └── 2.0.0/
├── other-skill/
│   └── 1.0.0/
└── ...
```

---

## Typical Workflows

### Workflow 1: Publish a Skill

You maintain a skill in your repo and want to share it.

```bash
# 1. Initialize (if not already done)
cur init

# 2. Create the skill directory
mkdir -p skills/my-skill
cat > skills/my-skill/SKILL.md << 'EOF'
---
name: my-skill
description: "Teaches agents to ..."
---

# My Skill

Instructions...
EOF

# 3. Declare in .curriculum
# Edit .curriculum, add to "skills":
# {
#   "version": "1.0.0",
#   "skills": [
#     { "name": "my-skill" }
#   ],
#   ...
# }

# 4. Push to central repo
cur push                    # Push all skills
# or
cur push my-skill           # Push one skill

# Verify
cur list | grep my-skill    # See it in the list
```

### Workflow 2: Use a Skill in Your Repo

You want to add a published skill as a dependency.

```bash
# 1. Go to your project repo
cd my-project

# 2. Initialize (if not already done)
cur init

# 3. Install the skill
cur install external-skill              # Latest version
cur install external-skill@1.2.0        # Specific version
cur install external-skill --save       # Also update .curriculum

# 4. Verify it's installed
ls -la .agents/skills/external-skill/

# 5. Install all declared dependencies at once
cur install                             # Reads .curriculum dependencies[]
```

### Workflow 3: Install Global (Personal) Skills

You have skills you use across many projects.

```bash
# Install a skill globally (to ~/.agents/skills/)
cur install my-skill --global

# It's now available to all repos on this machine
# but NOT listed in any single repo's .curriculum

# Remove it later
cur remove my-skill --global
```

### Workflow 4: Update a Skill Version

You improved your skill and want to publish a new version.

```bash
# 1. Make changes to skills/my-skill/
# 2. Update .curriculum version
# {
#   "version": "2.0.0",   # Bumped from 1.0.0
#   ...
# }

# 3. Push the new version
cur push my-skill
# → Published to ~/.curriculum/repository/my-skill/2.0.0/

# 4. Other repos can now install it
cur install my-skill@2.0.0 --save
```

---

## Command Patterns

### `cur init`

Initialize a repo to use curriculum.

```bash
cur init
# Creates: .curriculum with { "version": "0.1.0", "skills": [], "dependencies": [] }
```

### `cur push`

Push skills to the central repository.

```bash
cur push                    # Push all skills in .curriculum
cur push my-skill           # Push one skill
cur push --json             # JSON output (machine-readable)
```

**Requirements:**
- `.curriculum` must exist (run `cur init`)
- Top-level `version` must be set
- Each skill must have a valid `SKILL.md` with matching `name`

**Result:**
- Each skill is copied to `~/.curriculum/repository/<name>/<version>/`

### `cur install`

Install skills from the central repository.

```bash
cur install                             # Install all dependencies from .curriculum
cur install my-skill                    # Install latest version of one skill
cur install my-skill@1.2.0              # Install specific version
cur install my-skill --save             # Also update .curriculum dependencies
cur install my-skill --global           # Install to ~/.agents/skills/ (personal)
cur install my-skill@1.0.0 --save --global  # Combine flags
```

**Behavior:**
- Default destination: `.agents/skills/<name>/` (repo-local)
- `--global`: destination is `~/.agents/skills/<name>/` (personal, shared across repos)
- `--save`: automatically adds/updates the skill in `.curriculum` `dependencies[]`
- If version is omitted, installs the latest available version

### `cur remove`

Remove an installed skill.

```bash
cur remove my-skill                     # Remove from .agents/skills/my-skill/
cur remove my-skill --global            # Remove from ~/.agents/skills/my-skill/
cur remove my-skill --save              # Also remove from .curriculum dependencies
```

### `cur list`

List all available skills in the central repository.

```bash
cur list                    # Human-readable table
cur list --json             # JSON output

# Example output:
# SKILL       VERSIONS
# my-skill    1.0.0, 2.0.0 *
# other       0.5.0 *
```

The `*` marks the latest version.

---

## Common Patterns

### Pattern 1: Maintaining Multiple Skills

A single repo can provide multiple skills:

```json
{
  "version": "1.5.0",
  "skills": [
    { "name": "skill-one" },
    { "name": "skill-two" },
    { "name": "skill-three", "path": "agent-tools/skill-three" }
  ],
  "dependencies": []
}
```

All three are pushed with version `1.5.0`:
```bash
cur push
# → Publishes to:
#   ~/.curriculum/repository/skill-one/1.5.0/
#   ~/.curriculum/repository/skill-two/1.5.0/
#   ~/.curriculum/repository/skill-three/1.5.0/
```

### Pattern 2: Custom Skill Paths

By default, skills are in `skills/<name>/`. Override with `path`:

```json
{
  "skills": [
    { "name": "my-skill" },
    { "name": "legacy", "path": "old-agents/legacy-skill" },
    { "name": "nested", "path": "src/agent-skills/nested" }
  ]
}
```

### Pattern 3: Locking Dependency Versions

Pin specific versions for reproducibility:

```json
{
  "dependencies": [
    { "name": "auth-skill", "version": "2.1.0" },
    { "name": "logging-skill", "version": "1.0.0" },
    { "name": "utils", "version": "3.2.1" }
  ]
}
```

Install all at once with locked versions:
```bash
cur install
# Installs auth-skill@2.1.0, logging-skill@1.0.0, utils@3.2.1
```

### Pattern 4: Flexible Dependency Versions

Omit version to always pull the latest:

```json
{
  "dependencies": [
    { "name": "frequently-updated" }
  ]
}
```

```bash
cur install
# Installs latest version of frequently-updated
```

Or mix both:
```json
{
  "dependencies": [
    { "name": "stable-api", "version": "1.0.0" },
    { "name": "rapid-dev" }
  ]
}
```

---

## Validation Rules

### Skill Names

Must match agentskills.io naming rules:
- 1–64 characters
- Lowercase alphanumeric + hyphens only
- Must not start/end with hyphen
- Must not contain consecutive hyphens

Valid: `my-skill`, `python-testing`, `a`, `skill-name-123`  
Invalid: `-skill`, `skill-`, `skill--name`, `MySkill`, `skill name`

### SKILL.md Requirements

- Must exist in the skill directory
- Must have YAML frontmatter with `name` and `description`
- Frontmatter `name` must match the manifest entry `name`
- Directory basename must match skill `name`

Example valid structure:
```
skills/
└── my-skill/           ← Directory: "my-skill"
    └── SKILL.md        ← Contains: name: my-skill
```

### Manifest Version Format

Must be valid semver (semantic versioning):
- Valid: `"0.1.0"`, `"1.0.0"`, `"2.1.0-beta"`, `"1.0.0+build"`
- Invalid: `"latest"`, `"1"`, `"1.0"`

---

## Environment Variables

### `CURRICULUM_HOME`

Override the location of the central repository.

```bash
# Default: ~/.curriculum/repository/
export CURRICULUM_HOME=/custom/path
cur push              # Uses /custom/path/repository/
cur list              # Lists from /custom/path/repository/
```

---

## Troubleshooting

| Issue | Solution |
|---|---|
| "no .curriculum found" | Run `cur init` in the repo root |
| "skill not found in repository" | Check `cur list` to see available skills |
| "skill name does not match" | Ensure directory basename = SKILL.md `name` |
| "SKILL.md missing required field" | Add `name` and `description` to YAML frontmatter |
| "skill not found at .agents/skills/X" | Install it first with `cur install X` |
| Installing to wrong location | Use `--global` for personal skills, omit for repo-local |

---

## References

- [agentskills.io Specification](https://agentskills.io/specification)
- `cur` README — Run `cur --help` or see `cur` repository
- Individual command help: `cur <command> --help`

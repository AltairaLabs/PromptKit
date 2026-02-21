---
title: Skills Reference
description: Complete reference for SKILL.md format, pack configuration, and SDK API
sidebar:
  order: 4
---

Reference for skill definitions, configuration, and SDK integration.

---

## SKILL.md Format

Each skill is a directory containing a `SKILL.md` file with YAML frontmatter and Markdown instructions:

```yaml
---
name: pci-compliance                    # Required, kebab-case, max 64 chars
description: PCI DSS compliance rules   # Required, max 1024 chars
license: MIT                            # Optional
compatibility: ">=1.0.0"               # Optional
allowed-tools:                          # Optional — tools granted on activation
  - refund
  - process_payment
metadata:                               # Optional — arbitrary key-value pairs
  tags: "compliance, payments"
  author: "security-team"
---

# Instructions

Markdown body loaded when the skill is activated (Phase 2).
Supports full Markdown formatting.
```

### Required Fields

| Field | Constraints |
|-------|------------|
| `name` | Kebab-case (`[a-z0-9-]+`), max 64 characters |
| `description` | Max 1024 characters |

### Optional Fields

| Field | Description |
|-------|------------|
| `license` | SPDX license identifier |
| `compatibility` | Semver range for framework compatibility |
| `allowed-tools` | List of tool names granted when skill activates |
| `metadata` | Arbitrary key-value string pairs (e.g., tags, author) |

---

## Pack Configuration

### Directory-Based Skills

Reference a directory containing skill subdirectories:

```json
{
  "skills": [
    {"path": "skills"},
    {"path": "skills/billing"}
  ]
}
```

The `path` field (or `dir` alias) points to a directory relative to the pack file. All subdirectories containing a `SKILL.md` are discovered automatically.

### Inline Skills

Define skills directly in the pack without SKILL.md files:

```json
{
  "skills": [
    {
      "name": "greeting",
      "description": "Standard greeting protocol",
      "instructions": "Always greet the customer by name."
    }
  ]
}
```

Inline skills require `name`, `description`, and `instructions`.

### Preload

Mark a skill source as preloaded to activate it at startup:

```json
{
  "skills": [
    {"path": "skills/brand-voice", "preload": true}
  ]
}
```

Preloaded skills are activated before the first LLM message — their instructions and tools are always available.

---

## Shared Skills

Skills can be installed from Git repositories using the `@org/name` reference syntax and shared across projects.

### Reference Syntax

Use `@org/name` in the pack `skills` array to reference an installed skill:

```json
{
  "skills": [
    {"path": "@anthropic/pci-compliance"},
    {"path": "@anthropic/refund-processing"},
    {"path": "skills/local-skill"}
  ]
}
```

The `@org/name` prefix tells the runtime to look up the skill in the installed skill directories rather than resolving it as a relative path.

### Installation Locations

| Scope | Directory |
|-------|-----------|
| **User-level** | `~/.config/promptkit/skills/org/name/` |
| **Project-level** | `.promptkit/skills/org/name/` |

Install skills with the CLI:

```bash
# User-level (default)
promptarena skill install @anthropic/pci-compliance

# Project-level
promptarena skill install @anthropic/pci-compliance --project

# Into a specific workflow stage directory
promptarena skill install @anthropic/pci-compliance --into ./skills/billing
```

### Resolution Order

When the runtime encounters a skill reference, it resolves in this order:

1. **Inline** — skill defined directly in the pack JSON
2. **Local directory** — relative `path` resolved from the pack file location
3. **Project-level** — `.promptkit/skills/org/name/`
4. **User-level** — `~/.config/promptkit/skills/org/name/`

The first match wins. This lets you override a user-level skill with a project-specific version.

---

## Workflow State Skills

Workflow states can filter which skills are available using the `skills` field:

```json
{
  "workflow": {
    "states": {
      "billing": {
        "prompt_task": "billing",
        "skills": "skills/billing"
      },
      "intake": {
        "prompt_task": "intake"
      },
      "closed": {
        "prompt_task": "closed",
        "skills": "none"
      }
    }
  }
}
```

| Value | Behavior |
|-------|----------|
| `"skills/billing"` | Only skills under this directory path are available |
| *(omitted)* | All pack-level skills are available |
| `"none"` | No skills available in this state |

---

## Skill Tools

Skills register three tools in the `skill__` namespace:

| Tool | Input | Output |
|------|-------|--------|
| `skill__activate` | `{"skill": "name"}` | Instructions text + list of added tools |
| `skill__deactivate` | `{"skill": "name"}` | List of removed tools |
| `skill__read_resource` | `{"skill_name": "name", "path": "file.md"}` | File content |

The `skill__activate` tool description includes the Phase 1 skill index — a list of available skills with their names and descriptions.

---

## SDK API

### Auto-Detection

Skills are auto-detected when the pack includes a `skills` section:

```go
conv, _ := sdk.Open("support.pack.json", "assistant")
// Skills capability inferred from pack.Skills
```

### Programmatic Configuration

```go
conv, _ := sdk.Open("base.pack.json", "assistant",
    sdk.WithSkillsDir("./skills"),
    sdk.WithMaxActiveSkillsOption(5),
    sdk.WithSkillSelectorOption(selector),
)
```

| Option | Description | Default |
|--------|-------------|---------|
| `WithSkillsDir(dir)` | Add a skill source directory | — |
| `WithMaxActiveSkillsOption(n)` | Max concurrently active skills | 5 |
| `WithSkillSelectorOption(s)` | Skill selection strategy | ModelDriven |

### Skill Selectors

| Selector | When to use |
|----------|-------------|
| `ModelDrivenSelector` | Default — LLM decides which skills to activate |
| `TagSelector` | Pre-filter by metadata tags |
| `EmbeddingSelector` | RAG-based selection for large skill sets (50+) |

---

## Arena Assertions

Test skill behavior in Arena scenarios:

### `skill_activated` (conversation-level)

Asserts specific skills were activated at least N times:

```yaml
conversation_assertions:
  - type: skill_activated
    params:
      skill_names: ["pci-compliance", "refund-processing"]
      min_calls: 1    # optional, default 1
```

### `skill_not_activated` (conversation-level)

Asserts specific skills were never activated:

```yaml
conversation_assertions:
  - type: skill_not_activated
    params:
      skill_names: ["escalation-policy"]
```

---

## Directory Layout

Recommended organization for skills with workflow integration:

```
skills/
├── billing/                    # Scoped to billing workflow state
│   ├── pci-compliance/
│   │   └── SKILL.md
│   └── refund-processing/
│       └── SKILL.md
├── orders/                     # Scoped to orders workflow state
│   └── order-troubleshooting/
│       └── SKILL.md
├── escalation/                 # Available when not filtered
│   └── escalation-policy/
│       └── SKILL.md
└── brand-voice/                # Top-level — available everywhere
    └── SKILL.md
```

See the [workflow-skills example](https://github.com/AltairaLabs/PromptKit/tree/main/examples/workflow-skills) for a complete working implementation.

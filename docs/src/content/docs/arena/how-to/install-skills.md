---
title: Install Skills
---

## Goal

Install, list, and remove shared skills from Git repositories or local paths.

## Prerequisites

- PromptKit CLI (`promptarena`) installed

## Install a Skill

Install a skill from a Git repository:

```bash
promptarena skill install @anthropic/pdf-processing
```

Install a specific version (Git tag or ref):

```bash
promptarena skill install @anthropic/pdf-processing@v1.0.0
```

Install from a local directory:

```bash
promptarena skill install ./path/to/my-skill
```

**What happens:**

1. For `@org/name` references, the CLI clones the repository from `https://github.com/org/name`
2. If a version is specified, it checks out that Git tag or ref
3. The skill is installed to the user-level directory: `~/.config/promptkit/skills/org/name/`
4. The CLI verifies the directory contains a valid `SKILL.md` file

## Install to Project Level

Use the `--project` flag to install into the project-local directory instead of user-level:

```bash
promptarena skill install @anthropic/pci-compliance --project
```

This installs to `.promptkit/skills/anthropic/pci-compliance/` relative to the current directory. Project-level skills take priority over user-level skills during resolution.

## Install into a Workflow Stage Directory

Use the `--into` flag to install a skill directly into a specific directory — useful for placing skills alongside workflow stage configurations:

```bash
promptarena skill install @anthropic/pci-compliance --into ./skills/billing
```

This installs to `./skills/billing/pci-compliance/`. The `--into` flag is mutually exclusive with `--project`.

## List Installed Skills

See all installed skills grouped by location:

```bash
promptarena skill list
```

**Expected output:**

```
Project:
  @anthropic/pci-compliance  (.promptkit/skills/anthropic/pci-compliance)

User:
  @anthropic/pdf-processing  (~/.config/promptkit/skills/anthropic/pdf-processing)
```

## Remove a Skill

Remove an installed skill:

```bash
promptarena skill remove @anthropic/pdf-processing
```

This removes the skill directory from the installed location.

## Using Installed Skills in Pack YAML

Reference installed skills in your pack's `skills` array using the `@org/name` prefix:

```json
{
  "skills": [
    {"path": "@anthropic/pci-compliance"},
    {"path": "@anthropic/refund-processing"},
    {"path": "skills/local-skill", "preload": true}
  ]
}
```

The runtime resolves `@org/name` references from the installed skill directories automatically.

## Skill Discovery

When the runtime encounters a skill reference, it resolves in this order:

1. **Inline** — skill defined directly in the pack JSON
2. **Local directory** — relative `path` resolved from the pack file location
3. **Project-level** — `.promptkit/skills/org/name/`
4. **User-level** — `~/.config/promptkit/skills/org/name/`

The first match wins, so project-level installations override user-level ones.

## See Also

- [Skills Reference](/reference/skills) — SKILL.md format, pack configuration, and SDK API
- [CLI Commands](/arena/reference/cli-commands#promptarena-skill) — Complete skill command reference

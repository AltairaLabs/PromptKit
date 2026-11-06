---
title: Skills reference
description: Complete reference for SKILL.md format, pack configuration, skill tools, workflow filters, and SDK options
sidebar:
  order: 4
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - pkg/config/runtime_config.go
    - runtime/evals/handlers/register.go
    - runtime/evals/handlers/skill_activated.go
    - runtime/evals/handlers/skill_activation_order.go
    - runtime/evals/handlers/skill_not_activated.go
    - runtime/prompt/schema/promptpack.schema.json
    - runtime/selection/selector.go
    - runtime/skills/active_set.go
    - runtime/skills/executor.go
    - runtime/skills/installer.go
    - runtime/skills/parser.go
    - runtime/skills/registry.go
    - runtime/skills/tool_executor.go
    - runtime/skills/types.go
    - sdk/capability.go
    - sdk/capability_skills.go
    - sdk/conversation.go
    - sdk/options.go
    - sdk/runtime_config.go
    - sdk/sdk.go
    - sdk/workflow.go
---

Reference for skill definitions, pack configuration, the `skill__` tools, workflow state filters, and the SDK options that configure skills. For what skills are and when to use them, see [Skills](/concepts/skills/).

## SKILL.md format

A skill is a directory containing a `SKILL.md` file: YAML frontmatter between `---` lines, then a Markdown body. The runtime loads the frontmatter at discovery and the body when the skill activates.

```markdown
---
name: pci-compliance
description: PCI DSS compliance rules
license: MIT
compatibility: ">=1.0.0"
allowed-tools:
  - refund
  - process_payment
metadata:
  tags: "compliance, payments"
  author: "security-team"
---

# Instructions

Markdown body returned when the skill activates.
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Kebab-case (`^[a-z0-9]+(-[a-z0-9]+)*$`), at most 64 characters. Unique across all sources. |
| `description` | string | yes | At most 1024 characters. Appears in the `skill__activate` index. |
| `license` | string | no | Free-form license name. The runtime does not interpret it. |
| `compatibility` | string | no | Free-form string. The runtime does not interpret it. |
| `allowed-tools` | string[] | no | Tools the skill grants on activation, capped by the pack's declared tools. |
| `metadata` | map of string to string | no | Arbitrary key-value pairs. |

A file that does not start with `---`, lacks the closing `---`, or fails the constraints above fails discovery with an error naming the file.

### Tool grants

On activation, a skill grants the tools in `allowed-tools` that the pack declares under `tools`. Names the pack does not declare are ignored. Tools the prompt already lists stay available without a grant. `skill__activate` returns the granted names in `added_tools`.

Deactivating a skill removes a granted tool only when no other active skill still grants it.

## Pack configuration

The pack's `skills` array lists skill sources. Each entry takes one of three shapes.

| Shape | Example | Description |
|-------|---------|-------------|
| String | `"skills/billing"` | A directory path or an `@org/name` reference. Same as `{"path": ...}`. |
| Path source | `{"path": "skills", "preload": true}` | A directory or `@org/name` reference, with optional `preload`. |
| Inline skill | `{"name": ..., "description": ..., "instructions": ...}` | A skill defined in the pack. The pack schema requires all three fields. |

```json
{
  "skills": [
    "skills/billing",
    {"path": "skills/brand-voice", "preload": true},
    {
      "name": "greeting",
      "description": "Standard greeting protocol",
      "instructions": "Always greet the customer by name."
    }
  ]
}
```

A directory source registers every `SKILL.md` found beneath it, at any depth. Relative paths resolve against the process working directory, not the pack file's directory.

An inline skill has no directory, so `skill__read_resource` fails for it and a path filter on a workflow state never admits it.

### Preload

A source with `preload: true` registers its skills as preloaded. The SDK activates preloaded skills into each conversation's active set when the conversation opens, so their `allowed-tools` grants apply from the first round. Their instructions are appended to the system prompt on every turn, under a `## Skill: <name>` heading. If a workflow state's `skills` filter later drops a preloaded skill, its instructions leave the prompt with it. Each preloaded skill costs context on every turn. Preloaded skills count toward the maximum active skills limit. A preload that fails is logged at WARN and does not abort `Open`.

### Name collisions

Skill names are unique across all sources. When two sources declare the same name, the first source listed wins: its instructions, description, and `allowed-tools` are registered, and the later copy's content is dropped. The only thing merged from the later copy is `preload: true`, which upgrades the registered skill to preloaded.

Every collision is logged at WARN, naming the live and the dropped copy:

```text
WARN skills: duplicate skill name, keeping first  skill=refund-policy kept=/opt/skills/base/refund-policy dropped=/opt/skills/tenant-a/refund-policy preload_upgraded=false
```

An override must be listed before the base source, or have a distinct name.

## Shared skills

A source path starting with `@` is a reference to an installed skill, written `@org/name` or `@org/name@version`. Discovery resolves the reference to an installed directory and fails when the skill is not installed. Any other path is read as a directory.

```json
{
  "skills": [
    "@anthropic/pci-compliance",
    "skills/local-skill"
  ]
}
```

| Scope | Directory | Lookup order |
|-------|-----------|--------------|
| Project | `.promptkit/skills/<org>/<name>/` under the working directory | First |
| User | `$XDG_CONFIG_HOME/promptkit/skills/<org>/<name>/`, or `~/.config/promptkit/skills/<org>/<name>/` when `XDG_CONFIG_HOME` is unset | Second |

The first directory that exists wins, so a project-level install overrides a user-level one.

`promptarena skill install` clones a skill from `https://github.com/<org>/<name>` into one of these locations:

| Command | Destination |
|---------|-------------|
| `promptarena skill install @org/name` | User directory |
| `promptarena skill install @org/name --project` | Project directory |
| `promptarena skill install @org/name --into ./skills/billing` | `./skills/billing/<name>/` |

`--project` and `--into` are mutually exclusive. The cloned repository must contain a `SKILL.md`.

## Workflow state skills

A workflow state's `skills` field restricts which skills a conversation can use while the workflow is in that state.

```json
{
  "workflow": {
    "states": {
      "billing": {"prompt_task": "billing", "skills": "skills/billing"},
      "intake": {"prompt_task": "intake"},
      "closed": {"prompt_task": "closed", "skills": "none"}
    }
  }
}
```

| Value | Effect |
|-------|--------|
| Omitted or empty | All registered skills are available. |
| `"none"` (case-insensitive) | No skills are available. |
| A directory or `SKILL.md` path | Skills at or below that directory are available. A relative path resolves like a pack `skills` source. |
| A glob (contains `*`, `?` or `[`) | Skills whose path matches, using `filepath.Match`. |

The filter applies when the workflow opens, when it resumes, and on each transition, including a transition that happens mid-turn. A state without the field clears the previous state's filter.

When a filter applies:

- Active skills the filter excludes are deactivated, and the SDK logs their names at INFO.
- `skill__activate` refuses a skill the filter excludes with an error naming the filter.
- The `skill__activate` index lists only the skills the filter admits.
- When the filter admits no registered skill (for example `"none"`), the state is not offered `skill__activate`.

## Skill tools

The skills capability registers three tools in the `skill__` namespace.

| Tool | Input | Output |
|------|-------|--------|
| `skill__activate` | `name` (string, required) | `instructions` (string), `added_tools` (string[]) |
| `skill__deactivate` | `name` (string, required) | `removed_tools` (string[]) |
| `skill__read_resource` | `skill_name` (string), `path` (string), both required | `content` (string) |

Behavior:

- Activating an active skill returns its instructions again and is not an error.
- Activation fails when the skill does not exist, the state's filter excludes it, or the maximum active skills limit is reached.
- Deactivating a skill that is not active fails.
- `skill__read_resource` reads a file under the skill's directory. A path that escapes the directory, including through a symlink, fails.
- Active skills belong to one conversation. Conversations that share a tool registry do not see each other's activations.

The `skill__activate` description carries the skill index:

```text
Available skills:
- pci-compliance: PCI DSS compliance rules
- refund-processing: Refund handling steps
```

The index is `No skills available.` for an empty catalog. A `Conversation` rebuilds the index for each provider round from the conversation's current filter. When an external selector is bound, it narrows the index to the skills it ranks for the turn's user text. A selector that fails or returns nothing leaves the full admitted index.

## SDK

### Detection

The SDK adds the skills capability when the pack has a `skills` array, or when you pass `WithSkillsDir` or `WithSkillSource`. An explicit capability of the same name takes precedence over an inferred one.

```go
conv, err := sdk.Open("support.pack.json", "assistant")
```

### Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithSkillsDir(dir)` | Adds a directory source. Repeatable. | None |
| `WithSkillSource(skills.SkillSource)` | Adds a source with `MountAs`, `Preload`, or inline fields. Repeatable. | None |
| `WithMaxActiveSkillsOption(n)` | Maximum concurrently active skills per conversation. `n` must be positive. | Unlimited |
| `WithSelector(name, selection.Selector)` | Registers an in-process selector that a runtime config binds to skills by name. | None |
| `WithSkillSelectorOption(skills.SkillSelector)` | Narrows the `skill__activate` index with a `skills.SkillSelector`, such as `TagSelector`. | None |

`WithSkillsDir` and `WithSkillSource` add the skills capability when the pack declares no `skills`. When it does, or when you pass a capability with `WithCapability`, these sources merge with the capability's, and a source the capability already holds is not added twice. The merged sources belong to that conversation alone.

```go
conv, err := sdk.Open("base.pack.json", "assistant",
    sdk.WithSkillsDir("./skills"),
    sdk.WithSkillSource(skills.SkillSource{Dir: "./agentskills", MountAs: "skills"}),
    sdk.WithMaxActiveSkillsOption(5),
)
```

### skills.SkillSource

| Field | Type | Description |
|-------|------|-------------|
| `Dir` | string | Directory to walk for `SKILL.md` files. `Path` is an alias; `Dir` wins when both are set. |
| `Path` | string | Alias for `Dir`. |
| `Name`, `Description`, `Instructions` | string | Inline skill fields. Set `Name` and leave `Dir` empty. |
| `MountAs` | string | Virtual directory prefix presented to workflow filters. A literal prefix: globs and `..` segments are rejected, as is `MountAs` on an inline source. |
| `Preload` | bool | Activate the skills at conversation open. Combined with OR across duplicate sources. |

`MountAs` changes only the path that filters match. A skill at `./agentskills/billing/pci` mounted as `skills` matches a state filter of `skills/billing/*`. `skill__read_resource` always reads the real directory.

### Selectors

A selector narrows the `skill__activate` index per turn. `spec.skills.selector` in the runtime config names a selector declared under `spec.selectors`, an external process. A name not declared there fails runtime config validation.

```yaml
spec:
  selectors:
    skill-ranker:
      # declaration of an external selector process
  skills:
    selector: skill-ranker
```

An in-process `selection.Selector` (`Name`, `Init`, `Select`) registered with `WithSelector` under the same name takes precedence over the declared process. See [Runtime config reference](/sdk/reference/runtime-config/).

A `skills.SkillSelector` passed to `WithSkillSelectorOption` runs on the same path. It narrows the index once per turn, with the user's message as its query, and is not consulted for a turn with no text. `TagSelector` reads the comma-separated `tags` entry in a skill's `metadata`. An error or an empty choice lists every skill the state admits. A selector bound in the runtime config takes precedence over one passed to `WithSkillSelectorOption`.

### Programmatic capability

```go
cap := sdk.NewSkillsCapability(
    []skills.SkillSource{{Dir: "./skills"}},
    sdk.WithMaxActiveSkills(5),
)
conv, err := sdk.Open("base.pack.json", "assistant", sdk.WithCapability(cap))
```

You can pass one capability to many `Open` calls, including concurrent ones. The SDK never modifies it. An `Open` runs on its own copy when it adds something the capability lacks: skill sources, `WithSkillSelectorOption`, `WithMaxActiveSkillsOption`, or a skills selector bound by the runtime config (`spec.skills.selector`). Those settings reach only that conversation.

## Eval types

Three eval types check skill activation. Each reads `skill__activate` calls from the conversation's tool calls. `skill_activated` and `skill_not_activated` count only calls that succeeded; `skill_activation_order` reads every call that carries a skill name. See [Checks](/reference/checks/) for how checks attach to scenarios.

| Type | Params | Score |
|------|--------|-------|
| `skill_activated` | `skill_names` (string[]), `min_calls` (int, default 1) | The fraction of named skills activated at least `min_calls` times. |
| `skill_not_activated` | `skill_names` (string[]) | `1` when none of the named skills was activated, otherwise `0`. |
| `skill_activation_order` | `sequence` (string[], required) | The fraction of `sequence` matched, in order, by the activated skills. `0` when `sequence` is empty or no skill was activated. |

```yaml
conversation_assertions:
  - type: skill_activated
    params:
      skill_names: ["pci-compliance", "refund-processing"]
      min_calls: 1
  - type: skill_not_activated
    params:
      skill_names: ["escalation-policy"]
```

## Related

- [Skills](/concepts/skills/)
- [Checks](/reference/checks/)
- [Runtime config reference](/sdk/reference/runtime-config/)

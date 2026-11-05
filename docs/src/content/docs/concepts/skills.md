---
title: Skills
description: How skills load instructions and tools into a conversation on demand, using the AgentSkills.io format.
sidebar:
  order: 8
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/packspec/types.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/prompt/schema/promptpack.schema.json
    - runtime/skills/active_set.go
    - runtime/skills/executor.go
    - runtime/skills/parser.go
    - runtime/skills/tool_executor.go
    - runtime/skills/types.go
    - sdk/capability.go
    - sdk/capability_skills.go
    - sdk/conversation.go
    - sdk/options.go
    - sdk/sdk.go
---

Skills load knowledge on demand. Instead of placing every instruction in the system prompt, the model activates a skill when a turn needs it.

## Why skills exist

As an agent gains capabilities, its system prompt fills with instructions that most turns never use. A customer support agent might carry escalation procedures, PCI compliance rules, troubleshooting playbooks, and a product catalog, while any single turn uses a fraction of them.

| Problem | Impact |
|---------|--------|
| Context waste | 15-20K tokens of unused instructions per turn |
| Model confusion | Irrelevant context degrades accuracy and coherence |
| No reuse | Knowledge is embedded in pack prompts and cannot be shared |

Skills address all three by loading knowledge when the model asks for it.

## The AgentSkills.io format

PromptKit uses the [AgentSkills.io](https://agentskills.io/specification) skill format, which Claude Code, OpenAI Codex, GitHub Copilot, Cursor, and Strands Agents also adopt.

A skill is a directory containing a `SKILL.md` file:

```text
pci-compliance/
├── SKILL.md           # Required: metadata + instructions
├── references/        # Optional: supporting documents
└── assets/            # Optional: templates, data files
```

The file starts with YAML frontmatter and continues with Markdown instructions:

```markdown
---
name: pci-compliance
description: PCI DSS rules for handling payment card data. Use when the customer mentions billing, refunds, or payment information.
allowed-tools:
  - refund
  - process_payment
---

# PCI Compliance Guidelines

When handling payment card data, always follow these rules:

1. Never log or echo full card numbers...
2. Verify cardholder identity before processing refunds...
```

## Progressive disclosure

A skill loads in three phases, so an unused skill costs almost nothing:

| Phase | What loads | Token cost | When |
|-------|-----------|------------|------|
| 1. Discovery | Name + description only | ~50 tokens per skill | Always (startup) |
| 2. Activation | Full `SKILL.md` instructions | 1-5K tokens | On `skill__activate` |
| 3. Resources | Supporting files | As needed | On `skill__read_resource` |

With 8 skills installed, discovery costs about 400 tokens instead of about 8,000 for loading everything.

## Three-level tool scoping

Skills compose with the tool system in a hierarchy:

| Level | What it defines | Example |
|-------|----------------|---------|
| Pack | Every tool that can exist (the ceiling) | `tools: {get_order, refund, search, escalate}` |
| Prompt | Baseline tools for a task | `tools: [get_order, search]` |
| Skill | Additional tools on activation | `allowed-tools: [refund]` |

When a skill activates, its `allowed-tools` extend the prompt's tool set, capped at the tools the pack declares. A skill cannot grant a tool the pack does not define. Deactivating a skill withdraws the tools it granted, unless another active skill still grants them.

Controlling which skills a conversation can activate therefore controls which extra tools the model can reach.

## Skill tools

The skills capability registers three tools in the `skill__` namespace, alongside `a2a__`, `workflow__`, and `mcp__`:

- `skill__activate` loads a skill's instructions and extends the tool set.
- `skill__deactivate` removes a skill's instructions and retracts the tools it granted.
- `skill__read_resource` reads a file from a skill's directory.

The `skill__activate` description carries the discovery index: each available skill with its description. The model reads the index and decides which skill to activate.

## Skills in workflow states

A workflow state can limit which skills are available in it, with a path to a skill directory or the literal `none`:

```yaml
workflow:
  states:
    billing:
      prompt_task: billing_agent
      skills: ./skills/billing       # Only skills in this directory
    intake:
      prompt_task: intake_agent      # No skills field = all skills available
    closed:
      prompt_task: closing_agent
      skills: none                   # No skills can activate
```

Organize skills into subdirectories by concern:

```text
skills/
├── billing/
│   ├── pci-compliance/SKILL.md
│   └── refund-processing/SKILL.md
├── orders/
│   └── order-troubleshooting/SKILL.md
└── brand-voice/SKILL.md              # Outside billing/, so not available in the billing state
```

The filter applies when the workflow opens or resumes in a state and on every transition. Entering a state deactivates any active skill its filter excludes, along with the tools that skill granted.

## Using skills from the SDK

The SDK detects skills declared in the pack's `skills` section. You can also add skill sources in code:

```go
// Skills declared in the pack's skills section are detected automatically.
conv, _ := sdk.Open("support.pack.json", "assistant")

// Or add them programmatically.
conv, _ = sdk.Open("base.pack.json", "assistant",
    sdk.WithSkillsDir("./skills"),
    sdk.WithMaxActiveSkillsOption(5),
)
```

Activation fails when a conversation already holds the maximum number of active skills. Each conversation tracks its own active skills, so one conversation reaching the limit does not affect another.

## Design consequences

- A skill can be pure behavioral guidance, such as brand voice or communication style, with no tools. It can also bring knowledge and tools together, such as compliance rules plus the refund tool.
- The model drives activation. It reads the skill index and calls `skill__activate` when it decides the knowledge is needed, with no external orchestration.
- Skills are portable. The same `SKILL.md` works in any framework that supports AgentSkills.io, so you can install community skills, share skills across packs, or publish your own.

## Related

- [Skills Reference](/reference/skills/) lists the `SKILL.md` format, pack syntax, and SDK API.
- [Tools & MCP](/concepts/tools-mcp/) explains how tools work in PromptKit.
- [A2A](/concepts/a2a/) covers the Agent-to-Agent protocol.
- [SDK](/sdk/index/) covers SDK integration.

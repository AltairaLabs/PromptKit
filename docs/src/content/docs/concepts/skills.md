---
title: Skills
description: Demand-driven knowledge and instruction loading with AgentSkills.io
sidebar:
  order: 8
---

Skills provide **demand-driven knowledge loading** — instead of cramming all instructions into the system prompt, skills load only the knowledge the model needs, when it needs it.

---

## Why Skills?

As agents grow in capability, their system prompts bloat with instructions irrelevant to most turns. A customer support agent might need escalation procedures, PCI compliance rules, troubleshooting playbooks, and product catalogs — but any single turn only uses a fraction.

| Problem | Impact |
|---------|--------|
| **Context waste** | 15-20K tokens of unused instructions per turn |
| **Model confusion** | Irrelevant context degrades accuracy and coherence |
| **No reuse** | Knowledge is embedded in pack prompts, not shareable |

Skills solve all three by loading knowledge on demand.

---

## The AgentSkills.io Standard

PromptKit uses the [AgentSkills.io](https://agentskills.io/specification) standard — the portable skill format adopted by Claude Code, OpenAI Codex, GitHub Copilot, Cursor, and Strands Agents.

A skill is a directory containing a `SKILL.md` file:

```
pci-compliance/
├── SKILL.md           # Required — metadata + instructions
├── references/        # Optional — supporting documents
└── assets/            # Optional — templates, data files
```

```yaml
---
name: pci-compliance
description: PCI DSS rules for handling payment card data. Use when the customer mentions billing, refunds, or payment information.
allowed-tools: refund process_payment
---

# PCI Compliance Guidelines

When handling payment card data, always follow these rules:

1. Never log or echo full card numbers...
2. Verify cardholder identity before processing refunds...
```

---

## Progressive Disclosure

Skills load in three phases, saving 95-98% of tokens for unused skills:

| Phase | What loads | Token cost | When |
|-------|-----------|------------|------|
| **1. Discovery** | Name + description only | ~50 tokens/skill | Always (startup) |
| **2. Activation** | Full SKILL.md instructions | 1-5K tokens | On `skill__activate` |
| **3. Resources** | Supporting files | As needed | On `skill__read_resource` |

With 8 skills installed, Phase 1 costs ~400 tokens instead of ~8,000 for loading everything.

---

## Three-Level Tool Scoping

Skills compose with the existing tool system in a clean hierarchy:

| Level | What it defines | Example |
|-------|----------------|---------|
| **Pack** | All possible tools (the ceiling) | `tools: {get_order, refund, search, escalate}` |
| **Prompt** | Baseline tools for a task | `allowed_tools: [get_order, search]` |
| **Skill** | Additional tools on activation | `allowed-tools: refund` |

When a skill activates, its `allowed-tools` extend the prompt's tool set — but **never beyond the pack's declared tools**. The pack is the ceiling.

This means **controlling skills controls tools**. Don't want refund capability in a state? Don't include the skill that brings it.

---

## Skill Tools

Skills integrate via the `skill__` namespace, alongside `a2a__`, `workflow__`, and `mcp__`:

| Tool | Purpose |
|------|---------|
| `skill__activate` | Load a skill's instructions + extend tool set |
| `skill__deactivate` | Remove a skill's instructions + retract tools |
| `skill__read_resource` | Read a file from a skill's directory |

The `skill__activate` tool description includes the Phase 1 index — a list of available skills with descriptions. The model reads this and decides which to activate.

---

## Workflow Integration

Workflow states can optionally scope which skills are available using directory paths:

```yaml
workflow:
  states:
    billing:
      prompt_task: billing_agent
      skills: ./skills/billing       # Only skills in this directory
    intake:
      prompt_task: intake_agent      # No skills field = all skills available
```

Organize skills into subdirectories by concern:

```
skills/
├── billing/
│   ├── pci-compliance/SKILL.md
│   └── refund-processing/SKILL.md
├── orders/
│   └── order-troubleshooting/SKILL.md
└── brand-voice/SKILL.md              # Top-level = always available
```

---

## SDK Usage

```go
// Skills auto-detected from pack's skills section
conv, _ := sdk.Open("support.pack.json", "assistant")

// Or configure programmatically
conv, _ := sdk.Open("base.pack.json", "assistant",
    sdk.WithSkillsDir("./skills"),
    sdk.WithMaxActiveSkillsOption(5),
)
```

---

## Key Concepts

**Skills are knowledge, not just tools.** A skill can be pure behavioral guidance (brand voice, communication style) with no tools at all. Or it can bring knowledge *and* tools together — compliance rules plus the refund tool.

**The model drives activation.** The model reads the skill index and calls `skill__activate` when it determines knowledge is needed. No external orchestration required.

**Skills are portable.** The same SKILL.md works across any framework that supports AgentSkills.io. Install community skills, share across packs, or publish your own.

---

## Related

- [Skills Reference](/reference/skills/) — Complete SKILL.md format, pack syntax, and SDK API reference
- [Workflow + Skills Example](https://github.com/AltairaLabs/PromptKit/tree/main/examples/workflow-skills) — Working example with directory-based filtering
- [Tools & MCP](tools-mcp) — How tools work in PromptKit
- [A2A](a2a) — Agent-to-Agent protocol (agents can also be skills)
- [SDK Skills Options](/sdk/index/) — SDK integration details

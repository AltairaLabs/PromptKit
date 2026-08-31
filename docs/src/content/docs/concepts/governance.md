---
title: Governance
description: Declaring who is accountable for an agent, how autonomously it acts, and what its tools can affect
sidebar:
  order: 10
---

A pack can declare governance facts about the agent it defines — who is answerable for it, how far it acts without a human, what it was built for, and whether it must disclose itself as AI.

These are **human-declared claims, not enforcement**. PromptKit carries them, resolves them, and hands them to you. It does not act on them. Nothing in the runtime refuses a request because a pack declared a low autonomy level, and nothing gates a tool because it was marked irreversible.

That is deliberate, and it is the specification's design rather than a gap in the implementation. A declaration describes; the decisions belong to the host that knows its own policy, its own deployment, and its own obligations.

---

## Why declare it at all

Compliance regimes increasingly ask questions that are about a system, not about a model: who is accountable, what was this built for, does it act on its own, does it tell people it is AI. Those answers usually live in a spreadsheet maintained separately from the thing it describes, and they go stale the moment the agent changes.

Putting them in the pack makes them travel with the agent, version with it, and diff in review like anything else.

| Without | With |
|---------|------|
| Governance facts in a document, updated by hand | Declared beside the prompts they describe |
| Nothing tells you an agent's autonomy changed | A pack diff shows it |
| Each deployment re-derives what the agent is for | The pack states it once |

---

## Where governance goes

Two levels. Pack-level, under `metadata`:

```yaml
metadata:
  governance:
    intended_purpose: Triage inbound billing questions and draft replies.
    autonomy_level: acts_with_approval
    accountable_owner: billing-platform-team
    requires_ai_disclosure: true
    approved_environments: [staging, production]
```

And per-agent, on any member of `agents`:

```yaml
agents:
  entry: triage
  members:
    triage:
      description: Routes and answers billing questions.
    refunds:
      description: Issues refunds.
      governance:
        autonomy_level: suggests
```

Here the pack says its agents act with approval; the `refunds` agent narrows itself to only suggesting. Everything it does not restate — the owner, the purpose, the disclosure requirement — it inherits.

---

## The override rule

An agent's governance overrides the pack's **per field**:

- A field present on the agent replaces the pack value for that field.
- A field absent inherits.
- **Arrays and objects replace whole. They are not merged or appended.**

The last point is the one that catches people, because appending is the intuitive guess. It is worth being concrete about why replacement is right:

```yaml
metadata:
  governance:
    approved_environments: [staging, production]

agents:
  members:
    refunds:
      governance:
        approved_environments: [staging]
```

The `refunds` agent is cleared for **staging only**. If arrays merged, narrowing an agent's approved environments would be impossible — every attempt to restrict would silently re-grant everything the pack allowed. The same applies to `capabilities`, `foreseeable_misuse`, `intended_deployment_contexts`, `extensions` and `vocabularies`.

One consequence to know when authoring: because `vocabularies` replaces whole, an agent that declares any vocabulary prefix must declare every prefix it uses, including ones the pack already defined. The `dpv`, `eu-aiact` and `ai` prefixes are well-known defaults and never need declaring.

---

## The fields

All optional. Absence means undeclared — which is not the same as a default, and not the same as a denial.

| Field | Meaning |
|-------|---------|
| `intended_purpose` | What the agent is built to do, in the author's words |
| `autonomy_level` | How far it acts without a human — see below |
| `accountable_owner` | The role, team or function answerable for it. Prefer a durable identifier over a named person |
| `operator_role` | The declaring organization's role for this agent |
| `risk_classification` | The risk class assigned to it, as a vocabulary term or free string |
| `requires_ai_disclosure` | Whether it must disclose that it is an AI to the people it interacts with |
| `intended_deployment_contexts` | Sectors or settings it is built for. Distinct from `metadata.domain`, which is a discovery tag |
| `capabilities` | Capabilities it exercises, as vocabulary terms or free strings |
| `approved_environments` | Environments it has been cleared to run in |
| `foreseeable_misuse` | Uses the author considers out of bounds and reasonably foreseeable |
| `vocabularies` | Prefix-to-IRI map for CURIE values used in the block |
| `extensions` | Opaque annotations for external tooling, never interpreted by the spec |

`autonomy_level` is a **closed enum** — a value outside these four fails schema validation:

| Value | Meaning |
|-------|---------|
| `suggests` | Produces output; a human performs any action |
| `acts_with_approval` | Acts, but each consequential action is approved first |
| `acts_with_oversight` | Acts on its own; a human monitors and can intervene or reverse |
| `acts_autonomously` | Acts without a human in the loop |

Everything else is an open string, because owners, risk frameworks and sector names are organization-specific.

### `requires_ai_disclosure` distinguishes silence from "no"

Most fields cannot tell "declared as empty" from "not declared", and it rarely matters. Disclosure is the exception, so it is carried as a nullable boolean:

- **Absent** — the pack says nothing. An agent inherits whatever the pack declared.
- **`false`** — someone decided disclosure is not required here.

An agent under a pack that requires disclosure can therefore state `requires_ai_disclosure: false` and have it stick. That is a deliberate exemption someone wrote down, and it reads differently in a review from an agent that simply never mentioned it.

---

## Tool action scope

Tools carry a related declaration, describing what calling them can affect:

```yaml
tools:
  issue_refund:
    name: issue_refund
    description: Issues a refund against an order.
    action_scope:
      effect: external
      reversibility: irreversible
      data_classes: [pii, financial]
```

| Field | Values |
|-------|--------|
| `effect` | `read`, `write`, `external` |
| `reversibility` | `reversible`, `compensable`, `irreversible` |
| `data_classes` | Open list of data-class terms |
| `extensions` | Opaque annotations for external tooling |

Like governance, `action_scope` **describes consequence and gates nothing**. It does not stop the tool being called. It is there so a host can decide to require approval for irreversible external tools, or a reviewer can see at a glance which tools touch financial data.

If you want a hard stop on tool use, that is `tool_policy` and the hooks — a different mechanism with a different job.

---

## Reading it

From the SDK, scoped to the conversation:

```go
conv, err := sdk.Open("./pack", "refunds")
if err != nil {
    return err
}

if g := conv.Governance(); g != nil {
    fmt.Println(sdk.DescribeGovernance(g))
    // purpose: Triage inbound billing questions and draft replies.;
    // autonomy: suggests; owner: billing-platform-team;
    // must disclose as AI; environments: staging

    if g.RequiresAIDisclosure != nil && *g.RequiresAIDisclosure {
        showAIDisclosureBanner()
    }
}
```

Note the summary: `autonomy: suggests` and `environments: staging` come from the agent, everything else from the pack. That is the override rule in one line.

`Governance()` returns what applies to **this** conversation. Opened as a named agent, that is the agent's declaration resolved against the pack's; for a plain single-prompt conversation, which is not an agent, it is the pack-level declaration. `PackGovernance()` ignores agent scope and returns the pack's.

Both return `nil` when nothing is declared — not a zero-valued declaration, which would read as "declared everything as empty". Both return a copy, so adjusting what you read does not rewrite the loaded pack.

From the runtime, for any agent in a loaded pack:

```go
g, err := prompt.ResolveGovernance(pack, "refunds")
```

An unknown agent name is an **error**, not a fallback to the pack values. For most lookups a quiet fallback is a convenience; here it would be a lie — a caller that mistyped the name would be handed the pack's autonomy level and told the agent needs no approval, when nothing about that agent had been checked.

---

## What a host might do with it

PromptKit does not do any of this for you. These are the shapes the declaration is built to support:

- **Show it.** Render the AI disclosure the pack requires; put the accountable owner beside a transcript in an audit view.
- **Gate a deployment.** Refuse to promote a pack whose `approved_environments` does not include the environment you are deploying to. This is a check in your pipeline, not something the runtime enforces.
- **Route for approval.** Require human sign-off on tool calls whose `action_scope.reversibility` is `irreversible`, using the existing HITL approval flow.
- **Report.** Inventory every pack you run and what each one claims, as evidence for a compliance review.

The last one is often the immediate payoff: the facts already exist in the packs, so the inventory is a script rather than a survey.

---

## See also

- [Validation](/concepts/validation/) — what the schema enforces, and where guardrails take over
- [Tools & MCP](/concepts/tools-mcp/) — `tool_policy`, approvals and the mechanisms that do gate
- [A2A](/concepts/a2a/) — declaring agents and their members

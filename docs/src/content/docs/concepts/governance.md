---
title: Governance
description: Declaring who is accountable for an agent, how autonomously it acts, and what its tools can affect
sidebar:
  order: 10
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/packspec/types.go
    - runtime/prompt/governance.go
    - runtime/prompt/governance_validate.go
    - runtime/prompt/pack.go
    - runtime/prompt/schema/promptpack.schema.json
    - sdk/governance.go
    - sdk/internal/pack/load.go
    - sdk/internal/pack/validate.go
    - sdk/sdk.go
    - sdk/validate_pack.go
---

A pack can declare governance facts about the agent it defines: who is answerable for it, how far it acts without a human, what it was built for, and whether it must disclose itself as AI.

These are human-declared claims. PromptKit carries them, resolves them, and hands them to you. It does not act on them. The runtime does not refuse a request because a pack declared a low autonomy level, and it does not gate a tool because the tool is marked irreversible.

A declaration describes. The decisions belong to the host, which knows its own policy, deployment and obligations.

---

## Why declare it at all

Compliance regimes ask questions about a system rather than a model: who is accountable, what was this built for, does it act on its own, does it tell people it is AI. Those answers usually live in a spreadsheet maintained apart from the thing it describes, and they go stale when the agent changes.

Putting them in the pack makes them travel with the agent, version with it, and diff in review like any other change.

| Without | With |
|---------|------|
| Governance facts in a document, updated by hand | Declared beside the prompts they describe |
| Nothing tells you an agent's autonomy changed | A pack diff shows it |
| Each deployment re-derives what the agent is for | The pack states it once |

---

## Where governance goes

Governance lives at two levels. Pack-level, under `metadata`:

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

The pack says its agents act with approval. The `refunds` agent narrows itself to suggesting. It inherits everything it does not restate: the owner, the purpose, the disclosure requirement and the approved environments.

---

## The override rule

An agent's governance overrides the pack's per field:

- A field present on the agent replaces the pack value for that field.
- A field absent from the agent inherits the pack value.
- Arrays and `extensions` replace whole. They are not merged or appended.
- `vocabularies` is the exception: prefixes merge.

Replacement is the right rule for arrays:

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

The `refunds` agent is cleared for staging only. If arrays merged, narrowing an agent's approved environments would be impossible, because every attempt to restrict would re-grant everything the pack allowed. The same applies to `capabilities`, `foreseeable_misuse`, `intended_deployment_contexts`, `obligations`, `reviews` and `extensions`. `independent_of` is an object, but it also replaces whole: an agent that declares it gets its own axes and enforcement, never the pack's enforcement attached to the agent's axes.

`obligations` and `reviews` replace independently, so an agent that restates its obligations but not its reviews inherits the pack's reviews. Each review's `satisfies` must name an obligation in the agent's resolved governance. Restate `reviews` alongside `obligations` when the obligation ids change.

### `vocabularies` merges

`vocabularies` maps a prefix to an IRI so CURIE values like `acme:tier-3` resolve. It is the one container that accumulates rather than replaces:

```yaml
metadata:
  governance:
    vocabularies:
      acme: https://acme.example/ns#
    risk_classification: acme:tier-3     # a CURIE against the pack's prefix

agents:
  members:
    refunds:
      governance:
        vocabularies:
          other: https://other.example/ns#
```

The `refunds` agent inherits `risk_classification: acme:tier-3`. If its own `vocabularies` replaced the pack's, the `acme` prefix would leave scope and the inherited value would become unresolvable, so an agent would break a field it never touched. Prefixes accumulate, as in other CURIE systems.

An agent that redeclares a prefix wins on it, which is how you point a name at a different IRI:

```yaml
# pack:  acme -> https://acme.example/v1#
# agent: acme -> https://acme.example/v2#   → the agent's IRI applies
```

These prefixes are well-known and need no declaration: `dpv`, `eu-aiact`, `ai`, `pd`, `risk`, `tech`, `legal-eu-gdpr`, `legal-us`, `sector-health`, `sector-finance`, `sector-education`, `sector-law`, `sector-publicservices`, `sector-infra`, `hipaa` and `pp`.

---

## The fields

All fields are optional. Absence means undeclared, which is neither a default nor a denial.

| Field | Meaning |
|-------|---------|
| `intended_purpose` | What the agent is built to do, in the author's words |
| `autonomy_level` | How far it acts without a human; see below |
| `accountable_owner` | The role, team or function answerable for it. Prefer a durable identifier over a named person |
| `operator_role` | The declaring organization's role for this agent |
| `risk_classification` | The risk class assigned to it, as a vocabulary term or free string |
| `requires_ai_disclosure` | Whether it must disclose that it is an AI to the people it interacts with |
| `intended_deployment_contexts` | Sectors or settings it is built for. Distinct from `metadata.domain`, which is a discovery tag |
| `capabilities` | Capabilities it exercises, as vocabulary terms or free strings |
| `approved_environments` | Environments it has been cleared to run in |
| `foreseeable_misuse` | Uses the author considers out of bounds and reasonably foreseeable |
| `vocabularies` | Prefix-to-IRI map for CURIE values used in the block |
| `obligations` | Obligations the declaration triggers, each with the controls (`field`, `validator`, `eval` or `external`) that discharge it |
| `reviews` | Recurring reviews, each with an ISO 8601 `cadence`, an owning team, and the obligation ids it `satisfies` |
| `independent_of` | Axes (`model`, `provider`, `tools`, `prompts`, `accountable_owner`) on which whatever produces this agent's input must differ from it, with `strict` or `advisory` enforcement |
| `extensions` | Opaque annotations for external tooling, never interpreted by the spec |

`autonomy_level` is a closed enum. A value outside these four fails schema validation:

| Value | Meaning |
|-------|---------|
| `suggests` | Produces output; a human performs any action |
| `acts_with_approval` | Acts, but each consequential action is approved first |
| `acts_with_oversight` | Acts on its own; a human monitors and can intervene or reverse |
| `acts_autonomously` | Acts without a human in the loop |

Every other field is an open string, because owners, risk frameworks and sector names are organization-specific.

### `requires_ai_disclosure` distinguishes silence from "no"

Most fields cannot tell "declared as empty" from "not declared", and it rarely matters. Disclosure can, because it is a nullable boolean:

- Absent: the pack says nothing, and an agent inherits whatever the pack declared.
- `false`: someone decided disclosure is not required here.

An agent under a pack that requires disclosure can therefore state `requires_ai_disclosure: false` and have it stick. The exemption is written down, and it reads differently in a review from an agent that never mentioned disclosure.

### References must resolve

An obligation cannot be discharged by naming something that does not exist, so `sdk.Open` rejects a pack whose governance references dangle:

- `obligations[].id` and `reviews[].id` are unique within the governance object that declares them.
- A `validator` control names the `id` of a validator on one of the pack's prompts, and validator ids are unique across the pack.
- An `eval` control, and a review's `eval`, name an eval in the pack's `evals` or a prompt's `evals`.
- A `field` control names a governance property that is declared in the effective governance object.
- Each entry in a review's `satisfies` names an obligation in the effective governance object.
- `cadence` is a non-empty ISO 8601 duration. The schema checks this too, and a pack opened with schema validation skipped is still checked.

For an agent, the effective object is its declaration resolved against the pack's, so a control may rely on a field the agent inherits.

A term in `obligation`, `applies_to` or a review's `type` that uses a CURIE prefix neither declared in `vocabularies` nor well-known is a warning. `sdk.Open` logs it, and `sdk.ValidatePack` reports it as an issue with severity `warning`.

---

## Tool action scope

Tools carry a related declaration that describes what calling them can affect:

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

Like governance, `action_scope` describes consequence and gates nothing. It does not stop the tool being called. A host can use it to require approval for irreversible external tools, and a reviewer can see which tools touch financial data.

A hard stop on tool use is a different mechanism with a different job: `tool_policy` and the hooks.

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
    // must disclose as AI; environments: staging/production

    if g.RequiresAIDisclosure != nil && *g.RequiresAIDisclosure {
        showAIDisclosureBanner()
    }
}
```

In the summary, `autonomy: suggests` comes from the agent and everything else from the pack. That is the override rule applied.

`Governance()` returns what applies to this conversation. Opened as a named agent, that is the agent's declaration resolved against the pack's. For a plain single-prompt conversation, which is not an agent, it is the pack-level declaration. `PackGovernance()` ignores agent scope and returns the pack's.

Both return `nil` when nothing is declared, never a zero-valued declaration, which would read as "declared everything as empty". Both return a copy, so adjusting what you read does not rewrite the loaded pack.

From the runtime, for any agent in a loaded pack:

```go
g, err := prompt.ResolveGovernance(pack, "refunds")
```

An unknown agent name is an error rather than a fallback to the pack values. A caller that mistyped the name would otherwise receive the pack's autonomy level and conclude the agent needs no approval, when nothing about that agent had been checked.

---

## What a host might do with it

PromptKit does none of this for you. The declaration supports these uses:

- Show it. Render the AI disclosure the pack requires, or put the accountable owner beside a transcript in an audit view.
- Gate a deployment. Refuse to promote a pack whose `approved_environments` does not include the target environment. This is a check in your pipeline.
- Route for approval. Require human sign-off on tool calls whose `action_scope.reversibility` is `irreversible`, using the existing HITL approval flow.
- Report. Inventory every pack you run and what each one claims, as evidence for a compliance review. The facts already exist in the packs, so the inventory is a script.

---

## See also

- [Validation](/concepts/validation/): what the schema enforces, and where guardrails take over
- [Tools & MCP](/concepts/tools-mcp/): `tool_policy`, approvals and the mechanisms that do gate
- [A2A](/concepts/a2a/): declaring agents and their members

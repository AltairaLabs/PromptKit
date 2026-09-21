---
title: Upgrade Notes
description: Changes that need action when moving between PromptKit versions.
---

Release notes are generated from merged pull requests. This page carries the
changes that need you to do something, with what to change and why.

## Unreleased

### Checks name the provider they need, and the host binds it

Checks that need a model they do not own — a judge for `toxicity`, a classifier
for `text_sentiment` — now name it by a **logical key the pack declares in
`requires`**. The host binds that name to a concrete provider and can rebind it
whenever it likes, without the pack changing.

```yaml
requires:
  providers:
    - key: grader          # a name this pack invented
      role: llm
      description: grades the toxicity guardrail
      required: true

prompts:
  chat:
    validators:
      - type: toxicity
        params:
          provider: grader
```

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithProvider(agent),
    sdk.WithNamedProvider(sdk.ProviderSpec{
        ID: "grader", Type: "openai", Model: "gpt-4.1-mini",
    }),
)
```

**What breaks, and what to do:**

| If you | You will see | Change |
|---|---|---|
| Use `classifier_id` in a pack | the check errors, naming the replacement | rename it to `provider`, and declare that name in `requires` |
| Declare a judge-backed check (`toxicity`, `bias`, `pii_leakage`, `role_violation`, `llm_judge`, the RAG primitives) that names no provider | `Open()` fails | add `params.provider` naming a key from `requires`, or pass `sdk.WithJudgeProvider` as a host default |
| Bind an ancillary provider with `sdk.WithLLMProvider` | it silently became your agent | use `sdk.WithNamedProvider`, which registers it for a pack to name without touching the agent |
| Run `sdk.Evaluate` on a pack whose checks name providers | the checks error | pass `EvaluateOpts.ProviderBinding` (or `JudgeTargets`, which is treated as one) |
| Relied on `sdk.JudgeProviderKey` | it is gone | the runtime no longer names providers; the pack does |

**Why the break is worth it.** A judge-backed guardrail with no judge used to
build successfully and then block **every turn**, reporting a content violation
as the reason — or, for `pii_leakage`, run only its regex layer with the LLM
half silently absent. Both now fail at `Open()`, naming the check and whether
the fix belongs in the pack or in your wiring. A check that names a provider it
cannot get is an error rather than a skip, because a skip scores 1.0 and passes:
a safety control that never ran must not report clean.

### `tools_offered` now answers for the turn, not the conversation

`tools_offered` was documented as checking what **a turn** handed the provider
and in practice reported the union over the whole conversation, in two places
at once: the pipeline never reset its record between turns, and the eval
context extracted over the full history.

Both are fixed. The set a check sees is now the tools offered across the
current turn's rounds — a skill grant that widens the set mid-turn still
counts, a grant from three turns ago does not.

**What to expect:** a check that was passing on stale evidence can start
failing, and that failure is the correct answer:

| Check | Before | Now |
|---|---|---|
| `tool_names: [refund]` | passed if `refund` was offered in **any** earlier turn | passes only if this turn offered it |
| `tool_names: [refund], absent: true` | could never fail once `refund` had been offered once | fails when this turn offers it |

If an assertion flips to failing, read it as the grant not being active on that
turn rather than as a regression in the check.

### Voice (VAD) sessions now run guardrails

A pack's `validators:` did nothing in a VAD voice session. Guardrails are
provider hooks, and the VAD pipeline built its provider stage with a nil hook
registry, so every hook path early-returned: no `validation.started` /
`passed` / `failed` events, and no enforcement. The docs said guardrails
applied everywhere; for voice they did not.

They now run, exactly as they do in text.

**What to expect:** a voice conversation that was passing because nothing
checked it can start being blocked. That is the configured policy taking
effect, not a regression. Before upgrading, check what your pack's
`validators:` would do to the voice path — they have never been exercised
there.

This covers VAD mode (`OpenVoice` and the VAD topology). Native realtime and
duplex sessions are a separate stage with no hook support at all, tracked in
#1682; guardrails still do not run there.

### Provider adapters now rewrite your JSON Schema for the vendor

A schema you write for a pack went to the wire as written, and the three
providers enforce rules that contradict each other:

| | Anthropic | Gemini | OpenAI (strict) |
|---|---|---|---|
| `additionalProperties: false` on every object | required | **rejected** | required |
| every property in `required` | not required | n/a | required |
| `minimum` / `maxItems` / `uniqueItems` / … | **rejected** | accepted | accepted |

So no schema was portable, and a perfectly valid one failed on whichever
provider you had not tried. The adapters now rewrite it on the way out —
output schemas and tool schemas alike.

**What to expect:**

- A schema that used to be rejected now works. The failure was a 400 naming a
  JSON path, arriving at the first real invocation rather than at deploy.
- On Anthropic, constraints its grammar cannot carry (`minimum`, `maxItems`,
  `uniqueItems`, …) are removed from the wire and restated in the node's
  `description`, so the model still sees them. Tool arguments stay validated
  against the schema **you** wrote — the rewrite never touches your descriptor.
- `not` is the one keyword left in place: it has no equivalent in the accepted
  subset, so the request still fails, with an error naming the keyword.

### Claude tools use Anthropic's native strict tool use

Tool definitions now carry `strict: true`, so the API constrains decoding to
your schema and `tool_use.input` is guaranteed to validate rather than merely
likely to.

**What to expect:** better-formed tool arguments, and a schema adapted as
above, because strict mode enforces the same rules. If you need a schema sent
exactly as written, turn it off per provider:

```yaml
providers:
  - id: claude
    type: claude
    model: claude-sonnet-5
    additional_config:
      strict_tools: false
```

## v2.4.0

### Judge-backed guardrails need a judge

Superseded by the entry above, which changes how the judge is supplied. If you
adopted `sdk.JudgeProviderKey` from this release, move to naming the provider in
the pack.

### A tool call slower than 30s no longer kills its own turn

No action needed. The pipeline's idle timer is held open while tools run, so a
slow tool is bounded by its own `TimeoutMs` rather than by `IdleTimeout`. If you
raised `IdleTimeout` to work around this, you can put it back.

### A2A `message/send` keeps caller context values

No action needed. Values your HTTP middleware puts on the request context —
identity, tenant, request-scoped config — now reach the conversation on
`message/send` as they always did on `message/stream`. If you worked around this
by keying state on `contextID`, that workaround can go.

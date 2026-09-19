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

---
title: The Hook System
sidebar:
  order: 20
---

PromptKit exposes four families of hooks that let you observe, mutate, or veto work happening inside the runtime. They share a family resemblance but their semantics differ in important ways. This page explains the mental model so you can pick the right one and reason about what happens when something goes wrong.

## Why four hook types?

Different parts of the runtime have different contracts with the caller. A single "hook" abstraction would either be too weak (unable to gate a live LLM call) or too strong (forcing observational concerns to think about denial semantics). Four hook types, one per contract:

| Hook | Contract | Can deny? | Can mutate? | Fires per |
|---|---|---|---|---|
| **ProviderHook** | Request/response around an LLM call | Yes | Yes — the request before the call, the response after (via enforcement) | Each round, before and after the call |
| **ChunkInterceptor** | Each streaming chunk | Yes (abort stream) | Yes (enforcement) | Each chunk |
| **ToolHook** | Request/response around a tool call | Yes | Yes (enforcement) | Each tool call |
| **SessionHook** | Session lifecycle | No (error is logged and discarded) | No | Session start, each turn, session end |
| **EvalHook** | Eval result | No (observational) | Yes (direct result mutation) | Each eval result |

Pick the hook whose contract matches your intent. A PII redactor gates content → `ProviderHook`. An audit log observes turns → `SessionHook`. A metrics exporter watches eval scores → `EvalHook`. A kill-switch that aborts mid-stream → `ChunkInterceptor`.

## The two shapes of "mutate"

All hooks that can modify behavior fall into one of two shapes.

**Decision-based** (Provider, Chunk, Tool). The hook returns a `Decision` struct. To "mutate," the hook modifies the request/response in place **before** returning `hooks.Enforced(...)`. The alternative is `hooks.Deny(...)`, which produces a `HookDeniedError` and aborts the pipeline. Built-in guardrails always use `Enforced`; denial is reserved for hooks that want to be fatal.

`Enforced` means something different at each of the two `ProviderHook` seams:

- **`AfterCall`** — the hook has already rewritten `resp.Message` (truncated or replaced it). The runtime picks up the modified content as the round's response.
- **`BeforeCall`** — the provider call is **skipped entirely**: no request is built, no tokens are spent. Write the assistant text you want returned in its place to `req.Replacement` (`hooks.ProviderRequest.Replacement`; an exec hook returns it as `metadata.replacement`), or leave it empty to get the default blocked message. The runtime substitutes a canned assistant turn carrying `FinishReason: "safety"`, the decision metadata on `Message.Meta`, and a `ValidationResult` on `Message.Validations`.

**Enforcing stops the round loop, and pending tool calls are dropped.** This is the consequence most likely to surprise you. If an `AfterCall` hook enforces on a response that also requested tool calls, those calls are **not executed**, and they are stripped from the message before it lands in history — an assistant message carrying tool calls with no matching tool results is a protocol error on the next provider call. Earlier releases executed the tools and rolled on to another round.

What enforcement does **not** do is abort the pipeline. A hook belongs to one stage: the provider call is skipped and that `ProviderStage`'s round loop stops, but the message is emitted and every downstream stage — saving, TTS, recording, evals — still runs, exactly as it would for a real response. Only `Deny` aborts.

**Direct-mutation** (Eval). The hook is handed a pointer to the result and mutates it in place. There's no decision struct — the hook is observational by contract, but it's allowed to edit the observation before it propagates (redact explanations, enrich details, add tracing metadata). No pipeline gating happens either way.

Session hooks are unusual: they return a plain Go `error`, and on the SDK path that
error is **logged and discarded** (`sdk/session_hooks.go`) so a failing lifecycle hook
can never block a conversation. Use session hooks for pure observation, not for
gating or content modification — they cannot deny anything.

## Execution ordering

Hooks do not fire in a flat sequence. The nesting is **turn ⊃ provider ⊃ round ⊃ call**:

```
SessionHook.OnSessionStart                    once per conversation
pipeline.started                              event
  ┌─ per ProviderStage (a composition runs one per step) ──────────┐
  │  ┌─ ROUND 1..MaxRounds ──────────────────────────────────────┐ │
  │  │  ProviderHook.BeforeCall                                  │ │
  │  │  provider.call.started      event                         │ │
  │  │  → provider request → (1..N HTTP attempts on retry)       │ │
  │  │  ChunkInterceptor.OnChunk   per chunk (streaming only)    │ │
  │  │  provider.call.completed    event                         │ │
  │  │  ProviderHook.AfterCall                                   │ │
  │  │  ToolHook.BeforeExecution   per tool call                 │ │
  │  │  tool.call.started          event                         │ │
  │  │  ToolHook.AfterExecution    per tool call                 │ │
  │  │  ─── if the response requested tools, loop to BeforeCall ─┘ │
  └─────────────────────────────────────────────────────────────────┘
pipeline.completed                            event
SessionHook.OnSessionUpdate                   once per turn, AFTER it completes
EvalHook.OnEvalResult                         per eval result, from the eval runner
```

Four consequences that a flat list hides:

- **`BeforeCall`/`AfterCall` fire once per round, not once per turn.** A turn that
  uses tools runs several rounds, so a hook doing expensive or billable work must
  decide whether it wants per-round or per-turn semantics. Built-in input guardrails
  handle this by evaluating only when the last message is a user message — later
  rounds end in a tool result.
- **A turn can involve more than one provider.** Each composition step builds its own
  provider stage with its own round loop, and shares the same hook registry.
- **Retries happen below the hook boundary.** One round is exactly one
  `BeforeCall`/`AfterCall` pair even if the transport retried the HTTP request
  several times. Hooks never see retries.
- **On the streaming path, `OnChunk` fires before `provider.call.completed`.**
  Chunks are consumed as they arrive; the completion event carries the totals
  (tokens, cost, finish reason) that are only known once the stream closes. So a
  chunk interceptor that aborts a stream runs *before* any completion event for
  that call.

Within a single phase, multiple hooks run in **registration order**. For decision-based
hooks the first non-`Allow` short-circuits. For eval hooks every registered hook always
runs — a panic in one doesn't block the others.

## Error handling and safety

**Nil-safety.** A nil `*hooks.Registry` is a no-op. You can wire hooks optionally without special-casing "no hooks configured."

**Panic safety.** Eval hooks run inside a `recover()` scoped to each hook — one panic does not block the rest, and the eval result is still emitted. Provider, tool, and session hooks do **not** currently recover panics; a panicking hook crashes the request. Don't panic in a hook.

**Timeouts.** Exec-based hooks (subprocess-backed) have a configurable `timeout_ms`. If the subprocess exceeds it, the parent kills it and — in `filter` mode — treats the timeout as a denial. In `observe` mode and for eval hooks, the timeout just aborts the subprocess and the pipeline continues.

**Concurrency.** Hooks may run concurrently with one another across different conversations. Stateless hooks are always safe. Stateful hooks (e.g. a streaming buffer per response) must scope their state to a single conversation or synchronize explicitly.

## When to use an exec hook vs. a Go hook

A **Go hook** is the right choice when:
- You're shipping a library that wraps PromptKit and wants opinionated defaults.
- The hook logic is fast enough that process-spawn overhead would dominate.
- You need access to Go types (e.g. inspecting `*providers.StreamChunk` internals).

An **exec hook** is the right choice when:
- The hook is implemented in a non-Go language (Python ML models, Node log shippers).
- The hook is operated by a different team and should be upgraded independently of the runtime.
- You want per-deployment configurability via `RuntimeConfig` YAML without rebuilding.

Exec hooks are slower (process spawn per call) and less expressive (JSON round-trip), but they're more flexible operationally. Most teams start with Go hooks and graduate specific policies (PII, audit) to exec hooks when the operational boundary matters.

## When not to reach for a hook

Hooks are the right tool for **cross-cutting concerns** — observability, safety, policy — that apply uniformly across many calls. They are the wrong tool for:

- **Per-prompt behavior** — use the prompt itself, or a scenario variable.
- **Business logic** — put it in a tool, not a tool hook.
- **State that only one hook reads** — use a local struct field, not a hook.
- **Changing what the LLM sees in a specific turn** — use a pipeline stage (see [Pipeline Reference](/runtime/reference/pipeline/)), which is a cleaner extension point for content transformation.

If you find yourself writing "if this tool, then that hook," the logic probably belongs in the tool or the pipeline stage, not in a hook.

## Operational rules of thumb

These are not strict requirements; they reflect what tends to go wrong when hooks are written carelessly.

**Order hooks fast-to-slow.** Decision-based hooks short-circuit on the first `Deny`, so cheap checks should run first — they protect expensive ones from running on requests that were already going to be rejected.

```go
sdk.WithProviderHook(guardrails.NewLengthHook(1000, 250)),     // O(1)
sdk.WithProviderHook(guardrails.NewBannedWordsHook(banned)),    // O(n*w)
sdk.WithProviderHook(customExpensiveHook),                      // slow
```

**Prefer streaming guardrails when latency matters.** A `ChunkInterceptor` can abort a streaming response mid-flight, saving generation cost when the model produces something you'd reject anyway. The built-in `BannedWordsHook` and `LengthHook` already do this; `MaxSentencesHook` and `RequiredFieldsHook` need the full response and don't.

**Keep hooks stateless when you can.** Stateless hooks are trivially safe under concurrent use across conversations. Stateful hooks (e.g. a streaming buffer per response) must scope their state to one conversation or synchronise it explicitly. The runtime does not isolate hook state for you.

**Don't panic in a hook.** Eval hooks are wrapped in `recover()` per hook, but provider/tool/session hooks are not — a panic crashes the request. If your hook can fail, return `Deny` (or, for SessionHook, an error) instead.

## See also

- [Hooks Reference](/runtime/reference/hooks/) — interface signatures, registration, built-in guardrails
- [Custom Hooks How-To](/sdk/how-to/hooks/custom-hooks/) — implement each hook type in Go
- [Exec Hooks How-To](/sdk/how-to/hooks/exec-hooks/) — subprocess-backed hooks in any language
- [Exec Protocol Reference](/sdk/reference/exec-protocol/) — stdin/stdout wire format
- [RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/) — declarative hook configuration via YAML

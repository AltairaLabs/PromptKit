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
| **ProviderHook** | Request/response around an LLM call | Yes | Yes (via enforcement) | Each provider call |
| **ChunkInterceptor** | Each streaming chunk | Yes (abort stream) | Yes (enforcement) | Each chunk |
| **ToolHook** | Request/response around a tool call | Yes | Yes (enforcement) | Each tool call |
| **SessionHook** | Session lifecycle | Yes (via error return) | No | Session start, each turn, session end |
| **EvalHook** | Eval result | No (observational) | Yes (direct result mutation) | Each eval result |

Pick the hook whose contract matches your intent. A PII redactor gates content → `ProviderHook`. An audit log observes turns → `SessionHook`. A metrics exporter watches eval scores → `EvalHook`. A kill-switch that aborts mid-stream → `ChunkInterceptor`.

## The two shapes of "mutate"

All hooks that can modify behavior fall into one of two shapes.

**Decision-based** (Provider, Chunk, Tool). The hook returns a `Decision` struct. To "mutate," the hook modifies the request/response in place **before** returning `hooks.Enforced(...)`. The pipeline then continues with the modified content. The alternative is `hooks.Deny(...)`, which produces a `HookDeniedError` and aborts. Built-in guardrails always use `Enforced`; denial is reserved for hooks that want to be fatal.

**Direct-mutation** (Eval). The hook is handed a pointer to the result and mutates it in place. There's no decision struct — the hook is observational by contract, but it's allowed to edit the observation before it propagates (redact explanations, enrich details, add tracing metadata). No pipeline gating happens either way.

Session hooks are unusual: they return a plain Go `error`. Non-nil errors propagate to the caller but there's no enforcement/denial distinction. Use session hooks for pure observation or for "abort the session" errors, not for content modification.

## Execution ordering

Within a registered conversation, hooks execute in a fixed order relative to pipeline stages:

1. `SessionHook.OnSessionStart` — at session creation.
2. `ProviderHook.BeforeCall` — before each LLM call.
3. `ChunkInterceptor.OnChunk` — for each streaming chunk.
4. `ProviderHook.AfterCall` — after each LLM call.
5. `ToolHook.BeforeExecution` — before each tool call.
6. `ToolHook.AfterExecution` — after each tool call.
7. `SessionHook.OnSessionUpdate` — after each turn completes.
8. `SessionHook.OnSessionEnd` — at session teardown.
9. `EvalHook.OnEvalResult` — each time an eval produces a result (independent of the session loop — fires from the eval runner).

Within a single phase, multiple hooks run in **registration order**. For decision-based hooks, the first `Deny` short-circuits. For eval hooks, every registered hook always runs — a panic in one doesn't block the others.

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
- [Custom Hooks How-To](/sdk/how-to/custom-hooks/) — implement each hook type in Go
- [Exec Hooks How-To](/sdk/how-to/exec-hooks/) — subprocess-backed hooks in any language
- [Exec Protocol Reference](/sdk/reference/exec-protocol/) — stdin/stdout wire format
- [RuntimeConfig](/sdk/how-to/use-runtime-config/) — declarative hook configuration via YAML

# Pipeline Architecture

This document is the architectural contract for `runtime/pipeline/stage` and the consumers that build pipelines on top of it (`sdk/`, `tools/arena/`). It describes the layered model, what kinds of dependencies each layer is allowed to hold, and the rules for adding new ones.

PR reviewers should cite specific sections of this document when challenging design choices in stage / pipeline / service-injection PRs. New stages and services are reviewed against the principles below; deviations require updating this document first.

For an introduction to the package and how stages compose, see [`README.md`](README.md). This document is the architectural reference; the README is the operational guide.

---

## 1. Three layers

PromptKit pipelines have three distinct lifetimes. Mixing them produces leaks, redundancy, and the bug class issue [#1035](https://github.com/AltairaLabs/PromptKit/issues/1035) lives in.

| Layer | Lifetime | Realised today as |
|---|---|---|
| **Run** | A multi-turn execution: one `sdk.Conversation`, one Arena `Engine` run | `sdk/conversation.go::Conversation` struct; `tools/arena/engine/engine.go::Engine` struct |
| **Turn** | One pipeline execution: one `sdk.Conversation.Send`, one `arena.TurnExecutor.ExecuteTurn` | `sdk/internal/pipeline.Config` (built per `Send`); `tools/arena/turnexecutors.TurnRequest` |
| **Stage** | One `Stage.Process` invocation inside a Turn | Each individual Stage struct (e.g. `PromptAssemblyStage`, `TemplateStage`, `ProviderStage`) |

### 1.1 Run-level state

A Run holds conversation-spanning state. Initialising a Run wires the services below; closing the Run closes them. A Run is the natural boundary for cleanup, persistent connection lifecycle, and metrics aggregation.

A Run owns:

- **Conversation history.** Backed by an injected `statestore.Store`. Read by load stages, written by save stages, accessed by memory and retrieval stages.
- **Provider pool.** A `*providers.Registry` (`runtime/providers/registry.go`) keyed by `provider.ID()`. Holds every provider instance the run will use — agent, summarizer, judge, persona, embedding. One pool, one lifecycle, one `Close()`.
- **Tool registry.** A `*tools.Registry` populated by capabilities and the SDK / Arena directly. Tool descriptors and their executors live here for the duration of the Run.
- **MCP registry.** A `*mcp.Registry` for any MCP servers wired in.
- **Hook registry.** A `*hooks.Registry` holding the unified `ProviderHook` / `ToolHook` / `SessionHook` chains. See §3 for the authoring layers that converge here.
- **Memory services** (when configured). The `memory.Store`, `memory.Retriever`, and `memory.Extractor` injected via `sdk.WithMemory`. Memory is opt-in; Runs without it are valid.
- **Eval runner** (when configured). An `*evals.EvalRunner` that subscribes to lifecycle events and runs configured evals.
- **Metrics collector** (when configured). A `*metrics.Collector` for Prometheus emission.
- **Event bus.** An `*events.EventBus` — the dispatch infrastructure. Listeners (telemetry, metrics, recording, contract-test probes) subscribe to it.

### 1.2 Turn-level state

A Turn holds per-pipeline-execution state. Each `Send()` builds a fresh pipeline; the Turn-level state is built fresh too. This is what coordinates a single user-message-and-response cycle.

A Turn owns:

- **`TurnState`** (post-PR 3). The shared per-Turn coordinator: rendered system prompt, allowed tools, validators, variables, conversation/user identifiers. Populated by the pipeline builder or by producer stages once at the start of the Turn. Read (and where appropriate, mutated) by consumer stages by reference. **This is not in `StreamElement.Metadata`** — see §4.
- **Event emitter.** An `*events.Emitter` (`runtime/events/emitter.go`) — a thin facade over the Run's `*events.EventBus`, holding per-Turn context (`executionID`, `sessionID`, `conversationID`, `userID`) and exposing typed publish methods. The Bus dispatches; the Emitter ergonomically constructs and stamps. Same mechanism, different audience: stages publish through the Emitter, listeners subscribe to the Bus.
- **Resolved provider references.** The Turn-level pipeline build looks up which provider to use from the Run's `*providers.Registry` and injects it directly into the stages that need it. Stages do not navigate the Registry.

### 1.3 Stage-level concerns

A Stage is a per-element processor. It receives input and output channels via `Process(ctx, input, output)` and runs as a goroutine for the duration of the Turn. Its constructor takes the *narrow, specific* dependencies it needs — typically a subset of Turn-level services and the relevant `TurnState` reference.

A Stage:

- **Holds the dependencies it was given at construction.** It does not navigate a service graph. It does not look up services by name at runtime.
- **May read or write `TurnState`** if it received a reference. Mutations to `TurnState` happen in pipeline-stage order; that is the synchronization invariant. See §4.
- **Reads from the input channel, writes to the output channel, closes output on exit.** This is the core stage contract, unchanged.
- **Does not hold references to other stages.** Stages communicate via channels and `TurnState`, not direct calls.

---

## 2. Services vs configuration

Two categories of dependency that look superficially similar but obey different rules.

### 2.1 Services

A *service* has lifecycle, holds state, and is invoked at runtime. Examples:

- `statestore.Store` — `Load`/`Save`/`Fork` invoked across many Sends.
- `providers.Provider` — `Predict`/`PredictStream` invoked.
- `tools.Registry` — `Register`/`Get`/`Execute` invoked.
- `events.EventBus` — `Publish`/`Subscribe` invoked.
- `metrics.Collector` — counters/histograms invoked.

Services are constructed once at Run-time and torn down at `Close()`. They are the primary candidates for the registry pattern (`providers.Registry`, `tools.Registry`, `mcp.Registry`).

### 2.2 Configuration

Configuration is parameter data passed to a constructor. It has no lifecycle, holds no live state, and is not invoked at runtime. Examples:

- `RecordingStageConfig` — tells `RecordingStage` what to record (input vs output, sample rate, etc.). The stage is the active component; the config is its parameters.
- `ContextBuilderPolicy` — tells `ContextBuilderStage` its token budget and truncation strategy.
- `ProviderConfig` — tells `ProviderStage` max-tokens, temperature, response format.

Configuration belongs in the `Config` struct used to build the pipeline. It is **not** a Run-level concept and does not belong in any registry.

The distinction matters because services need uniform lifecycle handling (`Close` on shutdown) and benefit from pooling. Configuration does not.

---

## 3. Hook authoring layers

The hook system has one runtime mechanism (`hooks.Registry`) and three authoring sources. Documenting the convergence prevents the common confusion of treating "validator," "guardrail," and "hook" as separate runtime features.

| Authoring source | Where declared | Becomes |
|---|---|---|
| Pack-declared `ValidatorConfig` | YAML in a PromptPack file | `hooks.ProviderHook` via `guardrails.NewGuardrailHook` (see `sdk/sdk.go`) |
| Eval-handler-as-guardrail | Eval handler wrapped via `GuardrailHookAdapter` | `hooks.ProviderHook` (also implements `ChunkInterceptor` for streaming abort) |
| User-registered raw hook | `sdk.WithProviderHook` / `WithToolHook` / `WithSessionHook` | `hooks.ProviderHook` / `ToolHook` / `SessionHook` directly |

All three converge on `hooks.Registry`. The `ProviderStage` invokes the registry's `RunBeforeProviderCall` / `RunAfterProviderCall` chains; the tool dispatcher invokes `RunBeforeToolExecution` / `RunAfterToolExecution`. The runtime sees no distinction between authoring sources — everything is a hook by the time it runs.

The same eval handler can be a guardrail (enforced inline via the adapter) or a measurement (run post-hoc by `EvalRunner`). The difference is wrapping, not type.

---

## 4. Per-Turn data flow

This is the most consequential principle in the document, and the source of most past pipeline bugs.

### 4.1 The principle

> Per-Turn invariants live in `TurnState`. Per-element flags live on `StreamElement.Meta` (a typed `ElementMetadata` struct). `StreamElement.Metadata map[string]interface{}` is deprecated and slated for removal.

### 4.2 What's per-Turn vs per-element

A consumer audit (see [the proposal](../../../docs/superpowers/specs/2026-04-27-pipeline-architecture-proposal.md) for full table) showed that almost every key in `StreamElement.Metadata` is constant across all elements in a Send — they are *Turn-level invariants being unnecessarily copied per-element*:

- **Per-Turn:** `system_template`, `system_prompt`, `allowed_tools`, `validator_configs`, `variables`, `template_*`, `conversation_id`, `user_id`. Constant across every element of a Send.
- **Per-element:** `from_history` (genuinely differs — some elements *are* history, some aren't, in the same Send).

The historical `Metadata map[string]any` shape conflated these two scopes, which is why issue #1035 was structurally possible: `PromptAssemblyStage` looped through every element and wrote 8 per-Turn keys onto each, even though every value was identical. `TemplateStage` then re-rendered for each element. With 2000 history messages, this produced 16000 redundant map writes and 2001 redundant template renders per Send.

### 4.3 The post-migration model

Per-Turn data flows through `TurnState`:

- The pipeline builder (or a producer stage's first iteration via `sync.Once`) populates `TurnState` once per Send.
- Consumer stages hold a `*TurnState` reference passed in at construction and read directly: `state.SystemPrompt`, `state.AllowedTools`, etc.
- Mutations are allowed in pipeline-stage order: writers run before readers (channel hand-off provides happens-before). Arena's instruction stages append to `state.SystemPrompt` between `TemplateStage` and `ProviderStage`; that ordering is the synchronization invariant.

Per-element data flows on `StreamElement.Meta`:

- A typed `ElementMetadata` struct with named fields. Initial fields: `FromHistory` plus future per-element flags (guardrail markers, tool-call IDs, error tags).
- Adding a field requires editing the struct, which is reviewable.

### 4.4 Anti-patterns

- ❌ Copying per-Turn data onto every element. (#1035 in one line.)
- ❌ Stages reading per-Turn data from `elem.Metadata` after PR 3 lands. Read from `TurnState` instead.
- ❌ Stages writing per-Turn data to `elem.Metadata` after PR 3 lands. Write to `TurnState` instead.
- ❌ Wholesale `for k, v := range elem.Metadata` copies into persistent state. Read explicit typed fields. Arena's `ArenaStateStoreSaveStage` had this pattern; it's gone after PR 4.
- ❌ Type-asserting `elem.Metadata["key"].(SomeType)` in stage logic. Use the typed field on `Meta` or `TurnState`.

---

## 5. Adding new services or fields

Anything that extends the architecture at any layer requires updating this document in the same PR. Reviewers reject changes that introduce new layer concepts without doc updates.

### 5.1 Adding a new Run-level service

1. Justify why it belongs at Run level (long-lived, cross-Turn, has a `Close()`).
2. Update §1.1 with a one-line description and the type.
3. Wire it in `sdk.Open` and (if Arena exposes it) in `arena/engine`.
4. Document the cleanup path — usually `Conversation.Close()` calls into the registry or service directly.

### 5.2 Adding a new Turn-level service or `TurnState` field

1. Decide: is the new thing per-Turn-invariant (goes in `TurnState`), or a Turn-level service (separate type, injected at Turn build time)?
2. If `TurnState`: extend the struct, document the producer (which stage populates it) and consumer (which reads it).
3. If service: same as Run-level rules, but the service is constructed per-Turn from Run-level dependencies.
4. Update §1.2.

### 5.3 Adding a new `ElementMetadata` field

1. Confirm the data is genuinely per-element (differs between elements within the same Send). If not, it belongs in `TurnState`.
2. Add a typed field with a clear name.
3. Document which stage(s) write it and which read it.
4. Update §4.2.

### 5.4 Adding a new stage

1. Identify which Turn-level dependencies the stage needs. List them in the constructor.
2. Stages do not look up services dynamically — dependencies are injected at construction.
3. If the stage performs side effects on a service (Load, Save, Render, Emit, Execute), add a contract test in `sdk/integration/probes/` pinning the per-Send budget. See [the contract regime](../../../sdk/integration/probes/) for the pattern.
4. Add the stage to the pipeline builder in `sdk/internal/pipeline/builder.go` and (if Arena uses it) `tools/arena/engine/builder_integration.go`.

### 5.5 Adding a new authoring layer for hooks

1. Update §3 with the new authoring source and the conversion path.
2. The runtime mechanism (`hooks.Registry`) does not change.

---

## 6. Migration status

The principles above represent the target architecture. As of this document's date, parts are in flight:

| Section | Status | Tracking |
|---|---|---|
| §1.1 Run-level services | In place. SDK adoption of `providers.Registry` pending. | PR 2 (`feat/sdk-provider-pool`) |
| §1.2 `TurnState` | New type, not yet implemented. `StreamElement.Metadata map[string]any` is the current vehicle. | PR 3 (`feat/turnstate-runtime`) |
| §1.2 Event emitter / bus | In place. |  |
| §3 Hook authoring layers | In place. Documented here for the first time. |  |
| §4 Per-Turn data flow | Migration pending. `Metadata` bag deprecated through PR 3-5; deleted in PR 5. | PR 3, 4, 5 |
| §5 Add-new-X protocol | This document is the contract. New PRs cite specific sections. |  |

The migration sequence is documented in [`docs/superpowers/specs/2026-04-27-pipeline-architecture-proposal.md`](../../../docs/superpowers/specs/2026-04-27-pipeline-architecture-proposal.md). Until §4's migration is complete, both `TurnState` (when it exists) and the deprecated `Metadata` field are populated; consumer code reads from whichever is authoritative for the relevant key. After PR 5, only `TurnState` and `Meta` remain.

---

## 7. What this document does NOT govern

Out of scope, deferred to other proposals:

- **Memory extraction wiring.** Currently the `MemoryRetrievalStage` and `MemoryExtractionStage` are scaffolded but the data feed (`Metadata["messages"]`) was never populated by production code. PR 3 deletes the dead reference. A separate proposal will decide how memory extraction is fed (likely via a `TurnState` cursor or a `ConversationView` extension).
- **`ConversationView` coordinator.** A higher-level abstraction over `statestore.Store` that consolidates the read/write access patterns four pipeline stages currently make independently. Out of scope here; deferred to a follow-on proposal once `TurnState` is in place.
- **Eval runner positioning.** `EvalRunner` is a Run-level service today (§1.1). Its relationship to Turn-boundaries (when does it fire, how does it get judge providers from the pool, etc.) is documented in `runtime/evals/` and not duplicated here.
- **Self-play executors and Arena's TurnExecutor abstraction.** Arena builds pipelines per turn and orchestrates multiple turns; the layer above pipeline (the `TurnExecutor`) is documented in [`tools/arena/CLAUDE.md`](../../../tools/arena/CLAUDE.md). This document covers the pipeline layer.

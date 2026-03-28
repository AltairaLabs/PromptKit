# Runtime Module — Service Responsibilities

## Ownership

The runtime owns all core abstractions, interfaces, and execution logic. It is the single source of truth for how tools execute, how pipelines run, how providers are called, and how state machines transition.

## Responsibilities

### Tools (`tools/`)
- **Executor interface** — the universal contract for tool execution
- **Registry** — tool descriptor cache, executor dispatch by Mode, validation, rate limiting
- **Mode system** — built-in (mock, live, mcp, client, local) + custom mode routing
- **Descriptor schema** — JSON Schema validation for tool inputs/outputs
- **Async execution** — pending status for human-in-the-loop flows

### Pipeline (`pipeline/stage/`)
- **Stage interface** — Transform, Accumulate, Generate, Sink, Bidirectional
- **StreamPipeline** — DAG execution with channel-based element flow
- **ProviderStage** — LLM call orchestration, multi-round tool loop, compaction, cost budget
- **Context management** — ContextBuilderStage (truncation), CompactionStrategy (inter-round folding)
- **State persistence** — StateStoreLoad/Save stages, IncrementalSave, ContextAssembly
- **Idle timeout** — activity-based cancellation (resets on provider calls, tool execution, chunks)

### Workflow (`workflow/`)
- **State machine** — Spec, State, Context, StateMachine with thread-safe ProcessEvent
- **Terminal states** — explicit `Terminal` field + backward-compatible empty-OnEvent detection
- **Loop guards** — max_visits, on_max_visits redirect, VisitCounts tracking
- **Budgets** — max_total_visits, max_tool_calls, max_wall_time_sec from engine block
- **Artifacts** — ArtifactDef, SetArtifact/GetArtifact, append/replace modes, transition snapshots
- **TransitionResult** — structured return with redirect metadata
- **Tool descriptors** — `RegisterTransitionTool` builds the schema; handlers wired by consumers
- **Validation** — version, entry state, event targets, loop guard rules

### Providers (`providers/`)
- **Provider interface** — Predict, PredictStream, CalculateCost
- **Optional interfaces** — ToolSupport, MultimodalSupport, StreamInputSupport, ContextWindowProvider
- **Implementations** — Claude, OpenAI, Gemini, Ollama, vLLM, Voyage AI, Imagen, Mock, Replay

### Events (`events/`)
- **EventBus** — pluggable pub/sub (in-memory default, extensible to NATS/Kafka/Redis)
- **Emitter** — convenience wrapper stamping execution/session/conversation IDs
- **40+ event types** — pipeline, stage, provider, tool, validation, context, workflow, multimodal

### Hooks (`hooks/`)
- **ProviderHook** — BeforeCall/AfterCall interception of LLM requests
- **ChunkInterceptor** — streaming chunk interception (optional extension of ProviderHook)
- **ToolHook** — BeforeExecution/AfterExecution interception of tool calls
- **SessionHook** — session lifecycle (start, update, end)
- **Decision pattern** — Allow/Deny/Enforced with reason and metadata

### State Store (`statestore/`)
- **Store interface** — Load/Save/Fork for conversation state
- **Optional interfaces** — MessageReader (partial reads), MessageAppender (incremental writes), SummaryAccessor, MessageLog (write-through)
- **Implementations** — MemoryStore (in-memory), RedisStore

### Prompt (`prompt/`)
- **Registry** — load prompts by task type, variable substitution, model overrides
- **Repository pattern** — decouples prompt storage from registry logic
- **Fragment composition** — prompts composed from reusable fragments

### Types (`types/`)
- **Message** — role, content, parts, tool calls, tool results, cost info
- **ContentPart** — text, image, audio, video, document
- **CostInfo** — input/output/cached tokens, costs
- **ValidationResult** — pass/fail with details

## What Runtime Does NOT Own

- **Pack loading from files** — SDK's `internal/pack/` handles file I/O and JSON parsing
- **Capability inference** — SDK detects workflow/A2A/skills from pack structure
- **Deferred transitions** — SDK's workflow conversation defers LLM-initiated transitions
- **Scenario evaluation** — Arena's engine orchestrates multi-scenario concurrent execution
- **Tool handler wiring** — consumers wire handlers to tool descriptors (OnTool in SDK, RegisterExecutor in Arena)
- **Template variable injection** — SDK/Arena inject workflow context, artifacts into prompts

## Behavioral Contracts

### Tool Execution
- `Registry.Execute()` validates args, routes by Mode, enforces timeout, validates result
- Executors receive raw `json.RawMessage` args and return raw `json.RawMessage` results
- MultimodalExecutor extension returns content parts alongside JSON
- AsyncToolExecutor extension supports pending status for approval flows

### Pipeline Execution
- Stages run in goroutines, connected by channels
- Elements flow through: input → stages → output
- Context cancellation propagates to all stages
- Idle timeout resets on activity (provider call, tool execution, chunk)

### State Machine
- ProcessEvent is serialized (write lock)
- Budget checks run before event resolution
- Max_visits checks run after event resolution but before recording
- Redirect targets are NOT checked for their own max_visits (single-hop)
- Visit count increments for the actual destination (including redirects)
- Artifact snapshots captured at each transition (when artifacts exist)

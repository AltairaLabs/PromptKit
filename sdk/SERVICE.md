# SDK Module — Service Responsibilities

## Ownership

The SDK owns the developer-facing API surface: conversation lifecycle, options, pack loading, capability inference, and tool handler registration. It translates developer intent into runtime pipelines.

## Responsibilities

### Conversation Lifecycle
- **Open/OpenDuplex/OpenWorkflow** — create conversations from pack files
- **Resume** — restore conversations from state store
- **Send** — execute request/response turns (builds pipeline per call)
- **Stream** — streaming variant of Send
- **Close** — cleanup, session hooks, eval dispatch

### Options & Configuration
- **With* pattern** — ~40 options covering provider, state, context, hooks, capabilities, recording
- **Cross-option validation** — e.g., WithContextRetrieval requires WithContextWindow
- **Config → Pipeline translation** — options flow to `internal/pipeline.Config` → runtime stages

### Tool Handler Registration
- **OnTool / OnToolCtx** — local handlers (mode: "local") wrapped in `localExecutor`
- **OnClientTool** — deferred client-side handlers (mode: "client") wrapped in `clientExecutor`
- **OnToolAsync** — human-in-the-loop approval handlers
- **Handler snapshot at Send time** — consistency within a single pipeline execution
- **Client handler accessor** — live lookup for deferred tools registered mid-pipeline

### Capability System
- **Inference** — auto-detect workflow/A2A/skills from pack structure
- **Merging** — explicit capabilities override inferred ones (by name)
- **Lifecycle** — Init (receives CapabilityContext) → RegisterTools (on first pipeline build) → Close
- **WorkflowCapability** — delegates to `runtime/workflow.RegisterTransitionTool`
- **A2ACapability** — registers agent communication tools
- **SkillsCapability** — loads and registers skill tools

### Workflow Orchestration
- **WorkflowConversation** — manages per-state conversations over a StateMachine
- **Deferred transitions** — LLM-initiated transitions stored as pending, processed after Send completes
- **Context carry-forward** — summarize previous state's conversation, inject as template variable
- **State persistence** — persist workflow context to state store at transitions
- **Tool re-registration** — re-register transition tool for new state's available events

### Pipeline Construction
- **Stage ordering** — StateLoad → Variables → PromptAssembly → Template → ContextBuilder → Provider → StateSave
- **Compaction wiring** — auto-configure from provider's ContextWindowProvider or default 128K
- **Recording stages** — optional input/output capture for session replay
- **Mode-specific stages** — duplex (ASM), VAD (audio pipeline), text (standard)

## What SDK Does NOT Own

- **Tool execution logic** — runtime's `tools.Executor` and `Registry` own this
- **LLM call orchestration** — runtime's ProviderStage owns the tool loop
- **State machine transitions** — runtime's `workflow.StateMachine` owns ProcessEvent
- **Event emission** — runtime's `events.Emitter` owns event creation
- **Hook enforcement** — runtime's hooks package owns the Decision pattern

## Behavioral Contracts

### Send() Guarantees
- Pipeline is rebuilt from scratch on every Send()
- Tool handlers are snapshot at Send time (local) or live-lookup (client)
- Capabilities register tools only on first pipeline build
- Errors from pipeline execution are returned, not swallowed

### Workflow Transition Guarantees
- LLM-initiated transitions are deferred until after Send() returns the tool result
- Explicit Transition() calls execute immediately
- Old conversation is closed before new one opens
- Context summary is injected as `workflow_context` template variable
- Workflow context is persisted if state store is configured

### Thread Safety
- Conversation has RWMutex for closed flag and mode transitions
- Handler maps have separate mutexes for concurrent reads
- Each Send() gets independent handler snapshots
- WorkflowConversation serializes transitions

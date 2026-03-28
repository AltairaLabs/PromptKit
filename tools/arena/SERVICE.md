# Arena (PromptArena) Module — Service Responsibilities

## Ownership

Arena owns scenario-based testing and evaluation of LLM applications. It orchestrates multi-turn conversations against providers, runs assertions, and produces structured reports.

## Responsibilities

### Engine (`engine/`)
- **Composition root** — wires providers, tools, prompts, state store, workflow, evals
- **Concurrent scenario execution** — per-run isolation with cloned state machines and eval orchestrators
- **Mock provider mode** — transparent provider replacement for offline testing
- **Run planning** — scenario x provider x region matrix execution

### Workflow Integration
- **Detection** — explicit check of `config.Workflow` (no inference — Arena's config is explicit)
- **workflowTransitionExecutor** — `tools.Executor` that calls `sm.ProcessEvent()` immediately
- **Per-run state machines** — `RegisterRun/UnregisterRun` for concurrent isolation
- **Transition metadata** — redirect info, visit counts in run metadata
- **Tool re-registration** — updates available events after each state transition

### Conversation Execution
- **DefaultConversationExecutor** — iterates scripted turns, dispatches to turn executors
- **DuplexConversationExecutor** — streaming variant for ASM/VAD scenarios
- **PipelineExecutor** — builds runtime pipeline per turn with Arena-specific stages
- **State persistence** — always-on StateStore, writes per-turn (not deferred)

### Turn Execution Pipeline
Arena's pipeline has extra stages not present in SDK:
- **ScenarioContextExtractionStage** — extracts context references from scenario config
- **MockScenarioContextStage** — injects mock context for mock-provider scenarios
- **MediaConvertStage** — format conversion for provider capabilities
- **MediaExternalizerStage** — offload large media to file storage
- **ArenaAssertionStage** — run turn-level assertions inline
- **ArenaStateStoreSaveStage** — persist with assertion metadata

### Evaluation Orchestration
- **EvalOrchestrator** — dispatches assertions at turn, conversation, and session levels
- **Clone pattern** — per-run cloning for concurrent safety
- **Handler registry** — content_match, llm_judge, budget, latency, tool_usage, etc.
- **Metadata injection** — judge targets, prompt registry, workflow state

### Reporting
- **HTML** — interactive report with charts and filtering
- **JSON** — structured results for CI integration
- **JUnit** — CI-compatible test format
- **Markdown** — human-readable summary

## What Arena Does NOT Own

- **Tool execution dispatch** — runtime's `tools.Registry` owns this (Arena registers executors)
- **LLM call logic** — runtime's ProviderStage owns the tool loop
- **State machine logic** — runtime's `workflow.StateMachine` owns ProcessEvent
- **Provider implementations** — runtime's `providers/` package
- **Pipeline stage implementations** — runtime's `pipeline/stage/` (Arena adds its own stages)

## Behavioral Contracts

### Workflow Transitions
- **Immediate execution** — ProcessEvent called synchronously during tool execution
- **TaskType update** — scenario.TaskType updated to new state's prompt_task for next turn
- **Tool re-registration** — transition tool updated with new state's available events
- **Redirect metadata** — captured in per-run transition history

### Concurrent Execution
- Each run gets: own StateMachine, own EvalOrchestrator clone, own StateStore key
- Context carries runID for workflow executor routing
- No shared mutable state between concurrent runs

### State Persistence
- StateStore is always enabled (not optional like SDK)
- Messages saved per-turn (not deferred to conversation end)
- Assertion results attached to state as metadata
- Enables partial result recovery on failures

## Key Differences from SDK

| Concern | Arena | SDK |
|---------|-------|-----|
| Workflow transitions | Immediate (in executor) | Deferred (after Send) |
| Pipeline lifecycle | Per-turn | Per-Send (rebuilt each time) |
| Capability detection | Explicit config check | Pack structure inference |
| Tool registration | Direct RegisterExecutor | Via Capability.RegisterTools |
| State persistence | Always-on, per-turn | Optional, deferred |
| Concurrency model | Per-run isolation | Single conversation |
| Assertions | Inline (ArenaAssertionStage) | Post-execution (eval middleware) |
| Tool handler model | Custom Executor | OnTool → localExecutor wrapper |

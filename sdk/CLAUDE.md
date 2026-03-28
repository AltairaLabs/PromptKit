# SDK Module — Claude Code Instructions

## Role

The SDK is PromptKit's developer-facing API. It provides `Open()`, `Send()`, and options for building LLM applications from PromptPack files. The SDK composes runtime components into user-friendly abstractions.

## Key Invariant

**The SDK imports runtime but runtime never imports SDK.** The SDK translates developer intent (pack files, options, handlers) into runtime constructs (pipelines, executors, stages).

## Architecture Overview

```
sdk/
├── sdk.go                          # Open(), OpenDuplex(), OpenWorkflow() entry points
├── conversation.go                 # Conversation struct, Send(), pipeline building
├── workflow.go                     # WorkflowConversation, deferred transitions
├── workflow_transition_integration.go # transitionInternal(), context carry-forward
├── capability.go                   # Capability interface, inference, lifecycle
├── capability_workflow.go          # WorkflowCapability
├── capability_a2a.go               # A2ACapability
├── options.go                      # With* option functions
├── tool_executors.go               # localExecutor, clientExecutor (wraps OnTool → Executor)
├── internal/
│   ├── pipeline/builder.go         # Builds runtime pipeline from SDK config
│   └── pack/                       # SDK's own Pack types (mirrors runtime)
└── ...
```

## Critical Design Patterns

### 1. OnTool Wraps in Executor

SDK's `OnTool()` handlers are NOT a separate execution path. They're wrapped in a `localExecutor` implementing `tools.Executor` and registered in the runtime's `tools.Registry`:

```go
localExec := &localExecutor{handlers: handlersCopy}
c.toolRegistry.RegisterExecutor(localExec)  // Name() = "local"
```

The runtime's ProviderStage dispatches to it identically to any other executor. **Do not create parallel execution paths** — always go through `tools.Executor`.

### 2. Pipeline Built Per Send()

Every `Send()` rebuilds the entire pipeline. This enables:
- Dynamic tool registration between sends
- Capability registration on first build (guarded by `capabilitiesRegistered`)
- Handler snapshots at send time

### 3. Deferred Workflow Transitions

When the LLM calls `workflow__transition`, the runtime's `TransitionExecutor` defers the actual `ProcessEvent` call. The SDK commits it after `Send()` completes via `TransitionExecutor.Pending()` and `CommitPending()`. This ensures the LLM sees a consistent tool response before the state changes.

Both SDK and Arena now use the same deferred pattern — the runtime `TransitionExecutor` handles deferral, and each consumer commits at the appropriate point in its turn loop.

### 4. Capability Inference

```go
inferCapabilities(pack):
  pack.Workflow != nil  → WorkflowCapability
  pack.Agents != nil    → A2ACapability
  pack.Skills != nil    → SkillsCapability
```

Capabilities are auto-detected from pack structure. Explicit `WithCapability()` options override inference. This reduces boilerplate for common patterns.

### 5. Pack Types Mirror Runtime

`sdk/internal/pack/` defines its own `WorkflowSpec`, `WorkflowState`, etc. rather than importing `runtime/workflow`. This decouples the SDK's package graph from runtime evolution. Conversion happens via `packToRuntimePack()`.

## Adding New Functionality

- **New SDK option**: Add to `options.go` config struct + `With*` function. Wire through `conversation.go` → `internal/pipeline/builder.go`.
- **New capability**: Implement `Capability` interface. Add inference rule in `capability.go`. Register tools in `RegisterTools()`.
- **New tool handler type**: Add to `conversation.go` handler maps. Create executor in `tool_executors.go`. Register in `buildPipelineConfig()`.
- **New workflow tool**: Descriptor belongs in `runtime/workflow/`. Handler wiring belongs here in `registerWorkflowTools()`.

## Common Mistakes to Avoid

- **Don't duplicate runtime interfaces** — use `tools.Executor`, not a parallel dispatch mechanism
- **Don't import `sdk/` from `runtime/`** — if both need it, it belongs in runtime
- **Don't cache pipelines across Send() calls** — the per-Send rebuild is intentional
- **Don't execute workflow transitions immediately** — use the deferred pattern via `TransitionExecutor.Pending()`/`CommitPending()`

## Testing

```bash
go test ./sdk/... -count=1           # All SDK tests
go test ./sdk/integration/... -v     # Integration tests with mock providers
```

SDK tests use mock providers — no API keys needed. Workflow integration tests are in `sdk/workflow_test.go`.

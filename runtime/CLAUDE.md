# Runtime Module — Claude Code Instructions

## Role

The runtime is PromptKit's core library. It defines all interfaces, executes LLM calls, manages tools, runs pipelines, and handles state. **Runtime never imports SDK or Arena** — it is the foundation that both depend on.

## Key Invariant

**Runtime has zero dependencies on `sdk/` or `tools/arena/`**. All extensibility is via interfaces that higher-level modules implement. If you need to add functionality that both SDK and Arena use, it belongs here.

## Architecture Overview

```
runtime/
├── tools/         # Tool registry, executors, descriptors
├── pipeline/stage/ # Pipeline DAG, stages, ProviderStage (LLM+tool loop)
├── workflow/      # State machine, transitions, artifacts, budgets
├── providers/     # LLM provider interfaces + implementations
├── events/        # EventBus, Emitter, 40+ event types
├── hooks/         # ProviderHook, ToolHook, SessionHook
├── statestore/    # Store interface, MessageLog, optional interfaces
├── prompt/        # Registry, pack loading, template engine
├── types/         # Shared types (Message, ContentPart, CostInfo)
├── tokenizer/     # Heuristic token counting
└── ...            # audio, stt, tts, mcp, variables, etc.
```

## Tool Execution Model

Everything flows through the `tools.Executor` interface:

```go
type Executor interface {
    Execute(ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error)
    Name() string
}
```

**Mode-based dispatch**: Each tool has a `Mode` field. The Registry looks up `executors[tool.Mode]` to find the right executor. Built-in modes: `mock`, `live` (HTTP), `mcp`, `client`, `local`. Custom modes work automatically — register an executor with `Name()` matching the mode string.

**SDK's `OnTool()` handlers are wrapped in a `localExecutor`** that implements `Executor`. Arena registers executors directly. Both paths converge at `Registry.Execute()`.

## Pipeline & ProviderStage

The ProviderStage runs the LLM-tool loop:

1. Call provider (Predict/PredictWithTools)
2. If response has tool calls → execute via Registry → append results → loop
3. Between rounds: compaction, cost budget check, idle timeout reset
4. Max 50 rounds (configurable via ToolPolicy.MaxRounds)

**Key config on ProviderConfig**: `Compactor CompactionStrategy`, `MessageLog`, `MessageLogConvID`

## Workflow State Machine

`runtime/workflow/` owns the state machine, transitions, visit counting, budgets, and artifacts. The `workflow__transition` tool descriptor is built here (`transition_tool.go`), but **handlers are wired by consumers** (SDK, Arena) because they have different execution semantics.

**ProcessEvent returns `(*TransitionResult, error)`** — redirects (max_visits) are successful transitions, not errors.

## Adding New Functionality

- **New tool executor**: Implement `Executor`, register via `Registry.RegisterExecutor()`
- **New event type**: Add to `events/types.go`, add emitter method, subscribe in consumers
- **New hook type**: Define interface in `hooks/`, add to Registry, call from appropriate stage
- **New pipeline stage**: Implement `Stage` interface, wire in `sdk/internal/pipeline/builder.go`
- **New provider capability**: Define optional interface (like `ContextWindowProvider`), type-assert in consumers
- **New statestore capability**: Define optional interface (like `MessageLog`), type-assert in pipeline stages

## Testing

```bash
go test ./runtime/... -v -race -count=1
```

Runtime tests are self-contained — no external services needed. Mock providers and in-memory stores are used throughout.

# Arena (PromptArena) Module — Claude Code Instructions

## Role

Arena is PromptKit's testing and evaluation framework. It runs scenarios against LLM providers, executes assertions, and produces reports. Arena uses runtime components directly — it does NOT go through the SDK.

## Key Invariant

**Arena imports runtime but never imports SDK.** Arena builds its own pipelines, manages its own tool registry, and handles workflow state machines independently. It uses the same `tools.Registry`, `tools.Executor`, and pipeline stages as the SDK, but wires them differently.

## Architecture Overview

```
tools/arena/
├── engine/                         # Core execution engine
│   ├── engine.go                   # Engine struct, composition root
│   ├── conversation_executor.go    # Turn-by-turn conversation execution
│   ├── workflow_tool_executor.go   # Immediate workflow transitions
│   ├── execution_workflow_integration.go # Workflow init + per-run setup
│   ├── eval_orchestrator.go        # Assertion/eval dispatch
│   └── builder_integration.go      # Tool, provider, prompt registry setup
├── cmd/promptarena/                # CLI entry point
├── assertions/                     # Built-in assertion handlers
├── turnexecutors/                  # Pipeline-based turn execution
├── reader/                         # Config file loading
├── render/                         # Report generation
├── results/                        # HTML, JSON, JUnit, Markdown output
├── tui/                            # Terminal UI
└── ...
```

## Critical Design Patterns

### 1. Immediate Workflow Transitions (vs SDK's Deferred)

Arena executes workflow transitions **synchronously during tool execution**:

```go
// In workflowTransitionExecutor.Execute():
tr, err := run.sm.ProcessEvent(event)  // immediate
run.scenario.TaskType = newState.PromptTask  // update for next turn
registerTransitionTool(registry, newState)    // re-register for new events
```

This differs from the SDK which defers transitions until after `Send()`. Arena can do this because:
- Arena controls the turn loop externally (scripted turns, not interactive)
- The next turn naturally uses the updated TaskType
- No need to send a tool result to the LLM before transitioning (Arena manages the conversation)

### 2. Per-Run State Machine Isolation

Each scenario execution gets its own `StateMachine` via `RegisterRun(runID, scenario)`:

```go
type workflowRunState struct {
    sm          *workflow.StateMachine  // independent per run
    scenario    *config.Scenario        // mutable TaskType
    transitions []map[string]any        // transition history
}
```

This enables concurrent scenario execution without shared state. Context propagates the run ID: `withWorkflowScenarioID(ctx, runID)`.

### 3. Direct Executor Registration

Arena registers executors directly on the `tools.Registry`, not through a Capability abstraction:

```go
// In initWorkflow():
transExec := newWorkflowTransitionExecutor(spec, registry)
registry.RegisterExecutor(transExec)  // Name() = "workflow-transition"
registerTransitionTool(registry, entryState)  // sets Mode = "workflow-transition"
```

The Mode on the tool descriptor routes to the executor via `Registry.getExecutorForTool()`.

### 4. Pipeline Per Turn (not Per Conversation)

Arena builds a fresh pipeline for each turn, not per conversation:

```
Turn 1: Build pipeline → execute → save results → assertions
Turn 2: Build pipeline → execute → save results → assertions
...
```

This allows per-turn assertion evaluation and state persistence between turns.

### 5. Eval Orchestrator Cloning

Each concurrent run gets a cloned `EvalOrchestrator` to prevent data races:

```go
runOrch := e.evalOrchestrator.Clone()  // independent metadata per run
```

The runner (immutable eval defs) is shared; metadata (workflow state, emitter) is per-run.

## Adding New Functionality

- **New tool executor**: Implement `tools.Executor`, register in `initWorkflow()` or `BuildEngineComponents()`
- **New assertion type**: Add handler in `assertions/`, register in handler registry
- **New output format**: Implement in `results/`, wire in render pipeline
- **New workflow tool**: Descriptor in `runtime/workflow/`. Executor in `engine/workflow_tool_executor.go`. Register in `initWorkflow()`.

## Common Mistakes to Avoid

- **Don't import SDK** — Arena and SDK are peers, both consuming runtime
- **Don't share state machines across runs** — always use `RegisterRun` for per-run isolation
- **Don't forget to set `desc.Mode`** — Arena's local `registerTransitionTool` sets Mode to route to the custom executor. Without it, the registry can't find the executor.
- **Don't mutate shared EvalOrchestrator** — always Clone() for concurrent runs

## Testing

```bash
go test ./tools/arena/... -count=1    # All Arena tests

# Run examples with mock providers
cd examples/guardrails-test
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --mock-provider --ci --formats html,json
```

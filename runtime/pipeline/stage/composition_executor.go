package stage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/composition/engine"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// errToolFailed wraps a tool-reported failure (a non-empty ToolResult.Error) as a
// Go error so composition step execution stops.
var errToolFailed = fmt.Errorf("tool reported failure")

// CompositionExecutorDeps carries the runtime collaborators a composition step
// needs to execute. Injected once; reused for every step of every Execute call.
type CompositionExecutorDeps struct {
	PromptRegistry *prompt.Registry
	Provider       providers.Provider
	ToolRegistry   *tools.Registry
	Emitter        *events.Emitter
	HookRegistry   *hooks.Registry
	BaseVariables  map[string]string
	// SchemaResolver maps a step's output_schema path to JSON-schema bytes for
	// structured output. nil (or a nil return) means no ResponseFormat is set.
	// Plan 3 supplies the real resolver from the pack/config dir.
	SchemaResolver func(path string) (json.RawMessage, error)
}

// NewCompositionStepExecutor returns an engine.StepExecutor that runs prompt/agent
// steps as sub-pipelines and tool steps via the registry.
func NewCompositionStepExecutor(deps CompositionExecutorDeps) engine.StepExecutor {
	return func(ctx context.Context, step *composition.Step, input json.RawMessage) (json.RawMessage, error) {
		//nolint:exhaustive // KindBranch and KindParallel are handled by the engine; a leaf executor never receives them.
		switch step.Kind {
		case composition.KindTool:
			return deps.executeTool(ctx, step, input)
		case composition.KindPrompt, composition.KindAgent:
			return deps.execLLM(ctx, step, input)
		default:
			return nil, fmt.Errorf("composition executor: unsupported step kind %q", step.Kind)
		}
	}
}

// executeTool calls the tool registry with the engine-resolved args and surfaces
// any tool-reported error as a Go error.
func (deps CompositionExecutorDeps) executeTool(
	ctx context.Context, step *composition.Step, input json.RawMessage,
) (json.RawMessage, error) {
	if deps.ToolRegistry == nil {
		return nil, fmt.Errorf("tool %q: tool registry not configured", step.Tool)
	}
	res, err := deps.ToolRegistry.Execute(ctx, step.Tool, input)
	if err != nil {
		return nil, fmt.Errorf("tool %q: %w", step.Tool, err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("tool %q: %w: %s", step.Tool, errToolFailed, res.Error)
	}
	return res.Result, nil
}

// execLLM is implemented in Task 3.
func (deps CompositionExecutorDeps) execLLM(
	_ context.Context, step *composition.Step, _ json.RawMessage,
) (json.RawMessage, error) {
	return nil, fmt.Errorf("step %q: prompt/agent execution not yet implemented", step.ID)
}

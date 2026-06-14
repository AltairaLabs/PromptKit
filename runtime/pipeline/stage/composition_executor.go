package stage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/composition/engine"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

// execLLM runs a prompt/agent step as a minimal PromptAssembly → Template →
// Provider sub-pipeline. Agent steps pass their tools + termination bounds to the
// tool loop; prompt steps run a single round with no tools.
func (deps CompositionExecutorDeps) execLLM(
	ctx context.Context, step *composition.Step, input json.RawMessage,
) (json.RawMessage, error) {
	if deps.Provider == nil || deps.PromptRegistry == nil {
		return nil, fmt.Errorf("step %q: provider/prompt registry not configured", step.ID)
	}

	turnState := &TurnState{}
	promptStage := NewPromptAssemblyStageWithTurnState(deps.PromptRegistry, step.PromptTask, deps.BaseVariables, turnState)
	templateStage := NewTemplateStageWithTurnState(deps.Emitter, turnState)

	cfg := &ProviderConfig{Source: "agent"}
	rf, err := deps.responseFormat(step)
	if err != nil {
		return nil, fmt.Errorf("step %q: %w", step.ID, err)
	}
	cfg.ResponseFormat = rf

	policy := &pipeline.ToolPolicy{}
	if step.Kind == composition.KindAgent {
		turnState.AllowedTools = step.Tools
		if step.Termination != nil {
			if step.Termination.MaxSteps > 0 {
				policy.MaxRounds = step.Termination.MaxSteps
			}
			policy.StopOnTool = step.Termination.ToolCalled
		}
	} else {
		policy.MaxRounds = 1
	}

	provStage := NewProviderStageWithTurnState(
		deps.Provider, deps.ToolRegistry, policy, cfg, deps.Emitter, deps.HookRegistry, turnState,
	)

	pipe, err := NewPipelineBuilder().Chain(promptStage, templateStage, provStage).Build()
	if err != nil {
		return nil, fmt.Errorf("step %q: building sub-pipeline: %w", step.ID, err)
	}

	userMsg := types.Message{Role: roleUser, Content: stepInputToText(input)}
	res, err := pipe.ExecuteSync(ctx, NewMessageElement(&userMsg))
	if err != nil {
		return nil, fmt.Errorf("step %q: %w", step.ID, err)
	}
	if res == nil || res.Response == nil {
		return nil, fmt.Errorf("step %q: sub-pipeline produced no response", step.ID)
	}
	return responseToJSON(res.Response)
}

// responseFormat builds a structured-output ResponseFormat from the step's
// output_schema via the injected resolver. Returns nil when no schema is set or
// no resolver is configured (Plan 3 wires the real resolver).
func (deps CompositionExecutorDeps) responseFormat(step *composition.Step) (*providers.ResponseFormat, error) {
	if step.OutputSchema == "" || deps.SchemaResolver == nil {
		return nil, nil //nolint:nilnil // "no response format" is a valid, expected result
	}
	raw, err := deps.SchemaResolver(step.OutputSchema)
	if err != nil {
		return nil, fmt.Errorf("resolving output_schema %q: %w", step.OutputSchema, err)
	}
	if len(raw) == 0 {
		return nil, nil //nolint:nilnil // resolver declined; no response format
	}
	return &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: raw,
		SchemaName: step.ID,
		Strict:     true,
	}, nil
}

// stepInputToText turns the engine-resolved JSON input into the sub-pipeline's
// user-message text. A JSON string becomes its unquoted value; anything else is
// passed through as compact JSON. nil/`null` becomes empty.
func stepInputToText(input json.RawMessage) string {
	if len(input) == 0 || string(input) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(input, &s); err == nil {
		return s
	}
	return string(input)
}

// responseToJSON returns the sub-pipeline response as JSON: a Content that is
// already valid JSON passes through; plain text is JSON-encoded as a string.
func responseToJSON(resp *Response) (json.RawMessage, error) {
	if json.Valid([]byte(resp.Content)) {
		return json.RawMessage(resp.Content), nil
	}
	enc, err := json.Marshal(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("encoding response: %w", err)
	}
	return enc, nil
}

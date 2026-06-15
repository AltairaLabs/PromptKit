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

// compositionStepIDKey is the metadata key stamped into TurnState.ProviderRequestMetadata
// so that the mock provider (and any metadata-aware provider) can key per-step responses.
const compositionStepIDKey = "composition_step_id"

// toolScopeStage narrows TurnState.AllowedTools, after prompt assembly, to the
// intersection of the prompt template's allowed tools and the composition step's
// tools — matching how skill tools are scoped. A prompt step (no tools) therefore
// ends up with no tools; an agent step gets only tools allowed by BOTH its
// prompt_task and its own tools[] list.
type toolScopeStage struct {
	turnState *TurnState
	stepTools []string
}

func (s *toolScopeStage) Name() string    { return "composition-allowed-tools" } //nolint:revive
func (s *toolScopeStage) Type() StageType { return StageTypeTransform }          //nolint:revive

func (s *toolScopeStage) Process(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error { //nolint:revive,lll
	defer close(out)
	applied := false
	for elem := range in {
		if !applied {
			s.turnState.AllowedTools = intersectInOrder(s.turnState.AllowedTools, s.stepTools)
			applied = true
		}
		select {
		case out <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

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
	// BaseMetadata is merged into each sub-pipeline's ProviderRequestMetadata
	// alongside composition_step_id. Arena uses this to propagate mock_scenario_id
	// so per-step mock responses are keyed against the right scenario.
	BaseMetadata map[string]interface{}
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

	meta := map[string]interface{}{compositionStepIDKey: step.ID}
	for k, v := range deps.BaseMetadata {
		meta[k] = v
	}
	turnState := &TurnState{
		// Stamp the executing step id so the mock provider can key per-step
		// responses (Arena testability); flows to PredictionRequest.Metadata.
		// BaseMetadata (e.g. mock_scenario_id from Arena) is also merged here.
		ProviderRequestMetadata: meta,
	}
	promptStage := NewPromptAssemblyStageWithTurnState(deps.PromptRegistry, step.PromptTask, deps.BaseVariables, turnState)
	templateStage := NewTemplateStageWithTurnState(deps.Emitter, turnState)

	cfg := &ProviderConfig{Source: "agent"}
	rf, err := deps.responseFormat(step)
	if err != nil {
		return nil, fmt.Errorf("step %q: %w", step.ID, err)
	}
	cfg.ResponseFormat = rf

	// Per-step tool policy: agent steps configure MaxRounds/StopOnTool from
	// Termination; prompt steps are always single-round.
	// Tool scoping is handled uniformly by toolScopeStage: it intersects the
	// prompt template's AllowedTools with step.Tools, so a prompt step (empty
	// Tools) yields no tools regardless of what the template declares.
	policy := &pipeline.ToolPolicy{}
	if step.Kind == composition.KindAgent {
		if step.Termination != nil {
			if step.Termination.MaxSteps > 0 {
				policy.MaxRounds = step.Termination.MaxSteps
			}
			policy.StopOnTool = step.Termination.ToolCalled
		}
	} else {
		policy.MaxRounds = 1
	}

	// toolScopeStage runs after PromptAssemblyStage and narrows AllowedTools to
	// the intersection of the prompt template's tools and the step's tools.
	toolsOverride := &toolScopeStage{turnState: turnState, stepTools: step.Tools}

	provStage := NewProviderStageWithTurnState(
		deps.Provider, deps.ToolRegistry, policy, cfg, deps.Emitter, deps.HookRegistry, turnState,
	)

	pipe, err := NewPipelineBuilder().Chain(promptStage, templateStage, toolsOverride, provStage).Build()
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
	return responseToJSON(res.Response, step.OutputSchema != "")
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

// responseToJSON returns the sub-pipeline response as JSON.
//
// When structured is true (the step declared an output_schema), content that is
// already valid JSON is passed through as-is — the provider was instructed to
// return structured JSON and the raw payload must be preserved.
//
// When structured is false (a plain text step), the content is always
// JSON-encoded as a string, even if it happens to be valid JSON (e.g. the
// literal "null", "42", or "true"). This prevents ambiguity in downstream
// ${step.output} resolution where a bare JSON primitive would be
// indistinguishable from a structured value.
func responseToJSON(resp *Response, structured bool) (json.RawMessage, error) {
	if structured && json.Valid([]byte(resp.Content)) {
		return json.RawMessage(resp.Content), nil
	}
	enc, err := json.Marshal(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("encoding response: %w", err)
	}
	return enc, nil
}

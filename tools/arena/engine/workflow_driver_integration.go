package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	wf "github.com/AltairaLabs/PromptKit/tools/arena/workflow"
)

// workflowTransitionTool is the name of the tool the LLM calls to initiate transitions.
const workflowTransitionTool = "workflow__transition"

// transitionToolCallArgs is the JSON structure for workflow__transition tool arguments.
type transitionToolCallArgs struct {
	Event   string `json:"event"`
	Context string `json:"context"`
}

// transitionToolCallResult is the JSON structure for tool call results.
type transitionToolCallResult struct {
	NewState string `json:"new_state"`
	Event    string `json:"event"`
}

// arenaWorkflowDriver implements workflow.Driver, bridging the Arena mock
// provider with the workflow state machine. Each Send() call uses the
// current state's prompt as the system message and forwards the user
// message to the provider. If the LLM returns a workflow__transition
// tool call, the driver processes the transition internally.
type arenaWorkflowDriver struct {
	pack            *prompt.Pack
	sm              *workflow.StateMachine
	provider        providers.Provider
	messages        []types.Message       // current-state conversation history (cleared on transition)
	messageTrace    []types.Message       // complete message log across all states (never cleared)
	scenarioID      string                // for mock provider lookup
	turnNumber      int                   // 1-indexed, incremented per Send() call (never reset)
	lastTransitions []wf.TransitionRecord // transitions from the most recent Send()
	workflowContext string                // context from the last transition tool call
}

// Verify interface compliance at compile time.
var _ wf.Driver = (*arenaWorkflowDriver)(nil)

// Send sends a user message using the current state's prompt and returns the assistant response.
// If the LLM calls the workflow__transition tool, the transition is processed and
// the result is available via Transitions().
func (d *arenaWorkflowDriver) Send(ctx context.Context, message string) (string, error) {
	// Reset transitions for this call
	d.lastTransitions = nil

	// Get the system prompt for the current state
	system, err := d.currentSystemPrompt()
	if err != nil {
		return "", err
	}

	// Append user message
	userMsg := types.Message{Role: "user", Content: message}
	d.messages = append(d.messages, userMsg)
	d.messageTrace = append(d.messageTrace, userMsg)

	// Increment turn number (scenario-global, never reset)
	d.turnNumber++

	// Build request
	req := providers.PredictionRequest{
		System:   system,
		Messages: d.messages,
		Metadata: map[string]any{
			"mock_scenario_id": d.scenarioID,
			"mock_turn_number": d.turnNumber,
		},
	}

	// If current state is terminal (no events), use plain Predict
	state := d.pack.Workflow.States[d.sm.CurrentState()]
	if state == nil || len(state.OnEvent) == 0 {
		return d.predictNoTools(ctx, req)
	}

	// Type-assert to ToolSupport for PredictWithTools
	ts, ok := d.provider.(providers.ToolSupport)
	if !ok {
		// Provider doesn't support tools — fall back to plain Predict
		return d.predictNoTools(ctx, req)
	}

	// Build the transition tool and call PredictWithTools
	toolDesc := d.buildTransitionTool(state)
	tools, err := ts.BuildTooling([]*providers.ToolDescriptor{toolDesc})
	if err != nil {
		return "", fmt.Errorf("BuildTooling failed: %w", err)
	}

	resp, toolCalls, err := ts.PredictWithTools(ctx, req, tools, "auto")
	if err != nil {
		return "", fmt.Errorf("PredictWithTools failed in state %q: %w", d.sm.CurrentState(), err)
	}

	// Build assistant message for trace
	assistantMsg := types.Message{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: toolCalls,
	}
	d.messages = append(d.messages, assistantMsg)
	d.messageTrace = append(d.messageTrace, assistantMsg)

	// Process any workflow__transition tool calls
	for _, tc := range toolCalls {
		if tc.Name != workflowTransitionTool {
			continue
		}

		if err := d.processTransitionToolCall(tc); err != nil {
			return resp.Content, err
		}
	}

	return resp.Content, nil
}

// processTransitionToolCall handles a single workflow__transition tool call.
func (d *arenaWorkflowDriver) processTransitionToolCall(tc types.MessageToolCall) error {
	var args transitionToolCallArgs
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		return fmt.Errorf("failed to parse %s args: %w", workflowTransitionTool, err)
	}

	fromState := d.sm.CurrentState()

	// Process the event on the state machine
	if err := d.sm.ProcessEvent(args.Event); err != nil {
		return fmt.Errorf("transition event %q failed: %w", args.Event, err)
	}

	toState := d.sm.CurrentState()

	// Record the transition
	d.lastTransitions = append(d.lastTransitions, wf.TransitionRecord{
		From:    fromState,
		To:      toState,
		Event:   args.Event,
		Context: args.Context,
	})

	// Store workflow context for the new state
	d.workflowContext = args.Context

	// Build tool result message
	resultJSON, _ := json.Marshal(transitionToolCallResult{
		NewState: toState,
		Event:    args.Event,
	})
	toolResultMsg := types.NewToolResultMessage(types.MessageToolResult{
		ID:      tc.ID,
		Name:    workflowTransitionTool,
		Content: string(resultJSON),
	})
	d.messageTrace = append(d.messageTrace, toolResultMsg)

	// Append new state's system prompt to trace
	if sp := d.SystemPromptForState(toState); sp != "" {
		substituted := d.substituteWorkflowContext(sp)
		d.messageTrace = append(d.messageTrace, types.Message{
			Role:    "system",
			Content: substituted,
		})
	}

	// Clear current-state messages (new state starts fresh)
	d.messages = nil

	return nil
}

// predictNoTools calls plain Predict (no tool support).
//
//nolint:gocritic // hugeParam: req value is needed for interface compatibility
func (d *arenaWorkflowDriver) predictNoTools(ctx context.Context, req providers.PredictionRequest) (string, error) {
	resp, err := d.provider.Predict(ctx, req)
	if err != nil {
		return "", fmt.Errorf("provider predict failed in state %q: %w", d.sm.CurrentState(), err)
	}

	assistantMsg := types.Message{Role: "assistant", Content: resp.Content}
	d.messages = append(d.messages, assistantMsg)
	d.messageTrace = append(d.messageTrace, assistantMsg)

	return resp.Content, nil
}

// currentSystemPrompt returns the system prompt for the current state,
// with {{workflow_context}} substituted.
func (d *arenaWorkflowDriver) currentSystemPrompt() (string, error) {
	promptTask := d.sm.CurrentPromptTask()
	pp, ok := d.pack.Prompts[promptTask]
	if !ok {
		return "", fmt.Errorf("prompt %q not found in pack for state %q", promptTask, d.sm.CurrentState())
	}
	return d.substituteWorkflowContext(pp.SystemTemplate), nil
}

// substituteWorkflowContext replaces {{workflow_context}} in a template.
func (d *arenaWorkflowDriver) substituteWorkflowContext(template string) string {
	return strings.ReplaceAll(template, "{{workflow_context}}", d.workflowContext)
}

// buildTransitionTool creates a ToolDescriptor for the workflow__transition tool.
func (d *arenaWorkflowDriver) buildTransitionTool(state *workflow.State) *providers.ToolDescriptor {
	// Collect available events as enum values
	events := make([]string, 0, len(state.OnEvent))
	for event := range state.OnEvent {
		events = append(events, event)
	}
	sort.Strings(events)

	// Build JSON schema for the tool
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"event": map[string]interface{}{
				"type":        "string",
				"enum":        events,
				"description": "The workflow event to trigger.",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"description": "Carry-forward context summarizing relevant information for the next state.",
			},
		},
		"required": []string{"event", "context"},
	}
	schemaJSON, _ := json.Marshal(schema)

	return &providers.ToolDescriptor{
		Name: workflowTransitionTool,
		Description: "Transition the workflow to a new state by triggering an event. " +
			"Use this when the conversation requires escalation, resolution, or any state change.",
		InputSchema: schemaJSON,
	}
}

// Transitions returns the transitions from the most recent Send() call.
func (d *arenaWorkflowDriver) Transitions() []wf.TransitionRecord {
	return d.lastTransitions
}

// CurrentState returns the current workflow state name.
func (d *arenaWorkflowDriver) CurrentState() string {
	return d.sm.CurrentState()
}

// IsComplete returns true if the workflow reached a terminal state (no outgoing events).
func (d *arenaWorkflowDriver) IsComplete() bool {
	return d.sm.IsTerminal()
}

// Close releases resources.
func (d *arenaWorkflowDriver) Close() error {
	return nil
}

// MessageTrace returns the complete message trace across all states.
func (d *arenaWorkflowDriver) MessageTrace() []types.Message {
	return d.messageTrace
}

// InitialSystemPrompt returns the system prompt for the workflow's entry state.
func (d *arenaWorkflowDriver) InitialSystemPrompt() string {
	return d.SystemPromptForState(d.pack.Workflow.Entry)
}

// SystemPromptForState returns the system prompt for the given workflow state.
func (d *arenaWorkflowDriver) SystemPromptForState(stateName string) string {
	state, ok := d.pack.Workflow.States[stateName]
	if !ok {
		return ""
	}
	pp, ok := d.pack.Prompts[state.PromptTask]
	if !ok {
		return ""
	}
	return pp.SystemTemplate
}

// newArenaDriverFactory creates a workflow.DriverFactory that uses an Arena
// provider to generate responses. The factory loads a pack file and creates
// a state machine for each scenario execution.
//
// The returned callback retrieves the driver created by the factory, allowing
// callers to access driver-specific methods like InitialSystemPrompt().
func newArenaDriverFactory(
	provider providers.Provider, scenarioID string,
) (factory wf.DriverFactory, getDriver func() *arenaWorkflowDriver) {
	var lastDriver *arenaWorkflowDriver

	factory = func(packPath string, variables map[string]string, carryForward bool) (wf.Driver, error) {
		pack, err := prompt.LoadPack(packPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load pack %s: %w", packPath, err)
		}

		if pack.Workflow == nil {
			return nil, fmt.Errorf("pack %s has no workflow definition", packPath)
		}

		sm := workflow.NewStateMachine(pack.Workflow)

		lastDriver = &arenaWorkflowDriver{
			pack:       pack,
			sm:         sm,
			provider:   provider,
			scenarioID: scenarioID,
		}
		return lastDriver, nil
	}

	getDriver = func() *arenaWorkflowDriver { return lastDriver }
	return factory, getDriver
}

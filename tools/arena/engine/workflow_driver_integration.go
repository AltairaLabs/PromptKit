package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/skills"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
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
	toolRegistry    *tools.Registry       // tool registry for executing non-workflow tool calls
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
		return d.predictNoTools(ctx, req, state)
	}

	// Type-assert to ToolSupport for PredictWithTools
	ts, ok := d.provider.(providers.ToolSupport)
	if !ok {
		// Provider doesn't support tools — fall back to plain Predict
		return d.predictNoTools(ctx, req, state)
	}

	// Build all tool descriptors: workflow transition + registered tools (skills, pack tools)
	toolDescs := d.buildAllToolDescriptors(state)
	providerTools, err := ts.BuildTooling(toolDescs)
	if err != nil {
		return "", fmt.Errorf("BuildTooling failed: %w", err)
	}

	start := time.Now()
	resp, toolCalls, err := ts.PredictWithTools(ctx, req, providerTools, "auto")
	if err != nil {
		return "", fmt.Errorf("PredictWithTools failed in state %q: %w", d.sm.CurrentState(), err)
	}
	latency := time.Since(start)

	// Collect tool info for observability
	toolNames := make([]string, len(toolDescs))
	toolDescriptions := make([]map[string]interface{}, len(toolDescs))
	for i, td := range toolDescs {
		toolNames[i] = td.Name
		desc := map[string]interface{}{
			"name":        td.Name,
			"description": td.Description,
		}
		if len(td.InputSchema) > 0 {
			desc["input_schema"] = td.InputSchema
		}
		toolDescriptions[i] = desc
	}

	// Build workflow state info
	currentState := d.sm.CurrentState()
	workflowState := map[string]interface{}{
		"current_state": currentState,
	}
	if state.Description != "" {
		workflowState["description"] = state.Description
	}
	if len(state.OnEvent) > 0 {
		events := make(map[string]string, len(state.OnEvent))
		for ev, target := range state.OnEvent {
			events[ev] = target
		}
		workflowState["available_events"] = events
	}
	if state.PromptTask != "" {
		workflowState["prompt_task"] = state.PromptTask
	}

	// Build assistant message for trace
	assistantMsg := types.Message{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: toolCalls,
		Timestamp: time.Now(),
		LatencyMs: latency.Milliseconds(),
		CostInfo:  resp.CostInfo,
		Meta: map[string]interface{}{
			"_available_tools":  toolNames,
			"_tool_descriptors": toolDescriptions,
			"_workflow_state":   workflowState,
		},
	}
	d.messages = append(d.messages, assistantMsg)
	d.messageTrace = append(d.messageTrace, assistantMsg)

	// Process tool calls: execute non-workflow tools via registry, handle workflow transitions
	for _, tc := range toolCalls {
		if tc.Name == workflowTransitionTool {
			if err := d.processTransitionToolCall(tc); err != nil {
				return resp.Content, err
			}
			continue
		}

		// Execute non-workflow tool calls (skill__activate, a2a__, etc.) via the tool registry
		if d.toolRegistry != nil {
			result, execErr := d.toolRegistry.Execute(tc.Name, tc.Args)
			var content string
			if execErr != nil {
				content = fmt.Sprintf(`{"error":%q}`, execErr.Error())
			} else if result.Error != "" {
				content = fmt.Sprintf(`{"error":%q}`, result.Error)
			} else {
				content = string(result.Result)
			}
			toolResultMsg := types.NewToolResultMessage(types.MessageToolResult{
				ID:      tc.ID,
				Name:    tc.Name,
				Content: content,
			})
			// Only append to trace, not to d.messages — tool results in d.messages
			// cause the mock provider to inflate the turn number (turn skew).
			d.messageTrace = append(d.messageTrace, toolResultMsg)
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

	// Append new state's system prompt to trace with full diagnostics
	if sp := d.SystemPromptForState(toState); sp != "" {
		substituted := d.substituteWorkflowContext(sp)
		newState := d.pack.Workflow.States[toState]

		sysMeta := map[string]interface{}{}

		// Workflow state info
		ws := map[string]interface{}{
			"current_state":  toState,
			"previous_state": fromState,
			"transition":     args.Event,
		}
		if newState != nil {
			if newState.Description != "" {
				ws["description"] = newState.Description
			}
			if newState.PromptTask != "" {
				ws["prompt_task"] = newState.PromptTask
			}
			if len(newState.OnEvent) > 0 {
				events := make(map[string]string, len(newState.OnEvent))
				for ev, target := range newState.OnEvent {
					events[ev] = target
				}
				ws["available_events"] = events
			}
			if len(newState.OnEvent) == 0 {
				ws["terminal"] = true
			}
		}
		sysMeta["_workflow_state"] = ws

		// Available tools in the new state
		if newState != nil {
			toolDescs := d.buildAllToolDescriptors(newState)
			toolNames := make([]string, len(toolDescs))
			toolDescriptions := make([]map[string]interface{}, len(toolDescs))
			for i, td := range toolDescs {
				toolNames[i] = td.Name
				desc := map[string]interface{}{
					"name":        td.Name,
					"description": td.Description,
				}
				if len(td.InputSchema) > 0 {
					desc["input_schema"] = td.InputSchema
				}
				toolDescriptions[i] = desc
			}
			sysMeta["_available_tools"] = toolNames
			sysMeta["_tool_descriptors"] = toolDescriptions
		}

		d.messageTrace = append(d.messageTrace, types.Message{
			Role:      "system",
			Content:   substituted,
			Timestamp: time.Now(),
			Meta:      sysMeta,
		})
	}

	// Clear current-state messages (new state starts fresh)
	d.messages = nil

	return nil
}

// predictNoTools calls plain Predict (no tool support).
//
//nolint:gocritic // hugeParam: req value is needed for interface compatibility
func (d *arenaWorkflowDriver) predictNoTools(
	ctx context.Context, req providers.PredictionRequest, state *workflow.State,
) (string, error) {
	start := time.Now()
	resp, err := d.provider.Predict(ctx, req)
	if err != nil {
		return "", fmt.Errorf("provider predict failed in state %q: %w", d.sm.CurrentState(), err)
	}
	latency := time.Since(start)

	// Build workflow state info for diagnostics
	meta := map[string]interface{}{}
	if state != nil {
		ws := map[string]interface{}{
			"current_state": d.sm.CurrentState(),
		}
		if state.Description != "" {
			ws["description"] = state.Description
		}
		if state.PromptTask != "" {
			ws["prompt_task"] = state.PromptTask
		}
		ws["terminal"] = true
		meta["_workflow_state"] = ws
	}

	assistantMsg := types.Message{
		Role:      "assistant",
		Content:   resp.Content,
		Timestamp: time.Now(),
		LatencyMs: latency.Milliseconds(),
		CostInfo:  resp.CostInfo,
		Meta:      meta,
	}
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
	events := workflow.SortedEvents(state.OnEvent)
	return workflow.BuildTransitionProviderDescriptor(events)
}

// buildAllToolDescriptors builds the complete set of tool descriptors to send to the LLM:
// the workflow__transition tool plus any registered tools (skill tools, pack tools).
func (d *arenaWorkflowDriver) buildAllToolDescriptors(state *workflow.State) []*providers.ToolDescriptor {
	descs := []*providers.ToolDescriptor{d.buildTransitionTool(state)}

	if d.toolRegistry == nil {
		return descs
	}

	for _, name := range d.toolRegistry.List() {
		td := d.toolRegistry.Get(name)
		if td == nil {
			continue
		}
		descs = append(descs, &providers.ToolDescriptor{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: td.InputSchema,
		})
	}

	return descs
}

// registerPackSkillTools discovers skills from a pack and registers them in the tool registry.
// The pack's skill paths are resolved relative to the pack file's directory.
func registerPackSkillTools(pack *prompt.Pack, packPath string, toolRegistry *tools.Registry) error {
	packDir := filepath.Dir(packPath)

	// Convert pack skill configs to runtime SkillSource, resolving relative paths
	sources := make([]skills.SkillSource, len(pack.Skills))
	for i, s := range pack.Skills {
		dir := s.EffectiveDir()
		if dir != "" && !filepath.IsAbs(dir) {
			dir = filepath.Join(packDir, dir)
		}
		sources[i] = skills.SkillSource{
			Dir:          dir,
			Name:         s.Name,
			Description:  s.Description,
			Instructions: s.Instructions,
			Preload:      s.Preload,
		}
	}

	reg := skills.NewRegistry()
	if err := reg.Discover(sources); err != nil {
		return fmt.Errorf("skills discovery: %w", err)
	}

	executor := skills.NewExecutor(skills.ExecutorConfig{Registry: reg})

	// Only register if not already registered
	index := executor.SkillIndex("")
	if toolRegistry.Get(skills.SkillActivateTool) == nil {
		_ = toolRegistry.Register(skills.BuildSkillActivateDescriptorWithIndex(index))
		_ = toolRegistry.Register(skills.BuildSkillDeactivateDescriptor())
		_ = toolRegistry.Register(skills.BuildSkillReadResourceDescriptor())
		toolRegistry.RegisterExecutor(skills.NewToolExecutor(executor))
	}

	// Preload skills marked with preload: true
	for _, sk := range reg.PreloadedSkills() {
		_, _, _ = executor.Activate(sk.Name)
	}

	logger.Info("Registered pack skill tools", "count", len(reg.List()))
	return nil
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

// InitialWorkflowState returns metadata about the entry state for diagnostics.
func (d *arenaWorkflowDriver) InitialWorkflowState() map[string]interface{} {
	entry := d.pack.Workflow.Entry
	state := d.pack.Workflow.States[entry]
	if state == nil {
		return nil
	}
	ws := map[string]interface{}{
		"current_state": entry,
	}
	if state.Description != "" {
		ws["description"] = state.Description
	}
	if state.PromptTask != "" {
		ws["prompt_task"] = state.PromptTask
	}
	if len(state.OnEvent) > 0 {
		events := make(map[string]string, len(state.OnEvent))
		for ev, target := range state.OnEvent {
			events[ev] = target
		}
		ws["available_events"] = events
	}
	// Include all workflow states for overview
	allStates := make(map[string]interface{})
	for name, s := range d.pack.Workflow.States {
		info := map[string]interface{}{
			"prompt_task": s.PromptTask,
		}
		if s.Description != "" {
			info["description"] = s.Description
		}
		if len(s.OnEvent) > 0 {
			info["events"] = s.OnEvent
		}
		if len(s.OnEvent) == 0 {
			info["terminal"] = true
		}
		allStates[name] = info
	}
	ws["all_states"] = allStates
	return ws
}

// AvailableToolNames returns the names of all tools registered in the driver's tool registry.
// This includes workflow transition tools and capability tools (skill__, a2a__, etc.).
// InitialToolDescriptors returns tool descriptor metadata for the entry state
// (name, description, input_schema) for use in the initial system prompt.
func (d *arenaWorkflowDriver) InitialToolDescriptors() []map[string]interface{} {
	entry := d.pack.Workflow.Entry
	state := d.pack.Workflow.States[entry]
	if state == nil {
		return nil
	}
	toolDescs := d.buildAllToolDescriptors(state)
	result := make([]map[string]interface{}, len(toolDescs))
	for i, td := range toolDescs {
		desc := map[string]interface{}{
			"name":        td.Name,
			"description": td.Description,
		}
		if len(td.InputSchema) > 0 {
			desc["input_schema"] = td.InputSchema
		}
		result[i] = desc
	}
	return result
}

func (d *arenaWorkflowDriver) AvailableToolNames() []string {
	if d.toolRegistry == nil {
		return nil
	}

	// Always include workflow__transition if the entry state has events
	var names []string
	if state := d.pack.Workflow.States[d.pack.Workflow.Entry]; state != nil && len(state.OnEvent) > 0 {
		names = append(names, workflowTransitionTool)
	}

	names = append(names, d.toolRegistry.List()...)
	return names
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
	provider providers.Provider, scenarioID string, toolRegistry *tools.Registry,
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

		// Discover and register skill tools from the pack if not already in the registry
		driverToolRegistry := toolRegistry
		if len(pack.Skills) > 0 {
			if driverToolRegistry == nil {
				driverToolRegistry = tools.NewRegistry()
			}
			if err := registerPackSkillTools(pack, packPath, driverToolRegistry); err != nil {
				return nil, fmt.Errorf("failed to register skill tools: %w", err)
			}
		}

		lastDriver = &arenaWorkflowDriver{
			pack:         pack,
			sm:           sm,
			provider:     provider,
			toolRegistry: driverToolRegistry,
			scenarioID:   scenarioID,
		}
		return lastDriver, nil
	}

	getDriver = func() *arenaWorkflowDriver { return lastDriver }
	return factory, getDriver
}

package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	wf "github.com/AltairaLabs/PromptKit/tools/arena/workflow"
)

// arenaWorkflowDriver implements workflow.Driver, bridging the Arena mock
// provider with the workflow state machine. Each Send() call uses the
// current state's prompt as the system message and forwards the user
// message to the provider.
type arenaWorkflowDriver struct {
	pack       *prompt.Pack
	sm         *workflow.StateMachine
	provider   providers.Provider
	messages   []types.Message // conversation history
	scenarioID string          // for mock provider lookup
	turnNumber int             // 1-indexed, incremented per input step
}

// Verify interface compliance at compile time.
var _ wf.Driver = (*arenaWorkflowDriver)(nil)

// Send sends a user message using the current state's prompt and returns the assistant response.
func (d *arenaWorkflowDriver) Send(ctx context.Context, message string) (string, error) {
	// Get the system prompt for the current state
	promptTask := d.sm.CurrentPromptTask()
	pp, ok := d.pack.Prompts[promptTask]
	if !ok {
		return "", fmt.Errorf("prompt %q not found in pack for state %q", promptTask, d.sm.CurrentState())
	}
	system := pp.SystemTemplate

	// Append user message to history
	d.messages = append(d.messages, types.Message{
		Role:    "user",
		Content: message,
	})

	// Track turn number (1-indexed, incremented per input step)
	d.turnNumber++

	// Call the provider with mock scenario metadata
	req := providers.PredictionRequest{
		System:   system,
		Messages: d.messages,
		Metadata: map[string]any{
			"mock_scenario_id": d.scenarioID,
			"mock_turn_number": d.turnNumber,
		},
	}
	resp, err := d.provider.Predict(ctx, req)
	if err != nil {
		return "", fmt.Errorf("provider predict failed in state %q: %w", d.sm.CurrentState(), err)
	}

	// Append assistant response to history
	d.messages = append(d.messages, types.Message{
		Role:    "assistant",
		Content: resp.Content,
	})

	return resp.Content, nil
}

// Transition triggers a state machine event and returns the new state name.
func (d *arenaWorkflowDriver) Transition(event string) (string, error) {
	if err := d.sm.ProcessEvent(event); err != nil {
		return d.sm.CurrentState(), err
	}
	return d.sm.CurrentState(), nil
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

// newArenaDriverFactory creates a workflow.DriverFactory that uses an Arena
// provider to generate responses. The factory loads a pack file and creates
// a state machine for each scenario execution.
func newArenaDriverFactory(provider providers.Provider, scenarioID string) wf.DriverFactory {
	return func(packPath string, variables map[string]string, carryForward bool) (wf.Driver, error) {
		pack, err := prompt.LoadPack(packPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load pack %s: %w", packPath, err)
		}

		if pack.Workflow == nil {
			return nil, fmt.Errorf("pack %s has no workflow definition", packPath)
		}

		sm := workflow.NewStateMachine(pack.Workflow)

		return &arenaWorkflowDriver{
			pack:       pack,
			sm:         sm,
			provider:   provider,
			scenarioID: scenarioID,
		}, nil
	}
}

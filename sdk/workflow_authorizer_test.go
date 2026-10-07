package sdk

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/runtime/v2/workflow"
)

// recordingPolicy refuses the events in refuse, with the given reason, and
// records every request it is asked about.
type recordingPolicy struct {
	mu     sync.Mutex
	refuse map[string]string
	seen   []workflow.TransitionRequest
}

func (p *recordingPolicy) AuthorizeTransition(_ context.Context, req workflow.TransitionRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, req)
	if reason, ok := p.refuse[req.Event]; ok {
		return errors.New(reason)
	}
	return nil
}

// eventScriptProvider fires events[i] through workflow__transition on round
// i+1 and answers plainly once the script runs out. Per round it records the
// system prompt and the last tool result the model was shown.
type eventScriptProvider struct {
	*mock.ToolProvider
	events      []string
	prompts     []string
	toolResults []string
}

func (p *eventScriptProvider) SupportsStreaming() bool { return false }

func (p *eventScriptProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	round := len(p.prompts)
	p.prompts = append(p.prompts, req.System)
	last := ""
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" {
		last = req.Messages[n-1].Content
		if last == "" && req.Messages[n-1].ToolResult != nil {
			last = req.Messages[n-1].ToolResult.GetTextContent()
		}
	}
	p.toolResults = append(p.toolResults, last)

	if round < len(p.events) {
		calls := []types.MessageToolCall{{
			ID: "call", Name: "workflow__transition",
			Args: []byte(`{"event":"` + p.events[round] + `","context":"c"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "answered"}, nil, nil
}

func openAuthorizedWorkflow(t *testing.T, policy *recordingPolicy, events ...string) (
	*WorkflowConversation, *eventScriptProvider,
) {
	t.Helper()
	provider := &eventScriptProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
		events:       events,
	}
	wc, err := OpenWorkflow(writeWorkflowTestPack(t, workflowPackJSON),
		WithProvider(provider), WithSkipSchemaValidation(), WithTransitionAuthorizer(policy))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	return wc, provider
}

// TestAuthorizerAllowsAndSeesTheRequestedTransition — intake has two events.
// The authorizer must be asked about the one the model fired, with both
// states' declarations, and the transition commits.
func TestAuthorizerAllowsAndSeesTheRequestedTransition(t *testing.T) {
	policy := &recordingPolicy{}
	wc, _ := openAuthorizedWorkflow(t, policy, "InfoComplete")

	_, err := wc.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.Equal(t, "processing", wc.CurrentState())
	require.Len(t, policy.seen, 1)
	req := policy.seen[0]
	require.Equal(t, "intake", req.From)
	require.Equal(t, "processing", req.To, "To must follow the fired event, not another of intake's events")
	require.Equal(t, "InfoComplete", req.Event)
	require.Equal(t, "gather_info", req.FromState.PromptTask)
	require.Equal(t, "process", req.ToState.PromptTask)

	req.FromState.OnEvent["InfoComplete"] = "elsewhere"
	require.Equal(t, "processing", wc.workflowSpec.States["intake"].OnEvent["InfoComplete"],
		"the authorizer's copy must not reach the loaded workflow")
}

// TestRefusedTransitionReachesTheModel runs a real turn: the model fires a
// transition the policy refuses. Nothing is recorded, the state stays, the
// model is told why, and its next round runs in the current state.
func TestRefusedTransitionReachesTheModel(t *testing.T) {
	policy := &recordingPolicy{refuse: map[string]string{"InfoComplete": "caller not verified"}}
	wc, provider := openAuthorizedWorkflow(t, policy, "InfoComplete")

	resp, err := wc.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.Equal(t, "intake", wc.CurrentState())
	require.Nil(t, wc.transExec.Pending(), "a refused transition must record nothing")
	require.Len(t, provider.prompts, 2)
	require.Equal(t, "You gather information.", provider.prompts[1],
		"the round after a refusal runs in the current state")
	require.Contains(t, provider.toolResults[1], "transition_refused")
	require.Contains(t, provider.toolResults[1], "caller not verified")
	require.Contains(t, resp.Text(), "answered")
}

// TestAgentChainStopsAtTheRefusedHop — processing holds the turn (control
// absent means agent), so one Send can hop twice. Each hop is authorized on
// its own; refusing the second leaves the workflow in the middle state.
func TestAgentChainStopsAtTheRefusedHop(t *testing.T) {
	policy := &recordingPolicy{refuse: map[string]string{"Done": "needs a human"}}
	wc, _ := openAuthorizedWorkflow(t, policy, "InfoComplete", "Done")

	_, err := wc.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.Equal(t, "processing", wc.CurrentState())
	require.Len(t, policy.seen, 2)
	require.Equal(t, "processing", policy.seen[1].From)
	require.Equal(t, "confirmation", policy.seen[1].To)
}

// TestHostTriggeredTransitionIsAuthorized — the same policy covers a
// transition the host fires; a refusal is returned and the state stays.
func TestHostTriggeredTransitionIsAuthorized(t *testing.T) {
	policy := &recordingPolicy{refuse: map[string]string{"InfoComplete": "out of hours"}}
	wc, _ := openAuthorizedWorkflow(t, policy)

	_, err := wc.Transition("InfoComplete")

	require.ErrorContains(t, err, "out of hours")
	require.Equal(t, "intake", wc.CurrentState())
	require.Equal(t, "InfoComplete", policy.seen[0].Event)
}

func TestNilTransitionAuthorizerIsRejected(t *testing.T) {
	_, err := OpenWorkflow(writeWorkflowTestPack(t, workflowPackJSON),
		WithProvider(mock.NewProvider("m", "m", false)), WithSkipSchemaValidation(),
		WithTransitionAuthorizer(nil))

	require.ErrorContains(t, err, "authorizer must not be nil")
}

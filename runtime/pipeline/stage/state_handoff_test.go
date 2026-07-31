package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// handoffProvider records the system prompt it is called with on each round and
// scripts a tool call on round 1 so the loop runs a second round.
type handoffProvider struct {
	*mock.ToolProvider
	seenPrompts []string
}

// SupportsStreaming false pins these tests to the unary tool loop
// (executeMultiRound). The streaming loop is covered separately — the two are
// parallel implementations, so both need the handoff wiring.
func (p *handoffProvider) SupportsStreaming() bool { return false }

// Predict records too, so a test failure distinguishes "wrong method routed"
// from "provider never called".
func (p *handoffProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.seenPrompts = append(p.seenPrompts, "PREDICT:"+req.System)
	return providers.PredictionResponse{Content: "plain"}, nil
}

func (p *handoffProvider) PredictWithTools(
	_ context.Context,
	req providers.PredictionRequest,
	_ providers.ProviderTools,
	_ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)

	// Round 1 transitions; every later round answers plainly so the loop ends.
	if len(p.seenPrompts) == 1 {
		calls := []types.MessageToolCall{{
			ID:   "call-1",
			Name: "workflow__transition",
			Args: []byte(`{"event":"Escalate","context":"caller verified"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "destination speaking"}, nil, nil
}

// fakeResolver reports a single handoff, then no further changes.
type fakeResolver struct {
	handoff Handoff
	err     error
	meta    map[string]any
	calls   int
}

func (f *fakeResolver) ResolvePendingHandoff(_ context.Context) (Handoff, error) {
	f.calls++
	if f.err != nil {
		return Handoff{}, f.err
	}
	if f.calls > 1 {
		return Handoff{Changed: false}, nil
	}
	return f.handoff, nil
}

func (f *fakeResolver) CurrentStateMeta() map[string]any { return f.meta }

func newHandoffStage(t *testing.T, resolver WorkflowStateResolver) (*ProviderStage, *handoffProvider) {
	t.Helper()

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "workflow__transition",
		Description: "Transition the workflow",
		InputSchema: []byte(`{"type":"object"}`),
	}))

	provider := &handoffProvider{ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil)}

	turnState := NewTurnState()
	turnState.SystemPrompt = "ORIGIN PROMPT"
	turnState.AllowedTools = []string{"workflow__transition"}

	stage := NewProviderStageWithTurnState(provider, registry, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, nil, turnState)
	stage.SetWorkflowStateResolver(resolver)

	return stage, provider
}

func runHandoffTurn(t *testing.T, stage *ProviderStage) {
	t.Helper()

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "someone charged my account"}
	input <- NewMessageElement(&userMsg)
	close(input)

	output := make(chan StreamElement, 32)
	require.NoError(t, stage.Process(context.Background(), input, output))
	for range output { //nolint:revive // draining
	}
}

// The core guarantee: after the transition tool result, the next provider round
// runs under the DESTINATION state's system prompt. Without the handoff the
// second round repeats the origin prompt, which is the bug this fixes — the
// origin agent gets the last word and the destination state never speaks.
func TestProviderStage_HandoffSwapsSystemPromptMidTurn(t *testing.T) {
	resolver := &fakeResolver{handoff: Handoff{
		Changed:      true,
		SystemPrompt: "DESTINATION PROMPT",
		AllowedTools: []string{"workflow__transition"},
	}}

	stage, provider := newHandoffStage(t, resolver)
	runHandoffTurn(t, stage)

	require.Len(t, provider.seenPrompts, 2,
		"transition tool call must produce a second round")
	require.Equal(t, "ORIGIN PROMPT", provider.seenPrompts[0])
	require.Equal(t, "DESTINATION PROMPT", provider.seenPrompts[1],
		"round after the transition must run as the destination state")
}

// A resolver reporting no change must leave the turn untouched — this is the
// path every non-workflow and non-transitioning turn takes.
func TestProviderStage_NoHandoffLeavesPromptUnchanged(t *testing.T) {
	resolver := &fakeResolver{handoff: Handoff{Changed: false, SystemPrompt: "SHOULD NOT APPLY"}}

	stage, provider := newHandoffStage(t, resolver)
	runHandoffTurn(t, stage)

	require.Len(t, provider.seenPrompts, 2)
	require.Equal(t, "ORIGIN PROMPT", provider.seenPrompts[0])
	require.Equal(t, "ORIGIN PROMPT", provider.seenPrompts[1],
		"Changed=false must not alter the system prompt")
}

// A nil resolver is the non-workflow case and must be inert.
func TestProviderStage_NilResolverIsInert(t *testing.T) {
	stage, provider := newHandoffStage(t, nil)
	runHandoffTurn(t, stage)

	require.Len(t, provider.seenPrompts, 2)
	require.Equal(t, "ORIGIN PROMPT", provider.seenPrompts[1])
}

// A resolver error must abort the turn rather than silently continuing under
// the wrong state's prompt.
func TestProviderStage_HandoffErrorAbortsTurn(t *testing.T) {
	resolver := &fakeResolver{err: context.DeadlineExceeded}

	stage, provider := newHandoffStage(t, resolver)

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "hello"}
	input <- NewMessageElement(&userMsg)
	close(input)

	output := make(chan StreamElement, 32)
	err := stage.Process(context.Background(), input, output)
	for range output { //nolint:revive // draining
	}

	require.Error(t, err)
	require.Contains(t, err.Error(), "workflow handoff")
	require.Len(t, provider.seenPrompts, 1,
		"no further round may run after a failed handoff")
}

// Each assistant message carries the state that produced it. A turn can span
// states, so per-turn attribution is not sufficient.
func TestProviderStage_StampsStatePerAssistantMessage(t *testing.T) {
	resolver := &fakeResolver{
		handoff: Handoff{Changed: true, SystemPrompt: "DESTINATION PROMPT"},
		meta:    map[string]any{"current_state": "handoff"},
	}
	stage, _ := newHandoffStage(t, resolver)

	response := types.Message{Role: "assistant", Content: "hi"}
	stage.stampWorkflowState(&response)

	require.Equal(t, map[string]any{"current_state": "handoff"},
		response.Meta[workflowStateMetaKey])
}

func TestProviderStage_StampIsNoOpWithoutResolverOrMeta(t *testing.T) {
	t.Run("nil resolver", func(t *testing.T) {
		stage, _ := newHandoffStage(t, nil)
		response := types.Message{Role: "assistant", Content: "hi"}
		stage.stampWorkflowState(&response)
		require.Nil(t, response.Meta)
	})

	t.Run("empty meta", func(t *testing.T) {
		stage, _ := newHandoffStage(t, &fakeResolver{})
		response := types.Message{Role: "assistant", Content: "hi"}
		stage.stampWorkflowState(&response)
		require.Nil(t, response.Meta)
	})
}

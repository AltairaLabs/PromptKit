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

	// Transition on the first round only; later rounds answer plainly so the
	// loop terminates.
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

// fakeResolver returns a scripted sequence of Handoffs, repeating the last.
// The resolver contract is "what should this turn be running", so a real
// implementation returns the ORIGIN state's prompt until the transition
// commits and the DESTINATION's afterwards — the sequence models that.
type fakeResolver struct {
	sequence []Handoff
	err      error
	meta     map[string]any
	calls    int
}

func (f *fakeResolver) ResolveCurrentState(_ context.Context) (Handoff, error) {
	if f.err != nil {
		return Handoff{}, f.err
	}
	f.calls++
	if len(f.sequence) == 0 {
		return Handoff{}, nil
	}
	idx := f.calls - 1
	if idx >= len(f.sequence) {
		idx = len(f.sequence) - 1
	}
	return f.sequence[idx], nil
}

func (f *fakeResolver) CurrentStateMeta() map[string]any { return f.meta }

// originThenDestination is the common script: the workflow is in the origin
// state when the turn starts, and in the destination state once the transition
// tool has run.
func originThenDestination() []Handoff {
	return []Handoff{
		{Valid: true, SystemPrompt: "ORIGIN PROMPT", AllowedTools: []string{"workflow__transition"}},
		{Valid: true, SystemPrompt: "DESTINATION PROMPT", AllowedTools: []string{"workflow__transition"}},
	}
}

func newHandoffStage(t *testing.T, resolver WorkflowStateResolver) (*ProviderStage, *handoffProvider, *TurnState) {
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

	return stage, provider, turnState
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
	resolver := &fakeResolver{sequence: originThenDestination()}

	stage, provider, _ := newHandoffStage(t, resolver)
	runHandoffTurn(t, stage)

	require.Len(t, provider.seenPrompts, 2,
		"transition tool call must produce a second round")
	require.Equal(t, "ORIGIN PROMPT", provider.seenPrompts[0])
	require.Equal(t, "DESTINATION PROMPT", provider.seenPrompts[1],
		"round after the transition must run as the destination state")
}

// A resumed execution re-runs PromptAssemblyStage, which resets the turn's
// prompt to the state the pipeline was BUILT for — even though the workflow has
// already moved on and nothing is "pending" any more. Only a comparison against
// the current state recovers it, which is why the resolver reports what should
// be running rather than what changed.
func TestProviderStage_HandoffSurvivesPipelineReExecution(t *testing.T) {
	resolver := &fakeResolver{sequence: originThenDestination()}

	stage, provider, turnState := newHandoffStage(t, resolver)

	runHandoffTurn(t, stage)
	require.Equal(t, "DESTINATION PROMPT", provider.seenPrompts[1])

	// Simulate what PromptAssemblyStage does on the next execution of the same
	// pipeline: reload the build-time prompt over whatever the handoff set.
	turnState.SystemPrompt = "ORIGIN PROMPT"

	runHandoffTurn(t, stage)

	require.Equal(t, "DESTINATION PROMPT", provider.seenPrompts[2],
		"a re-executed turn must be reconciled back to the current state")
}

// Reporting the state the turn is already running must not disturb it.
func TestProviderStage_MatchingStateLeavesPromptUnchanged(t *testing.T) {
	resolver := &fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "ORIGIN PROMPT", AllowedTools: []string{"workflow__transition"}},
	}}

	stage, provider, _ := newHandoffStage(t, resolver)
	runHandoffTurn(t, stage)

	require.Len(t, provider.seenPrompts, 2)
	require.Equal(t, "ORIGIN PROMPT", provider.seenPrompts[0])
	require.Equal(t, "ORIGIN PROMPT", provider.seenPrompts[1],
		"an unchanged state must not alter the system prompt")
}

// Stop ends the turn rather than running another round under a prompt the
// runtime must not use — externally orchestrated states pause for an injected
// event, composition states are run by CompositionStage.
func TestProviderStage_StopEndsTurnWithoutFurtherRound(t *testing.T) {
	resolver := &fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "ORIGIN PROMPT", AllowedTools: []string{"workflow__transition"}},
		{Stop: true},
	}}

	stage, provider, _ := newHandoffStage(t, resolver)
	runHandoffTurn(t, stage)

	require.Len(t, provider.seenPrompts, 1,
		"no round may run after the resolver reports Stop")
}

// A nil resolver is the non-workflow case and must be inert.
func TestProviderStage_NilResolverIsInert(t *testing.T) {
	stage, provider, _ := newHandoffStage(t, nil)
	runHandoffTurn(t, stage)

	require.Len(t, provider.seenPrompts, 2)
	require.Equal(t, "ORIGIN PROMPT", provider.seenPrompts[1])
}

// A resolver error must abort the turn rather than silently continuing under
// the wrong state's prompt.
func TestProviderStage_HandoffErrorAbortsTurn(t *testing.T) {
	resolver := &fakeResolver{err: context.DeadlineExceeded}

	stage, provider, _ := newHandoffStage(t, resolver)

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
	require.Empty(t, provider.seenPrompts,
		"the loop-start reconcile must fail before any provider round")
}

// streamingHandoffProvider drives the OTHER tool loop.
// executeAndEmit routes on SupportsStreaming(), and the streaming loop is a
// parallel implementation of executeMultiRound — it rebuilds
// streamingRoundParams per round rather than sharing code, so the handoff
// wiring has to be verified there independently.
type streamingHandoffProvider struct {
	*mock.ToolProvider
	seenPrompts []string
}

func (p *streamingHandoffProvider) SupportsStreaming() bool { return true }

func (p *streamingHandoffProvider) PredictStreamWithTools(
	_ context.Context,
	req providers.PredictionRequest,
	_ providers.ProviderTools,
	_ string,
) (<-chan providers.StreamChunk, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)
	round := len(p.seenPrompts)

	out := make(chan providers.StreamChunk, 2)
	go func() {
		defer close(out)
		if round == 1 {
			reason := "tool_calls"
			out <- providers.StreamChunk{
				ToolCalls: []types.MessageToolCall{{
					ID:   "call-1",
					Name: "workflow__transition",
					Args: []byte(`{"event":"Escalate","context":"caller verified"}`),
				}},
				FinishReason: &reason,
			}
			return
		}
		reason := "stop"
		out <- providers.StreamChunk{
			Content:      "destination speaking",
			Delta:        "destination speaking",
			FinishReason: &reason,
		}
	}()
	return out, nil
}

// The streaming loop must apply the handoff exactly as the unary loop does.
func TestProviderStage_StreamingLoopSwapsSystemPromptMidTurn(t *testing.T) {
	resolver := &fakeResolver{sequence: originThenDestination()}

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "workflow__transition",
		Description: "Transition the workflow",
		InputSchema: []byte(`{"type":"object"}`),
	}))

	provider := &streamingHandoffProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
	}

	turnState := NewTurnState()
	turnState.SystemPrompt = "ORIGIN PROMPT"
	turnState.AllowedTools = []string{"workflow__transition"}

	stage := NewProviderStageWithTurnState(provider, registry, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, nil, turnState)
	stage.SetWorkflowStateResolver(resolver)

	runHandoffTurn(t, stage)

	require.Len(t, provider.seenPrompts, 2,
		"the streaming loop must run a second round after the transition")
	require.Equal(t, "ORIGIN PROMPT", provider.seenPrompts[0])
	require.Equal(t, "DESTINATION PROMPT", provider.seenPrompts[1],
		"streaming round after the transition must run as the destination state")
}

// Each assistant message carries the state that produced it. A turn can span
// states, so per-turn attribution is not sufficient.
func TestProviderStage_StampsStatePerAssistantMessage(t *testing.T) {
	resolver := &fakeResolver{
		sequence: originThenDestination(),
		meta:     map[string]any{"current_state": "handoff"},
	}
	stage, _, _ := newHandoffStage(t, resolver)

	response := types.Message{Role: "assistant", Content: "hi"}
	stage.stampWorkflowState(&response)

	require.Equal(t, map[string]any{"current_state": "handoff"},
		response.Meta[workflowStateMetaKey])
}

func TestProviderStage_StampIsNoOpWithoutResolverOrMeta(t *testing.T) {
	t.Run("nil resolver", func(t *testing.T) {
		stage, _, _ := newHandoffStage(t, nil)
		response := types.Message{Role: "assistant", Content: "hi"}
		stage.stampWorkflowState(&response)
		require.Nil(t, response.Meta)
	})

	t.Run("empty meta", func(t *testing.T) {
		stage, _, _ := newHandoffStage(t, &fakeResolver{})
		response := types.Message{Role: "assistant", Content: "hi"}
		stage.stampWorkflowState(&response)
		require.Nil(t, response.Meta)
	})
}

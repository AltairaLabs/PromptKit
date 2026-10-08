package stage

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A handoff's Call switches the provider and parameters the next round runs
// with (#2206).

// callRound is what one round looked like to the provider that served it.
type callRound struct {
	system      string
	maxTokens   int
	temperature float32
	tools       providers.ProviderTools
}

// callRecordingProvider records each round, builds tools tagged with its own
// id, and calls the transition tool on its first round when transitions is
// set.
type callRecordingProvider struct {
	*mock.ToolProvider
	transitions bool
	streaming   bool
	rounds      []callRound
}

func newCallRecordingProvider(id string, transitions bool) *callRecordingProvider {
	return &callRecordingProvider{
		ToolProvider: mock.NewToolProvider(id, id+"-model", false, nil),
		transitions:  transitions,
	}
}

func (p *callRecordingProvider) SupportsStreaming() bool { return p.streaming }

func (p *callRecordingProvider) BuildTooling(_ []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	return "tools-of-" + p.ID(), nil
}

func (p *callRecordingProvider) next(req providers.PredictionRequest, tools providers.ProviderTools) bool {
	p.rounds = append(p.rounds, callRound{req.System, req.MaxTokens, req.Temperature, tools})
	return p.transitions && len(p.rounds) == 1
}

func (p *callRecordingProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	// Recorded only: a call without tools cannot carry the scripted transition.
	p.rounds = append(p.rounds, callRound{req.System, req.MaxTokens, req.Temperature, nil})
	return providers.PredictionResponse{Content: "from " + p.ID()}, nil
}

func (p *callRecordingProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, tools providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if p.next(req, tools) {
		calls := []types.MessageToolCall{{ID: "call-1", Name: "workflow__transition", Args: []byte(`{"event":"Go"}`)}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "from " + p.ID()}, nil, nil
}

func (p *callRecordingProvider) PredictStreamWithTools(
	_ context.Context, req providers.PredictionRequest, tools providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	if !p.streaming {
		return nil, errors.New(p.ID() + " does not stream")
	}
	transition := p.next(req, tools)
	out := make(chan providers.StreamChunk, 1)
	go func() {
		defer close(out)
		if transition {
			reason := "tool_calls"
			out <- providers.StreamChunk{FinishReason: &reason, ToolCalls: []types.MessageToolCall{{
				ID: "call-1", Name: "workflow__transition", Args: []byte(`{"event":"Go"}`),
			}}}
			return
		}
		reason := "stop"
		out <- providers.StreamChunk{Content: "from " + p.ID(), Delta: "from " + p.ID(), FinishReason: &reason}
	}()
	return out, nil
}

// originToDestinationCall keeps the system prompt identical across the
// handoff, so only the call changes: the tools must still be rebuilt for the
// destination's provider.
func originToDestinationCall(dest providers.Provider) []Handoff {
	return []Handoff{
		{Valid: true, SystemPrompt: "SAME PROMPT", AllowedTools: []string{"workflow__transition"}},
		{Valid: true, SystemPrompt: "SAME PROMPT", AllowedTools: []string{"workflow__transition"},
			Call: &HandoffCall{Provider: dest, MaxTokens: 64, Temperature: 0.3}},
	}
}

func newCallHandoffStage(t *testing.T, origin providers.Provider, resolver WorkflowStateResolver) *ProviderStage {
	t.Helper()
	stage, _, turnState := newHandoffStage(t, resolver)
	turnState.SystemPrompt = "SAME PROMPT"
	s := NewProviderStageWithTurnState(origin, stage.toolRegistry, nil, &ProviderConfig{
		MaxTokens: 100, Temperature: 0.9,
	}, nil, nil, turnState)
	s.SetWorkflowStateResolver(resolver)
	return s
}

func TestProviderStage_HandoffCallSwitchesProviderAndParameters(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := map[bool]string{false: "unary", true: "streaming"}[streaming]
		t.Run(name, func(t *testing.T) {
			origin := newCallRecordingProvider("origin", true)
			dest := newCallRecordingProvider("dest", false)
			origin.streaming, dest.streaming = streaming, streaming
			stage := newCallHandoffStage(t, origin, &fakeResolver{sequence: originToDestinationCall(dest)})

			runHandoffTurn(t, stage)

			require.Len(t, origin.rounds, 1, "the origin serves only the round before the handoff")
			assert.Equal(t, callRound{"SAME PROMPT", 100, 0.9, "tools-of-origin"}, origin.rounds[0])
			require.Len(t, dest.rounds, 1, "the round after the handoff runs on the destination's provider")
			assert.Equal(t, callRound{"SAME PROMPT", 64, 0.3, "tools-of-dest"}, dest.rounds[0],
				"its parameters, and tools built by it even though the prompt did not change")
		})
	}
}

// A stage reused for the next execution starts on its own provider again, not
// on the previous execution's handoff.
func TestProviderStage_HandoffCallDoesNotOutliveTheExecution(t *testing.T) {
	origin := newCallRecordingProvider("origin", true)
	dest := newCallRecordingProvider("dest", false)
	resolver := &fakeResolver{sequence: originToDestinationCall(dest)}
	stage := newCallHandoffStage(t, origin, resolver)
	runHandoffTurn(t, stage)
	require.Len(t, dest.rounds, 1)

	resolver.sequence, resolver.calls = []Handoff{{Valid: true, SystemPrompt: "SAME PROMPT"}}, 0
	runHandoffTurn(t, stage)
	assert.Len(t, origin.rounds, 2, "the next execution runs on the stage's own provider")
	assert.Len(t, dest.rounds, 1)
}

// sameProvider never panics, even on an uncomparable provider value.
func TestSameProvider(t *testing.T) {
	a := newCallRecordingProvider("a", false)
	b := newCallRecordingProvider("b", false)
	assert.True(t, sameProvider(a, a))
	assert.False(t, sameProvider(a, b))
	assert.False(t, sameProvider(a, nil))
	assert.True(t, sameProvider(nil, nil))
	type uncomparable struct {
		providers.Provider
		m map[string]int
	}
	u := uncomparable{Provider: a}
	assert.NotPanics(t, func() {
		assert.True(t, sameProvider(u, u), "the same id stands for the same provider")
		assert.False(t, sameProvider(u, uncomparable{Provider: b}))
	})
}

// A streaming turn that hands off to a provider that does not stream calls it
// whole, and the reply still completes the turn.
func TestProviderStage_HandoffToNonStreamingProviderInStreamingTurn(t *testing.T) {
	origin := newCallRecordingProvider("origin", true)
	origin.streaming = true
	dest := newCallRecordingProvider("dest", false) // does not stream
	stage := newCallHandoffStage(t, origin, &fakeResolver{sequence: originToDestinationCall(dest)})

	runHandoffTurn(t, stage)

	require.Len(t, origin.rounds, 1)
	require.Len(t, dest.rounds, 1, "the destination is called whole, not via PredictStream")
	assert.Equal(t, callRound{"SAME PROMPT", 64, 0.3, "tools-of-dest"}, dest.rounds[0])
}

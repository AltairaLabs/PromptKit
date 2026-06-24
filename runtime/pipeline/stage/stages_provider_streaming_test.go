package stage

import (
	"context"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multiTurnRecordingProvider records the messages of every Predict call and
// returns a numbered reply, so tests can assert one provider call per turn and
// that later turns see earlier turns as history.
type multiTurnRecordingProvider struct {
	requests [][]types.Message
}

func (p *multiTurnRecordingProvider) ID() string                          { return "rec" }
func (p *multiTurnRecordingProvider) Name() string                        { return "rec" }
func (p *multiTurnRecordingProvider) Type() base.ProviderType             { return base.ProviderTypeInference }
func (p *multiTurnRecordingProvider) Pricing() *base.PricingDescriptor    { return nil }
func (p *multiTurnRecordingProvider) Validate() error                     { return nil }
func (p *multiTurnRecordingProvider) Init(_ context.Context) error        { return nil }
func (p *multiTurnRecordingProvider) HealthCheck(_ context.Context) error { return nil }
func (p *multiTurnRecordingProvider) Model() string                       { return "rec-model" }
func (p *multiTurnRecordingProvider) SupportsStreaming() bool             { return false }
func (p *multiTurnRecordingProvider) ShouldIncludeRawOutput() bool        { return false }
func (p *multiTurnRecordingProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}
func (p *multiTurnRecordingProvider) Close() error { return nil }

func (p *multiTurnRecordingProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	msgs := make([]types.Message, len(req.Messages))
	copy(msgs, req.Messages)
	p.requests = append(p.requests, msgs)
	return providers.PredictionResponse{Content: fmt.Sprintf("reply-%d", len(p.requests))}, nil
}

func (p *multiTurnRecordingProvider) PredictStream(
	_ context.Context, _ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

// TestProviderStage_Streaming_OneReplyPerTurn verifies that with Streaming
// enabled, the provider fires once per EndOfTurn (not once at channel close),
// emits an assistant reply per turn, and re-emits an EndOfTurn boundary after
// each reply.
func TestProviderStage_Streaming_OneReplyPerTurn(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	ts := NewTurnState()
	ts.SystemPrompt = "sys"
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, ts)

	input := make(chan StreamElement, 8)
	input <- NewMessageElement(&types.Message{Role: "user", Content: "hello one"})
	input <- NewEndOfTurnElement()
	input <- NewMessageElement(&types.Message{Role: "user", Content: "hello two"})
	input <- NewEndOfTurnElement()
	close(input)

	output := make(chan StreamElement, 64)
	require.NoError(t, stage.Process(context.Background(), input, output))

	var assistant []*types.Message
	endOfTurns := 0
	for e := range output {
		switch {
		case e.EndOfTurn:
			endOfTurns++
		case e.Message != nil && e.Message.Role == roleAssistant:
			m := e.Message
			assistant = append(assistant, m)
		}
	}

	require.Len(t, prov.requests, 2, "provider should fire once per EndOfTurn, not once at close")
	require.Len(t, assistant, 2, "one assistant reply per turn")
	assert.Equal(t, "reply-1", assistant[0].Content)
	assert.Equal(t, "reply-2", assistant[1].Content)
	assert.Equal(t, 2, endOfTurns, "an EndOfTurn boundary is re-emitted after each reply")
}

// TestProviderStage_Streaming_ThreadsHistory verifies turn 2's provider request
// carries turn 1 (user message + assistant reply) as history.
func TestProviderStage_Streaming_ThreadsHistory(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	ts := NewTurnState()
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, ts)

	input := make(chan StreamElement, 8)
	input <- NewMessageElement(&types.Message{Role: "user", Content: "first question"})
	input <- NewEndOfTurnElement()
	input <- NewMessageElement(&types.Message{Role: "user", Content: "second question"})
	input <- NewEndOfTurnElement()
	close(input)

	output := make(chan StreamElement, 64)
	require.NoError(t, stage.Process(context.Background(), input, output))

	require.Len(t, prov.requests, 2)
	require.Len(t, prov.requests[0], 1, "turn 1 sees only its own user message")
	assert.Equal(t, "first question", prov.requests[0][0].Content)

	// Turn 2 sees: turn-1 user, turn-1 assistant reply, turn-2 user.
	require.Len(t, prov.requests[1], 3, "turn 2 sees turn 1 as history plus its own user message")
	assert.Equal(t, "first question", prov.requests[1][0].Content)
	assert.Equal(t, roleAssistant, prov.requests[1][1].Role)
	assert.Equal(t, "reply-1", prov.requests[1][1].Content)
	assert.Equal(t, "second question", prov.requests[1][2].Content)
}

// TestProviderStage_Streaming_EmitsUserMessages verifies the user transcript for
// each turn is forwarded downstream (so the save stage persists it), not just
// the assistant reply. The provider drains its input, so it must re-emit the
// turn's user messages itself.
func TestProviderStage_Streaming_EmitsUserMessages(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	input := make(chan StreamElement, 4)
	input <- NewMessageElement(&types.Message{Role: "user", Content: "persist me"})
	input <- NewEndOfTurnElement()
	close(input)

	output := make(chan StreamElement, 16)
	require.NoError(t, stage.Process(context.Background(), input, output))

	var roles []string
	for e := range output {
		if e.Message != nil {
			roles = append(roles, e.Message.Role)
		}
	}
	assert.Contains(t, roles, "user", "the turn's user message must be emitted downstream")
	assert.Contains(t, roles, roleAssistant, "the assistant reply must be emitted downstream")
}

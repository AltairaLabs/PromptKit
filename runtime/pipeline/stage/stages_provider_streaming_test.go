package stage

import (
	"context"
	"fmt"
	"sync"
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

// streamingRecordingProvider is the streaming counterpart of
// multiTurnRecordingProvider: SupportsStreaming reports true so the stage takes
// the executeStreamingMultiRound sub-path.
type streamingRecordingProvider struct {
	multiTurnRecordingProvider
}

func (p *streamingRecordingProvider) SupportsStreaming() bool { return true }

func (p *streamingRecordingProvider) PredictStream(
	_ context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	msgs := make([]types.Message, len(req.Messages))
	copy(msgs, req.Messages)
	p.requests = append(p.requests, msgs)
	reply := fmt.Sprintf("reply-%d", len(p.requests))
	ch := make(chan providers.StreamChunk, 1)
	stop := "stop"
	ch <- providers.StreamChunk{
		Content:      reply,
		Delta:        reply,
		FinishReason: &stop,
		FinalResult:  &providers.PredictionResponse{Content: reply},
	}
	close(ch)
	return ch, nil
}

// erroringProvider always fails its Predict call, to exercise the turn-level
// error path (the session must survive).
type erroringProvider struct {
	multiTurnRecordingProvider
}

func (p *erroringProvider) Predict(
	_ context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, fmt.Errorf("boom")
}

// blockingProvider blocks in Predict until its context is cancelled, signaling
// when generation is in-flight so a test can drive a barge-in mid-generation.
type blockingProvider struct {
	multiTurnRecordingProvider
	started chan struct{}
	once    sync.Once
}

func (p *blockingProvider) Predict(
	ctx context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return providers.PredictionResponse{}, ctx.Err()
}

// TestProviderStage_Streaming_InterruptCancelsGeneration verifies a barge-in
// Interrupt cancels in-flight provider generation: the turn is dropped (no
// assistant reply), the Interrupt is forwarded, and the session survives.
func TestProviderStage_Streaming_InterruptCancelsGeneration(t *testing.T) {
	prov := &blockingProvider{started: make(chan struct{})}
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	input := make(chan StreamElement)
	output := make(chan StreamElement, 16)
	done := make(chan error, 1)
	go func() { done <- stage.Process(context.Background(), input, output) }()

	input <- NewMessageElement(&types.Message{Role: "user", Content: "hello"})
	input <- NewEndOfTurnElement()
	<-prov.started // generation is in-flight
	input <- NewInterruptElement()
	close(input)

	require.NoError(t, <-done)

	var assistant, interrupts int
	for e := range output {
		if e.Message != nil && e.Message.Role == roleAssistant {
			assistant++
		}
		if e.Interrupt {
			interrupts++
		}
	}
	assert.Equal(t, 0, assistant, "interrupted generation must yield no assistant reply")
	assert.Equal(t, 1, interrupts, "the Interrupt must be forwarded downstream")
}

// TestProviderStage_Streaming_NilTurnState verifies streaming mode works when no
// TurnState is wired (the ad-hoc construction path).
func TestProviderStage_Streaming_NilTurnState(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	stage := NewProviderStage(prov, nil, nil, &ProviderConfig{Streaming: true})

	input := make(chan StreamElement, 4)
	input <- NewMessageElement(&types.Message{Role: "user", Content: "hi"})
	input <- NewEndOfTurnElement()
	close(input)

	output := make(chan StreamElement, 16)
	require.NoError(t, stage.Process(context.Background(), input, output))

	require.Len(t, prov.requests, 1)
}

// TestProviderStage_Streaming_EndOfStreamFires verifies an EndOfStream fires the
// trailing turn (no explicit EndOfTurn needed) and then stops.
func TestProviderStage_Streaming_EndOfStreamFires(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	input := make(chan StreamElement, 4)
	input <- NewMessageElement(&types.Message{Role: "user", Content: "last words"})
	input <- NewEndOfStreamElement()
	close(input)

	output := make(chan StreamElement, 16)
	require.NoError(t, stage.Process(context.Background(), input, output))

	require.Len(t, prov.requests, 1, "EndOfStream fires the buffered turn")
}

// TestProviderStage_Streaming_ForwardsInterrupt verifies an Interrupt element is
// passed through downstream (for the barge-in path), not swallowed.
func TestProviderStage_Streaming_ForwardsInterrupt(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	input := make(chan StreamElement, 2)
	input <- NewInterruptElement()
	close(input)

	output := make(chan StreamElement, 8)
	require.NoError(t, stage.Process(context.Background(), input, output))

	sawInterrupt := false
	for e := range output {
		if e.Interrupt {
			sawInterrupt = true
		}
	}
	assert.True(t, sawInterrupt, "Interrupt must be forwarded downstream")
}

// TestProviderStage_Streaming_EndOfTurnNoPending verifies an EndOfTurn with no
// buffered messages is a no-op (no provider call, no panic).
func TestProviderStage_Streaming_EndOfTurnNoPending(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	input := make(chan StreamElement, 2)
	input <- NewEndOfTurnElement()
	close(input)

	output := make(chan StreamElement, 4)
	require.NoError(t, stage.Process(context.Background(), input, output))

	assert.Empty(t, prov.requests, "EndOfTurn with no pending input must not call the provider")
}

// TestProviderStage_Streaming_StreamingProvider verifies the streaming-provider
// sub-path (executeStreamingMultiRound) produces a reply per turn.
func TestProviderStage_Streaming_StreamingProvider(t *testing.T) {
	prov := &streamingRecordingProvider{}
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	input := make(chan StreamElement, 4)
	input <- NewMessageElement(&types.Message{Role: "user", Content: "stream turn"})
	input <- NewEndOfTurnElement()
	close(input)

	output := make(chan StreamElement, 32)
	require.NoError(t, stage.Process(context.Background(), input, output))

	require.Len(t, prov.requests, 1)
	var assistant []*types.Message
	for e := range output {
		if e.Message != nil && e.Message.Role == roleAssistant {
			m := e.Message
			assistant = append(assistant, m)
		}
	}
	require.Len(t, assistant, 1, "streaming provider yields one assistant reply for the turn")
	assert.Equal(t, "reply-1", assistant[0].Content)
}

// TestProviderStage_Streaming_TurnErrorSurvives verifies a failed turn emits an
// error element but keeps the session alive for the next turn.
func TestProviderStage_Streaming_TurnErrorSurvives(t *testing.T) {
	prov := &erroringProvider{}
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	input := make(chan StreamElement, 4)
	input <- NewMessageElement(&types.Message{Role: "user", Content: "will fail"})
	input <- NewEndOfTurnElement()
	close(input)

	output := make(chan StreamElement, 8)
	require.NoError(t, stage.Process(context.Background(), input, output), "a turn error must not tear down the session")

	sawError := false
	for e := range output {
		if e.Error != nil {
			sawError = true
		}
	}
	assert.True(t, sawError, "a failed turn surfaces an error element")
}

// TestProviderStage_Streaming_ContextCancelled verifies sending honors context
// cancellation while forwarding an element.
func TestProviderStage_Streaming_ContextCancelled(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before processing

	input := make(chan StreamElement, 1)
	input <- NewInterruptElement() // routed to the forwarding (default) path
	close(input)

	output := make(chan StreamElement) // unbuffered: send blocks, ctx.Done wins
	err := stage.Process(ctx, input, output)
	assert.ErrorIs(t, err, context.Canceled)
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

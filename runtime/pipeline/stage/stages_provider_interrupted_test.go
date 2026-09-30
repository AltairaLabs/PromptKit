package stage

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// partialThenBlockProvider streams some text on its first call, then waits for
// the turn to be canceled; later calls reply normally. It records every
// request.
type partialThenBlockProvider struct {
	multiTurnRecordingProvider
	mu      sync.Mutex
	started chan struct{}
}

func (p *partialThenBlockProvider) SupportsStreaming() bool { return true }

func (p *partialThenBlockProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, append([]types.Message(nil), req.Messages...))
	first := len(p.requests) == 1
	p.mu.Unlock()

	ch := make(chan providers.StreamChunk)
	go func() {
		defer close(ch)
		if first {
			ch <- providers.StreamChunk{Content: "Well, the", Delta: "Well, the"}
			close(p.started)
			<-ctx.Done()
			return
		}
		stop := "stop"
		ch <- providers.StreamChunk{Content: "Sure.", Delta: "Sure.", FinishReason: &stop}
	}()
	return ch, nil
}

// TestProviderStage_Streaming_BargeInKeepsPartialReply covers the streaming
// (voice) turn path: a barge-in cancels the reply mid-sentence. What the model
// had said is emitted and kept in the session history marked interrupted, and
// the next turn does not send it back to the model.
func TestProviderStage_Streaming_BargeInKeepsPartialReply(t *testing.T) {
	prov := &partialThenBlockProvider{started: make(chan struct{})}
	stage := NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState())

	input := make(chan StreamElement)
	output := make(chan StreamElement, 32)
	done := make(chan error, 1)
	go func() { done <- stage.Process(context.Background(), input, output) }()

	input <- NewMessageElement(&types.Message{Role: "user", Content: "tell me something"})
	input <- NewEndOfTurnElement()
	<-prov.started // the reply is mid-sentence
	input <- NewInterruptElement()
	input <- NewMessageElement(&types.Message{Role: "user", Content: "actually, stop"})
	input <- NewEndOfTurnElement()
	close(input)
	require.NoError(t, <-done)

	var interrupted *types.Message
	for e := range output {
		if e.Message != nil && e.Message.IsInterrupted() {
			interrupted = e.Message
		}
	}
	require.NotNil(t, interrupted, "the partial reply must be emitted")
	assert.Equal(t, "Well, the", interrupted.Content)

	prov.mu.Lock()
	defer prov.mu.Unlock()
	require.Len(t, prov.requests, 2)
	second := prov.requests[1]
	for _, m := range second {
		assert.False(t, m.IsInterrupted(), "the interrupted reply was sent back to the model")
	}
	require.Len(t, second, 2, "both user turns stay in context")
	assert.Equal(t, "tell me something", second[0].GetContent())
	assert.Equal(t, "actually, stop", second[1].GetContent())
}

func TestProcessStreamChunks_ChunkErrorKeepsPartialOutput(t *testing.T) {
	s := &ProviderStage{}
	in := make(chan providers.StreamChunk, 4)
	out := make(chan StreamElement, 16)
	in <- providers.StreamChunk{Reasoning: "let me think"}
	in <- providers.StreamChunk{Content: "The answer", Delta: "The answer"}
	in <- providers.StreamChunk{Error: errors.New("connection reset")}
	close(in)

	got, err := s.processStreamChunks(context.Background(), in, out, roundRef{round: 1}, false)
	require.ErrorContains(t, err, "connection reset")
	assert.Equal(t, "The answer", got.content)
	require.NotNil(t, got.reasoning)
	assert.Equal(t, "let me think", got.reasoning.Text)
	assert.True(t, got.hasOutput())
}

// TestProcessStreamChunks_CanceledStreamWithoutErrorIsInterrupted covers a
// provider whose stream just closes when the caller cancels. The truncated
// reply must come back as an error, not as a completed round.
func TestProcessStreamChunks_CanceledStreamWithoutErrorIsInterrupted(t *testing.T) {
	s := &ProviderStage{}
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan providers.StreamChunk, 4)
	out := make(chan StreamElement, 16)
	in <- providers.StreamChunk{Content: "half a", Delta: "half a"}
	cancel()
	close(in)

	got, err := s.processStreamChunks(ctx, in, out, roundRef{round: 1}, false)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "half a", got.content)
}

func TestProcessStreamChunks_CancelAfterFinishIsNotInterrupted(t *testing.T) {
	s := &ProviderStage{}
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan providers.StreamChunk)
	out := make(chan StreamElement)
	stop := "stop"
	// Cancel only once the finished chunk has been emitted, then end the stream.
	go func() {
		in <- providers.StreamChunk{Content: "done", Delta: "done", FinishReason: &stop}
		<-out
		cancel()
		close(in)
	}()

	got, err := s.processStreamChunks(ctx, in, out, roundRef{round: 1}, false)
	require.NoError(t, err)
	assert.Equal(t, "done", got.content)
}

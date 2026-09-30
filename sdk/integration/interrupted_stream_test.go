package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

var errStreamDropped = errors.New("upstream dropped the stream")

// streamStep is one PredictStream call's behavior: the chunks it sends, then
// how it ends.
type streamStep struct {
	chunks []providers.StreamChunk
	// fail ends the stream with this error chunk after the chunks.
	fail error
	// waitCancel makes the stream wait for the caller to cancel after the
	// chunks, then close with no error chunk — as a provider that just stops
	// reading does.
	waitCancel bool
	// sent is closed once the chunks have been sent, if non-nil.
	sent chan struct{}
}

// scriptedStreamProvider streams one step per call and records the messages
// each call was sent.
type scriptedStreamProvider struct {
	base.Implementation
	mu       sync.Mutex
	steps    []streamStep
	requests [][]types.Message
}

func (p *scriptedStreamProvider) ID() string                   { return "scripted" }
func (p *scriptedStreamProvider) Model() string                { return "scripted-model" }
func (p *scriptedStreamProvider) SupportsStreaming() bool      { return true }
func (p *scriptedStreamProvider) ShouldIncludeRawOutput() bool { return false }
func (p *scriptedStreamProvider) Close() error                 { return nil }
func (p *scriptedStreamProvider) CalculateCost(int, int, int) types.CostInfo {
	return types.CostInfo{}
}

func (p *scriptedStreamProvider) Predict(
	context.Context, providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, errors.New("scripted provider streams only")
}

func (p *scriptedStreamProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, append([]types.Message(nil), req.Messages...))
	step := p.steps[0]
	p.steps = p.steps[1:]
	p.mu.Unlock()

	out := make(chan providers.StreamChunk)
	go func() {
		defer close(out)
		for _, c := range step.chunks {
			select {
			case out <- c:
			case <-ctx.Done():
				return
			}
		}
		if step.sent != nil {
			close(step.sent)
		}
		switch {
		case step.fail != nil:
			out <- providers.StreamChunk{Error: step.fail}
		case step.waitCancel:
			<-ctx.Done()
		}
	}()
	return out, nil
}

func (p *scriptedStreamProvider) request(i int) []types.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[i]
}

// partialThenAnswer scripts a stream that dies after reasoning and some text,
// followed by a normal reply for the next turn.
func partialThenAnswer(first streamStep) *scriptedStreamProvider {
	stop := "stop"
	return &scriptedStreamProvider{steps: []streamStep{
		first,
		{chunks: []providers.StreamChunk{
			{Delta: "Four.", Content: "Four.", FinishReason: &stop},
		}},
	}}
}

var partialChunks = []providers.StreamChunk{
	{Reasoning: "The user wants a story; start with a setting."},
	{Delta: "Once upon", Content: "Once upon"},
	{Delta: " a time", Content: "Once upon a time"},
}

func TestInterruptedStream_SendReturnsAndPersistsPartialReply(t *testing.T) {
	provider := partialThenAnswer(streamStep{chunks: partialChunks, fail: errStreamDropped})
	conv := openTestConv(t, sdk.WithProvider(provider))
	ctx := context.Background()

	resp, err := conv.Send(ctx, "Tell me a story")
	require.ErrorIs(t, err, errStreamDropped, "the failure must still reach the caller")
	require.NotNil(t, resp, "the partial reply must come back with the error")
	assert.Equal(t, "Once upon a time", resp.Text())
	assert.Equal(t, types.FinishReasonInterrupted, resp.Message().FinishReason)
	require.NotNil(t, resp.Message().Reasoning)
	assert.Equal(t, "The user wants a story; start with a setting.", resp.Message().Reasoning.Text)

	history := conv.Messages(ctx)
	require.NotEmpty(t, history)
	last := history[len(history)-1]
	assert.Equal(t, "assistant", last.Role)
	assert.Equal(t, "Once upon a time", last.Content)
	assert.True(t, last.IsInterrupted(), "the transcript must mark the reply interrupted")
	assert.Contains(t, last.Meta[types.MetaInterruptedCause], errStreamDropped.Error())

	// The next turn works, and the model is never shown the fragment.
	_, err = conv.Send(ctx, "What is 2+2?")
	require.NoError(t, err)
	for _, m := range provider.request(1) {
		assert.NotEqual(t, "Once upon a time", m.GetContent(), "interrupted reply was sent back to the model")
	}
	assert.Equal(t, "Tell me a story", provider.request(1)[0].GetContent(),
		"the interrupted turn's user message stays in context")
}

// TestInterruptedStream_CanceledSendKeepsPartialReply covers a provider whose
// stream simply stops when the caller cancels — no error chunk at all.
func TestInterruptedStream_CanceledSendKeepsPartialReply(t *testing.T) {
	sent := make(chan struct{})
	provider := partialThenAnswer(streamStep{chunks: partialChunks, waitCancel: true, sent: sent})
	conv := openTestConv(t, sdk.WithProvider(provider))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sent
		cancel()
	}()

	resp, err := conv.Send(ctx, "Tell me a story")
	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, resp, "the partial reply must come back with the cancellation")
	assert.Equal(t, "Once upon a time", resp.Text())
	assert.Equal(t, types.FinishReasonInterrupted, resp.Message().FinishReason)

	history := conv.Messages(context.Background())
	require.NotEmpty(t, history)
	assert.True(t, history[len(history)-1].IsInterrupted(),
		"a canceled turn must still save its partial reply")
}

func TestInterruptedStream_StreamErrorChunkCarriesPartialReply(t *testing.T) {
	provider := partialThenAnswer(streamStep{chunks: partialChunks, fail: errStreamDropped})
	conv := openTestConv(t, sdk.WithProvider(provider))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var text string
	var final sdk.StreamChunk
	for chunk := range conv.Stream(ctx, "Tell me a story") {
		if chunk.Type == sdk.ChunkText {
			text += chunk.Text
		}
		final = chunk
	}
	assert.Equal(t, "Once upon a time", text, "deltas stream as usual before the failure")
	require.ErrorIs(t, final.Error, errStreamDropped)
	require.NotNil(t, final.Message, "the error chunk must carry the partial reply")
	assert.Equal(t, types.FinishReasonInterrupted, final.Message.Message().FinishReason)
	assert.Equal(t, "Once upon a time", final.Message.Text())
}

// TestInterruptedStream_ReasoningOnlyIsKept covers a stream that dies while the
// model is still thinking: there is no text, but the reply is still kept.
func TestInterruptedStream_ReasoningOnlyIsKept(t *testing.T) {
	provider := partialThenAnswer(streamStep{
		chunks: []providers.StreamChunk{{Reasoning: "Thinking about it"}},
		fail:   errStreamDropped,
	})
	conv := openTestConv(t, sdk.WithProvider(provider))

	resp, err := conv.Send(context.Background(), "Hard question")
	require.ErrorIs(t, err, errStreamDropped)
	require.NotNil(t, resp)
	assert.Equal(t, types.FinishReasonInterrupted, resp.Message().FinishReason)
	require.NotNil(t, resp.Message().Reasoning)
	assert.Equal(t, "Thinking about it", resp.Message().Reasoning.Text)
}

func TestInterruptedStream_FailureBeforeAnyOutputReturnsNoResponse(t *testing.T) {
	provider := partialThenAnswer(streamStep{fail: errStreamDropped})
	conv := openTestConv(t, sdk.WithProvider(provider))

	resp, err := conv.Send(context.Background(), "Tell me a story")
	require.ErrorIs(t, err, errStreamDropped)
	assert.Nil(t, resp, "with nothing produced there is no partial reply to return")
}

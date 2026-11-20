package sdk

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// An output guardrail on a streamed reply judges each chunk before the caller
// gets it. A word it bans never reaches the caller, not even the fragments a
// provider split it into, and a reply it blocks reaches the caller as its
// message. A cut call still reports what the provider billed for it.

const noPineappleMessage = "Let's talk about something else."

func noPineapple() Option {
	return WithGuardrail(guardrails.Output("content_excludes",
		map[string]any{"patterns": []any{"pineapple"}},
		guardrails.WithMessage(noPineappleMessage)))
}

// streamedTurn is what a caller of Stream saw for one turn.
type streamedTurn struct {
	deltas    []string
	resp      *Response
	completed []*events.ProviderCallCompletedData
}

// streamChunks opens a conversation whose model streams chunks, streams one
// turn through Stream and returns what the caller saw.
func streamChunks(t *testing.T, chunks []string, opts ...Option) streamedTurn {
	t.Helper()
	bus := events.NewEventBus()
	var mu sync.Mutex
	var got streamedTurn
	bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		if d, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			mu.Lock()
			got.completed = append(got.completed, d)
			mu.Unlock()
		}
	})
	repo := &sequenceRepo{turns: []mock.Turn{{Type: "text", StreamChunks: chunks}}}
	conv, err := Open(writeGatePack(t), "assist", append([]Option{
		WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
		WithSkipSchemaValidation(),
		WithEventBus(bus),
	}, opts...)...)
	require.NoError(t, err)
	for c := range conv.Stream(context.Background(), "what goes on pizza?") {
		require.NoError(t, c.Error)
		switch c.Type {
		case ChunkText:
			got.deltas = append(got.deltas, c.Text)
		case ChunkDone:
			got.resp = c.Message
		}
	}
	require.NoError(t, conv.Close())
	bus.Close()
	require.NotNil(t, got.resp, "the stream ended without its response")
	mu.Lock()
	defer mu.Unlock()
	return got
}

// A banned word split across chunks never reaches the caller: the chunks that
// could be its start are held until the word is complete, and then the
// guardrail's message is the turn's text, delivered as its deltas.
func TestStream_BlockedWordSplitAcrossChunksNeverReachesCaller(t *testing.T) {
	got := streamChunks(t, []string{"P", "ine", "apple is the best topping."}, noPineapple())

	assert.Equal(t, []string{noPineappleMessage}, got.deltas)
	assert.Equal(t, noPineappleMessage, got.resp.Text())
	assert.Equal(t, got.resp.Text(), strings.Join(got.deltas, ""))
	assert.Equal(t, types.FinishReasonSafety, got.resp.Message().FinishReason)
}

// Text before the banned word is released as it arrives; the chunk carrying
// the word's start is held back to just before it, and the guardrail's message
// follows.
func TestStream_TextBeforeBlockedWordIsReleased(t *testing.T) {
	got := streamChunks(t, []string{"Say P", "ine", "apple, please."}, noPineapple())

	assert.Equal(t, []string{"Say ", noPineappleMessage}, got.deltas)
	assert.Equal(t, noPineappleMessage, got.resp.Text())
}

// Held text that turns out not to be the banned word is released: "Pine" is
// held until " cones" rules the word out, and the reply's last "Pine" until the
// reply ends. A reply the guardrail passes reaches the caller whole, the
// deltas making up its text.
func TestStream_HeldTextIsReleasedWhenItIsNotTheBannedWord(t *testing.T) {
	got := streamChunks(t, []string{"Pine", " cones", " and Pine"}, noPineapple())

	assert.Equal(t, []string{"Pine cones", " and ", "Pine"}, got.deltas)
	assert.Equal(t, "Pine cones and Pine", got.resp.Text())
}

// A provider call the guardrail cut reports the usage the provider sent at the
// end of its stream: the same usage the same reply reports uncut.
func TestStream_CutCallReportsTheProvidersUsage(t *testing.T) {
	chunks := []string{"P", "ine", "apple is the best topping."}
	uncut := streamChunks(t, chunks)
	require.Len(t, uncut.completed, 1)
	require.Positive(t, uncut.completed[0].OutputTokens)

	cut := streamChunks(t, chunks, noPineapple())

	require.Len(t, cut.completed, 1)
	assert.Equal(t, uncut.completed[0].InputTokens, cut.completed[0].InputTokens)
	assert.Equal(t, uncut.completed[0].OutputTokens, cut.completed[0].OutputTokens)
	assert.InDelta(t, uncut.completed[0].Cost, cut.completed[0].Cost, 1e-12)
	assert.Equal(t, uncut.resp.OutputTokens(), cut.resp.OutputTokens())
	assert.Equal(t, uncut.resp.InputTokens(), cut.resp.InputTokens())
}

// Send judges the whole reply at once: a blocked reply is the guardrail's
// message, and the call reports the usage of the reply the provider produced,
// as Stream does.
func TestSend_BlockedReplyIsTheMessageAndReportsTheProvidersUsage(t *testing.T) {
	send := func(opts ...Option) *Response {
		repo := &sequenceRepo{turns: []mock.Turn{{Type: "text", StreamChunks: []string{"P", "ine", "apple!"}}}}
		conv, err := Open(writeGatePack(t), "assist", append([]Option{
			WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
			WithSkipSchemaValidation(),
		}, opts...)...)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		resp, err := conv.Send(context.Background(), "what goes on pizza?")
		require.NoError(t, err)
		return resp
	}
	uncut := send()
	require.Equal(t, "Pineapple!", uncut.Text())

	blocked := send(noPineapple())

	assert.Equal(t, noPineappleMessage, blocked.Text())
	assert.Equal(t, uncut.InputTokens(), blocked.InputTokens())
	assert.Equal(t, uncut.OutputTokens(), blocked.OutputTokens())
	assert.Positive(t, blocked.OutputTokens())
}

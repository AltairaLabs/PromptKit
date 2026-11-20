package mock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// oneTurnRepo answers every request with turn.
type oneTurnRepo struct{ turn Turn }

func (r *oneTurnRepo) GetTurn(context.Context, ResponseParams) (*Turn, error) {
	t := r.turn
	return &t, nil
}

func (r *oneTurnRepo) GetResponse(context.Context, ResponseParams) (string, error) {
	return r.turn.Content, nil
}

// streamedChunk is the part of a chunk a scripted stream sets.
type streamedChunk struct {
	Delta, Content, Finish string
	OutputTokens           int
}

func collectStream(t *testing.T, ch <-chan providers.StreamChunk) []streamedChunk {
	t.Helper()
	var got []streamedChunk
	for c := range ch {
		require.NoError(t, c.Error)
		s := streamedChunk{Delta: c.Delta, Content: c.Content}
		if c.FinishReason != nil {
			s.Finish = *c.FinishReason
		}
		if c.CostInfo != nil {
			s.OutputTokens = c.CostInfo.OutputTokens
		}
		got = append(got, s)
	}
	return got
}

// A turn that scripts its chunks streams one chunk per entry, then a final
// chunk with the finish reason and the usage of the whole reply, on both the
// plain and the tool-calling stream.
func TestStreamChunks_StreamOneChunkPerEntryThenUsage(t *testing.T) {
	turn := Turn{Type: "text", StreamChunks: []string{"Say P", "ine", "apple, please."}}
	// 22 bytes of reply: 5 output tokens at the mock's 4 bytes per token.
	want := []streamedChunk{
		{Delta: "Say P", Content: "Say P"},
		{Delta: "ine", Content: "Say Pine"},
		{Delta: "apple, please.", Content: "Say Pineapple, please."},
		{Content: "Say Pineapple, please.", Finish: types.FinishReasonStop, OutputTokens: 5},
	}
	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}

	plain, err := NewProviderWithRepository("p", "m", false, &oneTurnRepo{turn: turn}).PredictStream(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, want, collectStream(t, plain))

	tp := NewToolProviderWithRepository("p", "m", false, &oneTurnRepo{turn: turn})
	withTools, err := tp.PredictStreamWithTools(t.Context(), req, nil, "auto")
	require.NoError(t, err)
	assert.Equal(t, want, collectStream(t, withTools))
}

// A turn that also sets Content streams its chunks, and Content is the reply.
func TestStreamChunks_ContentIsTheReplyWhenSet(t *testing.T) {
	turn := Turn{Type: "text", Content: "Hello there", StreamChunks: []string{"Hel", "lo"}}
	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}

	ch, err := NewProviderWithRepository("p", "m", false, &oneTurnRepo{turn: turn}).PredictStream(t.Context(), req)
	require.NoError(t, err)

	assert.Equal(t, []streamedChunk{
		{Delta: "Hel", Content: "Hel"},
		{Delta: "lo", Content: "Hello"},
		{Content: "Hello there", Finish: types.FinishReasonStop, OutputTokens: 2},
	}, collectStream(t, ch))
}

// Without scripted chunks the tool-calling stream still splits the reply 20
// bytes at a time.
func TestStreamChunks_UnscriptedReplySplitsTwentyBytesAtATime(t *testing.T) {
	const reply = "abcdefghijklmnopqrstuvwxyz0123456789ABCD" // 40 bytes: two whole chunks
	tp := NewToolProviderWithRepository("p", "m", false, &oneTurnRepo{turn: Turn{Type: "text", Content: reply}})
	ch, err := tp.PredictStreamWithTools(t.Context(),
		providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}, nil, "auto")
	require.NoError(t, err)

	assert.Equal(t, []streamedChunk{
		{Delta: "abcdefghijklmnopqrst", Content: "abcdefghijklmnopqrst"},
		{Delta: "uvwxyz0123456789ABCD", Content: reply},
		{Content: reply, Finish: types.FinishReasonStop, OutputTokens: 10},
	}, collectStream(t, ch))
}

// Without scripted chunks the plain stream sends the whole reply in one chunk.
func TestStreamChunks_UnscriptedPlainStreamIsOneChunk(t *testing.T) {
	ch, err := NewProviderWithRepository("p", "m", false,
		&oneTurnRepo{turn: Turn{Type: "text", Content: "Hello there"}}).PredictStream(t.Context(),
		providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}})
	require.NoError(t, err)

	assert.Equal(t, []streamedChunk{
		{Delta: "Hello there", Content: "Hello there", Finish: types.FinishReasonStop, OutputTokens: 2},
	}, collectStream(t, ch))
}

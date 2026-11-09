package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// convTopicEmbedder embeds a text by topic: apples, cars, or anything else.
type convTopicEmbedder struct{}

func (convTopicEmbedder) Embed(_ context.Context, req providers.EmbeddingRequest) (providers.EmbeddingResponse, error) {
	out := make([][]float32, len(req.Texts))
	for i, text := range req.Texts {
		switch t := strings.ToLower(text); {
		case strings.Contains(t, "apple"):
			out[i] = []float32{1, 0, 0}
		case strings.Contains(t, "car"):
			out[i] = []float32{0, 1, 0}
		default:
			out[i] = []float32{0, 0, 1}
		}
	}
	return providers.EmbeddingResponse{Embeddings: out}, nil
}
func (convTopicEmbedder) EmbeddingDimensions() int { return 3 }
func (convTopicEmbedder) MaxBatchSize() int        { return 16 }
func (convTopicEmbedder) ID() string               { return "conv-topic" }

// Retrieval looks for what the user is asking now: the turn about apples
// brings back the earlier apple message, not whatever matched the turn before.
func TestConv_RetrievalSearchesWithTheCurrentInput(t *testing.T) {
	ctx := context.Background()
	w := newConvWire(t, convWireReply{Text: "ok"})
	conv := openConv(t, w, WithStateStore(statestore.NewMemoryStore()), WithContextWindow(2),
		WithContextRetrieval(convTopicEmbedder{}, 1))
	for _, u := range []string{"I like apples", "my car is red", "the weather today"} {
		_, err := conv.Send(ctx, u)
		require.NoError(t, err)
	}

	_, err := conv.Send(ctx, "tell me more about apples")
	require.NoError(t, err)
	assert.Equal(t,
		[]string{"You help nobody.", "I like apples", "the weather today", "ok", "tell me more about apples"},
		convTexts(t, w.lastChat(t)))
}

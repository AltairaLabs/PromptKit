package compaction

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// recordingEmbedder records the texts of every Embed call and returns, for
// each text, a vector derived from the text alone: its length and first byte.
// short drops the last vector of a response, as a misbehaving provider might.
type recordingEmbedder struct {
	id    string
	fail  bool
	short bool

	mu    sync.Mutex
	calls [][]string
}

func (e *recordingEmbedder) Embed(_ context.Context, req providers.EmbeddingRequest) (providers.EmbeddingResponse, error) {
	e.mu.Lock()
	e.calls = append(e.calls, append([]string(nil), req.Texts...))
	e.mu.Unlock()
	if e.fail {
		return providers.EmbeddingResponse{}, errors.New("embedder down")
	}
	out := make([][]float32, 0, len(req.Texts))
	for _, text := range req.Texts {
		out = append(out, textVector(text))
	}
	if e.short {
		out = out[:len(out)-1]
	}
	return providers.EmbeddingResponse{Embeddings: out}, nil
}

func (e *recordingEmbedder) EmbeddingDimensions() int { return 2 }
func (e *recordingEmbedder) MaxBatchSize() int        { return 100 }
func (e *recordingEmbedder) ID() string               { return e.id }

func (e *recordingEmbedder) recorded() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([][]string(nil), e.calls...)
}

// modelEmbedder is a recordingEmbedder that also reports its model.
type modelEmbedder struct {
	*recordingEmbedder
	model string
}

func (e *modelEmbedder) Model() string { return e.model }

func textVector(text string) []float32 {
	first := float32(0)
	if text != "" {
		first = float32(text[0])
	}
	return []float32{float32(len(text)), first}
}

func TestEmbedAll_ReturnsOneVectorPerTextInOrder(t *testing.T) {
	p := &recordingEmbedder{id: "e"}

	got, err := EmbedAll(context.Background(), p, []string{"ab", "c"})

	require.NoError(t, err)
	assert.Equal(t, [][]float32{{2, 'a'}, {1, 'c'}}, got)
	assert.Equal(t, [][]string{{"ab", "c"}}, p.recorded(), "every text goes in one call")
}

func TestEmbedAll_WrapsTheProviderError(t *testing.T) {
	_, err := EmbedAll(context.Background(), &recordingEmbedder{id: "e", fail: true}, []string{"a"})

	require.Error(t, err)
	assert.Equal(t, "embedding generation failed: embedder down", err.Error())
}

func TestEmbedAll_RejectsAResponseMissingAVector(t *testing.T) {
	_, err := EmbedAll(context.Background(), &recordingEmbedder{id: "e", short: true}, []string{"a", "b"})

	require.Error(t, err)
	assert.Equal(t, "embedding count mismatch: expected 2, got 1", err.Error())
}

// The cache embeds each distinct text it does not hold once, returns a vector
// for every text in order, and keeps only the vectors of its latest call.
func TestEmbeddingCache_EmbedsOnlyWhatItDoesNotHold(t *testing.T) {
	p := &recordingEmbedder{id: "e"}
	var c EmbeddingCache

	first, err := c.Embed(context.Background(), p, []string{"aa", "b", "aa"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{2, 'a'}, {1, 'b'}, {2, 'a'}}, first)
	assert.Equal(t, 2, c.Len(), "one vector per distinct text")

	second, err := c.Embed(context.Background(), p, []string{"b", "ccc"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 'b'}, {3, 'c'}}, second)
	assert.Equal(t, 2, c.Len(), "the vector for a text the call no longer holds is released")

	third, err := c.Embed(context.Background(), p, []string{"ccc", "b"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{3, 'c'}, {1, 'b'}}, third)

	assert.Equal(t, [][]string{{"aa", "b"}, {"ccc"}}, p.recorded(),
		"a duplicate is embedded once, a cached text never again, and a fully cached call makes no Embed call")
}

func TestEmbeddingCache_AnErrorLeavesTheCacheAsItWas(t *testing.T) {
	p := &recordingEmbedder{id: "e"}
	var c EmbeddingCache
	_, err := c.Embed(context.Background(), p, []string{"a", "b"})
	require.NoError(t, err)

	p.fail = true
	got, err := c.Embed(context.Background(), p, []string{"a", "new"})
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Equal(t, 2, c.Len())

	p.fail = false
	again, err := c.Embed(context.Background(), p, []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 'a'}, {1, 'b'}}, again)
	assert.Equal(t, [][]string{{"a", "b"}, {"new"}}, p.recorded(), "the cached vectors survived the failed call")
}

// A vector is keyed by what produced it: another provider, or the same
// provider with another model, embeds the text again.
func TestEmbeddingCache_KeyIncludesProviderAndModel(t *testing.T) {
	base := &recordingEmbedder{id: "e"}
	small := &modelEmbedder{recordingEmbedder: base, model: "small"}
	large := &modelEmbedder{recordingEmbedder: base, model: "large"}
	other := &recordingEmbedder{id: "other"}
	var c EmbeddingCache

	for _, p := range []providers.EmbeddingProvider{small, small, large, base} {
		_, err := c.Embed(context.Background(), p, []string{"x"})
		require.NoError(t, err)
	}
	_, err := c.Embed(context.Background(), other, []string{"x"})
	require.NoError(t, err)

	assert.Equal(t, [][]string{{"x"}, {"x"}, {"x"}}, base.recorded(),
		"small is embedded once; large and the model-less provider each again")
	assert.Equal(t, [][]string{{"x"}}, other.recorded())
	assert.Equal(t, "e\x00small", embeddingIdentity(small))
	assert.Equal(t, "e", embeddingIdentity(base))
}

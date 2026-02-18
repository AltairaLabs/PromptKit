package statestore

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmbeddingProvider returns pre-configured vectors for known text strings.
type mockEmbeddingProvider struct {
	vectors map[string][]float32
}

func newMockEmbeddingProvider() *mockEmbeddingProvider {
	return &mockEmbeddingProvider{
		vectors: map[string][]float32{
			"hello":        {1, 0, 0},
			"hi there":     {0.9, 0.1, 0},
			"goodbye":      {0, 1, 0},
			"farewell":     {0.1, 0.9, 0},
			"weather":      {0, 0, 1},
			"sunny day":    {0.1, 0, 0.9},
			"how are you":  {0.8, 0.2, 0},
			"search hello": {1, 0, 0},
		},
	}
}

func (m *mockEmbeddingProvider) Embed(_ context.Context, req providers.EmbeddingRequest) (providers.EmbeddingResponse, error) {
	embeddings := make([][]float32, len(req.Texts))
	for i, text := range req.Texts {
		if vec, ok := m.vectors[text]; ok {
			embeddings[i] = vec
		} else {
			// Default vector for unknown text
			embeddings[i] = []float32{0.33, 0.33, 0.33}
		}
	}
	return providers.EmbeddingResponse{
		Embeddings: embeddings,
		Model:      "mock-embedding",
	}, nil
}

func (m *mockEmbeddingProvider) EmbeddingDimensions() int {
	return 3
}

func (m *mockEmbeddingProvider) MaxBatchSize() int {
	return 100
}

func (m *mockEmbeddingProvider) ID() string {
	return "mock-embedding"
}

func TestInMemoryIndex_IndexAndSearch(t *testing.T) {
	provider := newMockEmbeddingProvider()
	idx := NewInMemoryIndex(provider)
	ctx := context.Background()

	// Index several messages
	messages := []struct {
		turnIndex int
		content   string
	}{
		{0, "hello"},
		{1, "goodbye"},
		{2, "weather"},
		{3, "hi there"},
	}

	for _, m := range messages {
		err := idx.Index(ctx, "conv-1", m.turnIndex, types.Message{
			Role:    "user",
			Content: m.content,
		})
		require.NoError(t, err)
	}

	// Search for "search hello" which maps to [1,0,0], same as "hello"
	results, err := idx.Search(ctx, "conv-1", "search hello", 3)
	require.NoError(t, err)
	require.NotNil(t, results)
	require.True(t, len(results) > 0)

	// Top result should be "hello" since it has the exact same vector [1,0,0]
	assert.Equal(t, "hello", results[0].Message.Content)
	assert.Equal(t, 0, results[0].TurnIndex)
	assert.InDelta(t, 1.0, results[0].Score, 0.001)

	// Second result should be "hi there" [0.9,0.1,0] — very similar to [1,0,0]
	assert.Equal(t, "hi there", results[1].Message.Content)
}

func TestInMemoryIndex_SearchEmptyIndex(t *testing.T) {
	provider := newMockEmbeddingProvider()
	idx := NewInMemoryIndex(provider)
	ctx := context.Background()

	results, err := idx.Search(ctx, "conv-1", "hello", 5)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestInMemoryIndex_SearchEmptyQuery(t *testing.T) {
	provider := newMockEmbeddingProvider()
	idx := NewInMemoryIndex(provider)
	ctx := context.Background()

	// Index a message first
	err := idx.Index(ctx, "conv-1", 0, types.Message{Role: "user", Content: "hello"})
	require.NoError(t, err)

	results, err := idx.Search(ctx, "conv-1", "", 5)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestInMemoryIndex_SearchKZero(t *testing.T) {
	provider := newMockEmbeddingProvider()
	idx := NewInMemoryIndex(provider)
	ctx := context.Background()

	// Index a message first
	err := idx.Index(ctx, "conv-1", 0, types.Message{Role: "user", Content: "hello"})
	require.NoError(t, err)

	results, err := idx.Search(ctx, "conv-1", "hello", 0)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestInMemoryIndex_Delete(t *testing.T) {
	provider := newMockEmbeddingProvider()
	idx := NewInMemoryIndex(provider)
	ctx := context.Background()

	// Index messages
	err := idx.Index(ctx, "conv-1", 0, types.Message{Role: "user", Content: "hello"})
	require.NoError(t, err)
	err = idx.Index(ctx, "conv-1", 1, types.Message{Role: "user", Content: "goodbye"})
	require.NoError(t, err)

	// Verify search returns results before delete
	results, err := idx.Search(ctx, "conv-1", "hello", 5)
	require.NoError(t, err)
	require.NotNil(t, results)

	// Delete the conversation index
	err = idx.Delete(ctx, "conv-1")
	require.NoError(t, err)

	// Search should return nil after delete
	results, err = idx.Search(ctx, "conv-1", "hello", 5)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestInMemoryIndex_SearchTopK(t *testing.T) {
	provider := newMockEmbeddingProvider()
	idx := NewInMemoryIndex(provider)
	ctx := context.Background()

	// Index 5 messages
	texts := []string{"hello", "hi there", "goodbye", "farewell", "weather"}
	for i, text := range texts {
		err := idx.Index(ctx, "conv-1", i, types.Message{Role: "user", Content: text})
		require.NoError(t, err)
	}

	// Search with k=2
	results, err := idx.Search(ctx, "conv-1", "hello", 2)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Results should be sorted by score descending
	assert.GreaterOrEqual(t, results[0].Score, results[1].Score)
}

func TestInMemoryIndex_IndexEmptyContent(t *testing.T) {
	provider := newMockEmbeddingProvider()
	idx := NewInMemoryIndex(provider)
	ctx := context.Background()

	// Index a message with empty content — should be a no-op
	err := idx.Index(ctx, "conv-1", 0, types.Message{Role: "user", Content: ""})
	require.NoError(t, err)

	// Search should return nil since nothing was indexed
	results, err := idx.Search(ctx, "conv-1", "hello", 5)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestInMemoryIndex_MultipleConversations(t *testing.T) {
	provider := newMockEmbeddingProvider()
	idx := NewInMemoryIndex(provider)
	ctx := context.Background()

	// Index messages in conversation 1
	err := idx.Index(ctx, "conv-1", 0, types.Message{Role: "user", Content: "hello"})
	require.NoError(t, err)
	err = idx.Index(ctx, "conv-1", 1, types.Message{Role: "user", Content: "hi there"})
	require.NoError(t, err)

	// Index messages in conversation 2
	err = idx.Index(ctx, "conv-2", 0, types.Message{Role: "user", Content: "goodbye"})
	require.NoError(t, err)
	err = idx.Index(ctx, "conv-2", 1, types.Message{Role: "user", Content: "farewell"})
	require.NoError(t, err)

	// Search in conv-1 should only return conv-1 messages
	results, err := idx.Search(ctx, "conv-1", "hello", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, r := range results {
		assert.True(t, r.Message.Content == "hello" || r.Message.Content == "hi there",
			"unexpected message from different conversation: %s", r.Message.Content)
	}

	// Search in conv-2 should only return conv-2 messages
	results, err = idx.Search(ctx, "conv-2", "goodbye", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, r := range results {
		assert.True(t, r.Message.Content == "goodbye" || r.Message.Content == "farewell",
			"unexpected message from different conversation: %s", r.Message.Content)
	}

	// Search in non-existent conversation should return nil
	results, err = idx.Search(ctx, "conv-3", "hello", 10)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestCosineSimilarity32(t *testing.T) {
	// Identical vectors should have similarity of 1.0
	t.Run("identical_vectors", func(t *testing.T) {
		a := []float32{1, 2, 3}
		b := []float32{1, 2, 3}
		sim := cosineSimilarity32(a, b)
		assert.InDelta(t, 1.0, sim, 0.0001)
	})

	// Orthogonal vectors should have similarity of 0.0
	t.Run("orthogonal_vectors", func(t *testing.T) {
		a := []float32{1, 0, 0}
		b := []float32{0, 1, 0}
		sim := cosineSimilarity32(a, b)
		assert.InDelta(t, 0.0, sim, 0.0001)
	})

	// Mismatched lengths should return 0.0
	t.Run("mismatched_lengths", func(t *testing.T) {
		a := []float32{1, 0}
		b := []float32{1, 0, 0}
		sim := cosineSimilarity32(a, b)
		assert.Equal(t, 0.0, sim)
	})

	// Empty vectors should return 0.0
	t.Run("empty_vectors", func(t *testing.T) {
		var a, b []float32
		sim := cosineSimilarity32(a, b)
		assert.Equal(t, 0.0, sim)
	})
}

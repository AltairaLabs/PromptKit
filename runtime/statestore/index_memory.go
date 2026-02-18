package statestore

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// messageEmbedding stores a message with its embedding vector.
type messageEmbedding struct {
	turnIndex int
	message   types.Message
	vector    []float32
}

// InMemoryIndex provides an in-memory implementation of MessageIndex using
// brute-force cosine similarity search. Suitable for development, testing,
// and conversations with up to ~10K messages.
type InMemoryIndex struct {
	mu       sync.RWMutex
	provider providers.EmbeddingProvider
	entries  map[string][]messageEmbedding // conversationID → embeddings
}

// NewInMemoryIndex creates a new in-memory message index.
func NewInMemoryIndex(provider providers.EmbeddingProvider) *InMemoryIndex {
	return &InMemoryIndex{
		provider: provider,
		entries:  make(map[string][]messageEmbedding),
	}
}

// Index adds a message to the search index by computing its embedding.
//
//nolint:gocritic // message passed by value to match MessageIndex interface
func (idx *InMemoryIndex) Index(
	ctx context.Context, conversationID string, turnIndex int, message types.Message,
) error {
	// Only index messages with text content
	text := message.Content
	if text == "" {
		return nil
	}

	resp, err := idx.provider.Embed(ctx, providers.EmbeddingRequest{
		Texts: []string{text},
	})
	if err != nil {
		return err
	}

	if len(resp.Embeddings) == 0 {
		return nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.entries[conversationID] = append(idx.entries[conversationID], messageEmbedding{
		turnIndex: turnIndex,
		message:   message,
		vector:    resp.Embeddings[0],
	})

	return nil
}

// Search finds the top-k messages most relevant to the query string.
func (idx *InMemoryIndex) Search(ctx context.Context, conversationID, query string, k int) ([]IndexResult, error) {
	if query == "" || k <= 0 {
		return nil, nil
	}

	// Embed the query
	resp, err := idx.provider.Embed(ctx, providers.EmbeddingRequest{
		Texts: []string{query},
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Embeddings) == 0 {
		return nil, nil
	}

	queryVec := resp.Embeddings[0]

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	entries, exists := idx.entries[conversationID]
	if !exists || len(entries) == 0 {
		return nil, nil
	}

	// Compute cosine similarity for each entry
	type scored struct {
		entry messageEmbedding
		score float64
	}

	scores := make([]scored, 0, len(entries))
	for i := range entries {
		sim := cosineSimilarity32(queryVec, entries[i].vector)
		scores = append(scores, scored{entry: entries[i], score: sim})
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Take top-k
	if k > len(scores) {
		k = len(scores)
	}

	results := make([]IndexResult, k)
	for i := 0; i < k; i++ {
		results[i] = IndexResult{
			TurnIndex: scores[i].entry.turnIndex,
			Message:   scores[i].entry.message,
			Score:     scores[i].score,
		}
	}

	return results, nil
}

// Delete removes all indexed messages for a conversation.
func (idx *InMemoryIndex) Delete(_ context.Context, conversationID string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	delete(idx.entries, conversationID)
	return nil
}

// cosineSimilarity32 computes cosine similarity between two float32 vectors.
func cosineSimilarity32(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	denominator := math.Sqrt(normA) * math.Sqrt(normB)
	if denominator == 0 {
		return 0.0
	}

	return dotProduct / denominator
}

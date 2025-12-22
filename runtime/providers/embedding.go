// Package providers implements multi-LLM provider support with unified interfaces.
package providers

import "context"

// EmbeddingRequest represents a request for text embeddings.
type EmbeddingRequest struct {
	// Texts to embed (batched for efficiency)
	Texts []string

	// Model override for embedding model (optional, uses provider default if empty)
	Model string
}

// EmbeddingResponse contains the embedding vectors from a provider.
type EmbeddingResponse struct {
	// Embeddings contains one vector per input text, in the same order
	Embeddings [][]float32

	// Model is the model that was used for embedding
	Model string

	// Usage contains token consumption information (optional)
	Usage *EmbeddingUsage
}

// EmbeddingUsage tracks token consumption for embedding requests.
type EmbeddingUsage struct {
	// TotalTokens is the total number of tokens processed
	TotalTokens int
}

// EmbeddingProvider generates text embeddings for semantic similarity operations.
// Implementations exist for OpenAI, Gemini, and other embedding APIs.
//
// Embeddings are dense vector representations of text that capture semantic meaning.
// Similar texts will have embeddings with high cosine similarity scores.
//
// Example usage:
//
//	provider, _ := openai.NewEmbeddingProvider()
//	resp, err := provider.Embed(ctx, providers.EmbeddingRequest{
//	    Texts: []string{"Hello world", "Hi there"},
//	})
//	similarity := CosineSimilarity(resp.Embeddings[0], resp.Embeddings[1])
type EmbeddingProvider interface {
	// Embed generates embeddings for the given texts.
	// The response contains one embedding vector per input text, in the same order.
	// Implementations should handle batching internally if the request exceeds MaxBatchSize.
	Embed(ctx context.Context, req EmbeddingRequest) (EmbeddingResponse, error)

	// EmbeddingDimensions returns the dimensionality of embedding vectors.
	// Common values: 1536 (OpenAI ada-002/3-small), 768 (Gemini), 3072 (OpenAI 3-large)
	EmbeddingDimensions() int

	// MaxBatchSize returns the maximum number of texts per single API request.
	// Callers should batch requests appropriately, or rely on the provider
	// to handle splitting internally.
	MaxBatchSize() int

	// ID returns the provider identifier (e.g., "openai-embedding", "gemini-embedding")
	ID() string
}

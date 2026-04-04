//go:build integration

package ollama

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func ollamaURL() string {
	if url := os.Getenv("OLLAMA_URL"); url != "" {
		return url
	}
	return "http://localhost:11434"
}

func skipIfOllamaUnavailable(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ollamaURL()+"/api/tags", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("Ollama not available at %s: %v", ollamaURL(), err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("Ollama not healthy at %s: status %d", ollamaURL(), resp.StatusCode)
	}
}

func TestIntegration_OllamaEmbedding_SingleText(t *testing.T) {
	skipIfOllamaUnavailable(t)

	p := NewEmbeddingProvider(WithEmbeddingBaseURL(ollamaURL()))

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"The quick brown fox jumps over the lazy dog"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 1)

	// nomic-embed-text produces 768-dimensional vectors
	assert.Equal(t, p.EmbeddingDimensions(), len(resp.Embeddings[0]),
		"embedding dimensions should match provider config")
	assert.NotEmpty(t, resp.Model)

	// Sanity: vector should have non-zero values
	hasNonZero := false
	for _, v := range resp.Embeddings[0] {
		if v != 0 {
			hasNonZero = true
			break
		}
	}
	assert.True(t, hasNonZero, "embedding should have non-zero values")
}

func TestIntegration_OllamaEmbedding_BatchTexts(t *testing.T) {
	skipIfOllamaUnavailable(t)

	p := NewEmbeddingProvider(WithEmbeddingBaseURL(ollamaURL()))

	texts := []string{
		"Machine learning is a subset of artificial intelligence",
		"The weather today is sunny and warm",
		"Go is a statically typed programming language",
	}

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: texts})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 3)

	// All vectors should have the same dimensionality
	for i, emb := range resp.Embeddings {
		assert.Len(t, emb, p.EmbeddingDimensions(), "embedding %d dimensions mismatch", i)
	}
}

func TestIntegration_OllamaEmbedding_SemanticSimilarity(t *testing.T) {
	skipIfOllamaUnavailable(t)

	p := NewEmbeddingProvider(WithEmbeddingBaseURL(ollamaURL()))

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{
			"I love programming in Go",      // [0] similar to [1]
			"Go is my favourite language",   // [1] similar to [0]
			"The recipe calls for two eggs", // [2] unrelated
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 3)

	simGoGo := cosineSimilarity(resp.Embeddings[0], resp.Embeddings[1])
	simGoEgg := cosineSimilarity(resp.Embeddings[0], resp.Embeddings[2])

	t.Logf("similarity(Go, Go) = %.4f", simGoGo)
	t.Logf("similarity(Go, eggs) = %.4f", simGoEgg)

	assert.Greater(t, simGoGo, simGoEgg,
		"semantically similar texts should have higher cosine similarity")
	assert.Greater(t, simGoGo, float32(0.5),
		"similar texts should have similarity > 0.5")
}

func TestIntegration_OllamaEmbedding_EmptyRequest(t *testing.T) {
	skipIfOllamaUnavailable(t)

	p := NewEmbeddingProvider(WithEmbeddingBaseURL(ollamaURL()))

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{}})
	require.NoError(t, err)
	assert.Empty(t, resp.Embeddings)
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt32(normA) * sqrt32(normB))
}

func sqrt32(x float32) float32 {
	// Newton's method — good enough for similarity checks
	if x <= 0 {
		return 0
	}
	z := x
	for range 10 {
		z = (z + x/z) / 2
	}
	return z
}

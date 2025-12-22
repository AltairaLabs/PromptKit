package providers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewBaseEmbeddingProvider(t *testing.T) {
	p := NewBaseEmbeddingProvider(
		"test-provider",
		"test-model",
		"https://api.test.com",
		1024,
		100,
		30*time.Second,
	)

	assert.Equal(t, "test-provider", p.ID())
	assert.Equal(t, "test-model", p.Model())
	assert.Equal(t, "https://api.test.com", p.BaseURL)
	assert.Equal(t, 1024, p.EmbeddingDimensions())
	assert.Equal(t, 100, p.MaxBatchSize())
	assert.NotNil(t, p.HTTPClient)
}

func TestBaseEmbeddingProvider_EmptyResponseForModel(t *testing.T) {
	p := NewBaseEmbeddingProvider("test", "default-model", "", 1024, 100, time.Second)

	t.Run("uses provided model", func(t *testing.T) {
		resp := p.EmptyResponseForModel("custom-model")
		assert.Empty(t, resp.Embeddings)
		assert.Equal(t, "custom-model", resp.Model)
	})

	t.Run("uses default model when empty", func(t *testing.T) {
		resp := p.EmptyResponseForModel("")
		assert.Empty(t, resp.Embeddings)
		assert.Equal(t, "default-model", resp.Model)
	})
}

func TestBaseEmbeddingProvider_ResolveModel(t *testing.T) {
	p := NewBaseEmbeddingProvider("test", "default-model", "", 1024, 100, time.Second)

	t.Run("returns request model when provided", func(t *testing.T) {
		model := p.ResolveModel("custom-model")
		assert.Equal(t, "custom-model", model)
	})

	t.Run("returns default model when empty", func(t *testing.T) {
		model := p.ResolveModel("")
		assert.Equal(t, "default-model", model)
	})
}

func TestBaseEmbeddingProvider_HandleEmptyRequest(t *testing.T) {
	p := NewBaseEmbeddingProvider("test", "test-model", "", 1024, 100, time.Second)

	t.Run("returns empty response for empty texts", func(t *testing.T) {
		resp, isEmpty := p.HandleEmptyRequest(EmbeddingRequest{Texts: []string{}})
		assert.True(t, isEmpty)
		assert.Empty(t, resp.Embeddings)
		assert.Equal(t, "test-model", resp.Model)
	})

	t.Run("returns false for non-empty texts", func(t *testing.T) {
		_, isEmpty := p.HandleEmptyRequest(EmbeddingRequest{Texts: []string{"hello"}})
		assert.False(t, isEmpty)
	})
}

func TestBaseEmbeddingProvider_EmbedWithEmptyCheck(t *testing.T) {
	p := NewBaseEmbeddingProvider("test", "test-model", "", 1024, 100, time.Second)
	ctx := context.Background()

	t.Run("handles empty request", func(t *testing.T) {
		called := false
		embedFn := func(_ context.Context, _ []string, _ string) (EmbeddingResponse, error) {
			called = true
			return EmbeddingResponse{}, nil
		}

		resp, err := p.EmbedWithEmptyCheck(ctx, EmbeddingRequest{Texts: []string{}}, embedFn)
		assert.NoError(t, err)
		assert.Empty(t, resp.Embeddings)
		assert.False(t, called)
	})

	t.Run("calls embed function for non-empty request", func(t *testing.T) {
		called := false
		expectedTexts := []string{"hello", "world"}
		embedFn := func(_ context.Context, texts []string, model string) (EmbeddingResponse, error) {
			called = true
			assert.Equal(t, expectedTexts, texts)
			assert.Equal(t, "test-model", model)
			return EmbeddingResponse{
				Embeddings: [][]float32{{0.1, 0.2}, {0.3, 0.4}},
				Model:      model,
			}, nil
		}

		resp, err := p.EmbedWithEmptyCheck(ctx, EmbeddingRequest{Texts: expectedTexts}, embedFn)
		assert.NoError(t, err)
		assert.True(t, called)
		assert.Len(t, resp.Embeddings, 2)
	})

	t.Run("uses request model when provided", func(t *testing.T) {
		embedFn := func(_ context.Context, _ []string, model string) (EmbeddingResponse, error) {
			assert.Equal(t, "custom-model", model)
			return EmbeddingResponse{Model: model}, nil
		}

		resp, err := p.EmbedWithEmptyCheck(ctx, EmbeddingRequest{
			Texts: []string{"test"},
			Model: "custom-model",
		}, embedFn)
		assert.NoError(t, err)
		assert.Equal(t, "custom-model", resp.Model)
	})
}

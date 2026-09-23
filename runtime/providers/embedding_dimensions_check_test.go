package providers

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vectorsOfLen(count, n int) [][]float32 {
	out := make([][]float32, count)
	for i := range out {
		out[i] = make([]float32, n)
	}
	return out
}

func embedReturning(vecs [][]float32) EmbedFunc {
	return func(context.Context, []string, string) (EmbeddingResponse, error) {
		return EmbeddingResponse{Embeddings: vecs}, nil
	}
}

func TestCheckDimensions_UnknownSizeIsObservedFromFirstResponse(t *testing.T) {
	b := &BaseEmbeddingProvider{ProviderID: "p", ProviderModel: "m"}
	require.Equal(t, 0, b.EmbeddingDimensions(), "no size may be reported before one is known")

	_, err := b.EmbedWithEmptyCheck(context.Background(),
		EmbeddingRequest{Texts: []string{"a", "b"}}, embedReturning(vectorsOfLen(2, 384)))
	require.NoError(t, err)
	assert.Equal(t, 384, b.EmbeddingDimensions())

	_, err = b.EmbedWithEmptyCheck(context.Background(),
		EmbeddingRequest{Texts: []string{"c"}}, embedReturning(vectorsOfLen(1, 768)))
	require.Error(t, err, "a later response of a different size must not pass")
	assert.Contains(t, err.Error(), "returned 768-dimension embeddings, expected 384")
	assert.Equal(t, 384, b.EmbeddingDimensions())
}

func TestCheckDimensions_DeclaredSizeMismatchFails(t *testing.T) {
	b := &BaseEmbeddingProvider{ProviderID: "p", ProviderModel: "m", Dimensions: 1536}

	_, err := b.EmbedWithEmptyCheck(context.Background(),
		EmbeddingRequest{Texts: []string{"a"}}, embedReturning(vectorsOfLen(1, 768)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `model "m" returned 768-dimension embeddings, expected 1536`)
}

func TestCheckDimensions_DeclaredSizeMatchPasses(t *testing.T) {
	b := &BaseEmbeddingProvider{ProviderID: "p", ProviderModel: "m", Dimensions: 3}

	resp, err := b.EmbedWithEmptyCheck(context.Background(),
		EmbeddingRequest{Texts: []string{"a"}}, embedReturning([][]float32{{1, 2, 3}}))
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 2, 3}}, resp.Embeddings)
}

func TestCheckDimensions_MissingVectorFails(t *testing.T) {
	// ExtractOrderedEmbeddings leaves a nil slot when the server omits an
	// index; that must surface, not flow on as an empty vector.
	b := &BaseEmbeddingProvider{ProviderID: "p", ProviderModel: "m"}

	_, err := b.EmbedWithEmptyCheck(context.Background(),
		EmbeddingRequest{Texts: []string{"a", "b"}}, embedReturning([][]float32{{1, 2}, nil}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "embedding 1 is empty")
}

func TestCheckDimensions_RequestModelOverrideIsNotChecked(t *testing.T) {
	b := &BaseEmbeddingProvider{ProviderID: "p", ProviderModel: "m", Dimensions: 1536}

	resp, err := b.EmbedWithEmptyCheck(context.Background(),
		EmbeddingRequest{Texts: []string{"a"}, Model: "other"}, embedReturning(vectorsOfLen(1, 768)))
	require.NoError(t, err, "the configured size describes the configured model only")
	assert.Len(t, resp.Embeddings[0], 768)
	assert.Equal(t, 1536, b.EmbeddingDimensions())
}

func TestCheckDimensions_ConcurrentFirstResponsesAgree(t *testing.T) {
	b := &BaseEmbeddingProvider{ProviderID: "p", ProviderModel: "m"}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := b.EmbedWithEmptyCheck(context.Background(),
				EmbeddingRequest{Texts: []string{"a"}}, embedReturning(vectorsOfLen(1, 512)))
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.Equal(t, 512, b.EmbeddingDimensions())
}

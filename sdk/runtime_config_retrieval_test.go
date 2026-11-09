package sdk

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingEmbedder is the mock embedding provider behind the YAML-only
// retrieval test: it returns a fixed vector and counts calls.
type countingEmbedder struct{ calls atomic.Int64 }

func (e *countingEmbedder) Embed(_ context.Context, req providers.EmbeddingRequest) (providers.EmbeddingResponse, error) {
	e.calls.Add(1)
	vecs := make([][]float32, len(req.Texts))
	for i := range vecs {
		vecs[i] = []float32{1, 0, 0}
	}
	return providers.EmbeddingResponse{Embeddings: vecs}, nil
}
func (e *countingEmbedder) EmbeddingDimensions() int { return 3 }
func (e *countingEmbedder) MaxBatchSize() int        { return 16 }
func (e *countingEmbedder) ID() string               { return "retrieval-mock" }

const retrievalRuntimeYAML = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: test
spec:
  state_store:
    type: memory
  providers:
    - id: emb
      type: retrievalmock
      role: embedding
`

// A YAML-declared embedding provider must enable retrieval on its own: the
// only programmatic input is the context window, which a runtime config has no
// field for (#2338). Retrieval is observable as an older message, outside the
// hot window, reaching the provider's request.
func TestOpen_YAMLDeclaredRetrievalProviderEnablesRetrieval(t *testing.T) {
	emb := &countingEmbedder{}
	providers.RegisterEmbeddingProviderFactory("retrievalmock",
		func(_ providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) { return emb, nil })

	w := newConvWire(t, convWireReply{Echo: true})
	conv := openConv(t, w, WithRuntimeConfig(writeRuntimeConfig(t, retrievalRuntimeYAML)), WithContextWindow(2))
	assert.Equal(t, defaultRetrievalTopK, conv.config.retrievalTopK, "declared provider gets the default top-k")
	for _, msg := range []string{"one", "two", "three", "four"} {
		_, err := conv.Send(context.Background(), msg)
		require.NoError(t, err)
	}

	var texts []string
	for _, m := range w.lastChat(t).messages(t) {
		texts = append(texts, m.Text)
	}
	assert.Subset(t, texts, []string{"three", "echo:three", "four"}, "the hot window")
	assert.Contains(t, texts, "one", "retrieval must bring back a message from before the hot window: %v", texts)
}

func TestOpen_YAMLDeclaredRetrievalProviderNamesMissingOptions(t *testing.T) {
	providers.RegisterEmbeddingProviderFactory("retrievalmock",
		func(_ providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			return &countingEmbedder{}, nil
		})

	packPath := createTestPackFile(t, testPackJSON)
	rcPath := writeRuntimeConfig(t, retrievalRuntimeYAML)

	_, err := Open(packPath, "chat", WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)), WithRuntimeConfig(rcPath))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithContextWindow")
	assert.NotContains(t, err.Error(), "WithStateStore", "the runtime config supplies the state store")
}

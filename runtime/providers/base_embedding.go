package providers

import (
	"context"
	"net/http"
	"time"
)

// BaseEmbeddingProvider provides common functionality for embedding providers.
// Embed this struct in provider-specific implementations to reduce duplication.
type BaseEmbeddingProvider struct {
	ProviderModel string
	BaseURL       string
	APIKey        string
	HTTPClient    *http.Client
	Dimensions    int
	ProviderID    string
	BatchSize     int
}

// NewBaseEmbeddingProvider creates a base embedding provider with defaults.
func NewBaseEmbeddingProvider(
	providerID, defaultModel, defaultBaseURL string,
	defaultDimensions, defaultBatchSize int,
	defaultTimeout time.Duration,
) *BaseEmbeddingProvider {
	return &BaseEmbeddingProvider{
		ProviderID:    providerID,
		ProviderModel: defaultModel,
		BaseURL:       defaultBaseURL,
		Dimensions:    defaultDimensions,
		BatchSize:     defaultBatchSize,
		HTTPClient:    &http.Client{Timeout: defaultTimeout},
	}
}

// ID returns the provider identifier.
func (b *BaseEmbeddingProvider) ID() string {
	return b.ProviderID
}

// Model returns the current embedding model.
func (b *BaseEmbeddingProvider) Model() string {
	return b.ProviderModel
}

// EmbeddingDimensions returns the dimensionality of embedding vectors.
func (b *BaseEmbeddingProvider) EmbeddingDimensions() int {
	return b.Dimensions
}

// MaxBatchSize returns the maximum texts per single API request.
func (b *BaseEmbeddingProvider) MaxBatchSize() int {
	return b.BatchSize
}

// EmptyResponseForModel returns an empty EmbeddingResponse with the given model.
// Use this for handling empty input cases.
func (b *BaseEmbeddingProvider) EmptyResponseForModel(model string) EmbeddingResponse {
	if model == "" {
		model = b.ProviderModel
	}
	return EmbeddingResponse{
		Embeddings: [][]float32{},
		Model:      model,
	}
}

// ResolveModel returns the model to use, preferring the request model over the default.
func (b *BaseEmbeddingProvider) ResolveModel(reqModel string) string {
	if reqModel != "" {
		return reqModel
	}
	return b.ProviderModel
}

// HandleEmptyRequest checks if the request has no texts and returns early if so.
// Returns (response, true) if empty, (zero, false) if not empty.
func (b *BaseEmbeddingProvider) HandleEmptyRequest(
	req EmbeddingRequest,
) (EmbeddingResponse, bool) {
	if len(req.Texts) == 0 {
		return b.EmptyResponseForModel(b.ProviderModel), true
	}
	return EmbeddingResponse{}, false
}

// EmbedFunc is the signature for provider-specific embedding logic.
type EmbedFunc func(ctx context.Context, texts []string, model string) (EmbeddingResponse, error)

// EmbedWithEmptyCheck wraps embedding logic with empty request handling.
func (b *BaseEmbeddingProvider) EmbedWithEmptyCheck(
	ctx context.Context,
	req EmbeddingRequest,
	embedFn EmbedFunc,
) (EmbeddingResponse, error) {
	if resp, isEmpty := b.HandleEmptyRequest(req); isEmpty {
		return resp, nil
	}
	model := b.ResolveModel(req.Model)
	return embedFn(ctx, req.Texts, model)
}

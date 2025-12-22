package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// Common HTTP constants for embedding providers.
const (
	ContentTypeHeader   = "Content-Type"
	AuthorizationHeader = "Authorization"
	ApplicationJSON     = "application/json"
	BearerPrefix        = "Bearer "
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

// HTTPRequestConfig configures how to make an HTTP request.
type HTTPRequestConfig struct {
	URL         string
	Body        []byte
	UseAPIKey   bool   // If true, adds Authorization: Bearer <APIKey> header
	ContentType string // Defaults to application/json
}

// DoEmbeddingRequest performs a common HTTP POST request for embeddings.
// Returns the response body and any error.
func (b *BaseEmbeddingProvider) DoEmbeddingRequest(
	ctx context.Context,
	cfg HTTPRequestConfig,
) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, cfg.URL, bytes.NewReader(cfg.Body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	contentType := cfg.ContentType
	if contentType == "" {
		contentType = ApplicationJSON
	}
	httpReq.Header.Set(ContentTypeHeader, contentType)

	if cfg.UseAPIKey && b.APIKey != "" {
		httpReq.Header.Set(AuthorizationHeader, BearerPrefix+b.APIKey)
	}

	resp, err := b.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API error (status %d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// ExtractOrderedEmbeddings extracts embeddings from indexed response data
// and places them in the correct order. Returns an error if count doesn't match.
func ExtractOrderedEmbeddings[T any](
	data []T,
	getIndex func(T) int,
	getEmbedding func(T) []float32,
	expectedCount int,
) ([][]float32, error) {
	embeddings := make([][]float32, expectedCount)
	for _, item := range data {
		idx := getIndex(item)
		if idx >= 0 && idx < expectedCount {
			embeddings[idx] = getEmbedding(item)
		}
	}
	return embeddings, nil
}

// MarshalRequest marshals a request body to JSON with standardized error handling.
func MarshalRequest(req any) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	return body, nil
}

// UnmarshalResponse unmarshals a response body from JSON with standardized error handling.
func UnmarshalResponse(body []byte, resp any) error {
	if err := json.Unmarshal(body, resp); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return nil
}

// LogEmbeddingRequest logs a completed embedding request with common fields.
func LogEmbeddingRequest(provider, model string, textCount int, start time.Time) {
	logger.Debug(provider+" embedding request completed",
		"model", model,
		"texts", textCount,
		"latency_ms", time.Since(start).Milliseconds(),
	)
}

// LogEmbeddingRequestWithTokens logs a completed embedding request with token count.
func LogEmbeddingRequestWithTokens(provider, model string, textCount, tokens int, start time.Time) {
	logger.Debug(provider+" embedding request completed",
		"model", model,
		"texts", textCount,
		"tokens", tokens,
		"latency_ms", time.Since(start).Milliseconds(),
	)
}

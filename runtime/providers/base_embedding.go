package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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
	// Dimensions is the vector length the configured model produces: the
	// size the caller declared, else the size of a model the provider knows.
	// It is 0 when neither applies — the provider then takes the length of
	// the first vector it gets back rather than guessing one.
	Dimensions int
	ProviderID string
	BatchSize  int
	// PlatformAuth indicates the HTTPClient's transport applies
	// hyperscaler-platform auth (Azure Bearer, etc.) per request, so the
	// per-provider empty-API-key guard must be skipped.
	PlatformAuth bool

	// observedDims is the vector length seen on the first response when
	// Dimensions is 0. Atomic because Embed may run concurrently.
	observedDims atomic.Int64
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

// EmbeddingDimensions returns the dimensionality of embedding vectors: the
// declared or known size, else the size observed on the first response, else
// 0 when neither is available yet.
func (b *BaseEmbeddingProvider) EmbeddingDimensions() int {
	if b.Dimensions > 0 {
		return b.Dimensions
	}
	return int(b.observedDims.Load())
}

// CheckDimensions verifies every vector has the length EmbeddingDimensions
// reports, recording the first length seen when no size is known yet. A
// mismatch is an error: a caller that sized storage from EmbeddingDimensions
// would otherwise fail later, far from the cause.
func (b *BaseEmbeddingProvider) CheckDimensions(embeddings [][]float32) error {
	for i, v := range embeddings {
		if len(v) == 0 {
			return fmt.Errorf("%s: embedding %d is empty", b.ProviderID, i)
		}
		want := b.EmbeddingDimensions()
		if want == 0 {
			b.observedDims.CompareAndSwap(0, int64(len(v)))
			want = b.EmbeddingDimensions()
		}
		if len(v) != want {
			return fmt.Errorf(
				"%s: model %q returned %d-dimension embeddings, expected %d; "+
					"set dimensions to the size the model actually produces",
				b.ProviderID, b.ProviderModel, len(v), want)
		}
	}
	return nil
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
	resp, err := embedFn(ctx, req.Texts, model)
	if err != nil {
		return resp, err
	}
	// Dimensions describes the configured model; a per-request model override
	// may legitimately produce a different size.
	if model == b.ProviderModel {
		if err := b.CheckDimensions(resp.Embeddings); err != nil {
			return EmbeddingResponse{}, err
		}
	}
	return resp, nil
}

// HTTPRequestConfig configures how to make an HTTP request.
type HTTPRequestConfig struct {
	URL         string
	Body        []byte
	UseAPIKey   bool   // If true, adds Authorization: Bearer <APIKey> header
	ContentType string // Defaults to application/json
	// Headers are set on the request before it is sent. Providers that
	// authenticate with a non-Bearer scheme use this — notably Gemini, whose
	// key goes in x-goog-api-key so it never enters the URL and therefore
	// never reaches a log through *url.Error.
	Headers map[string]string
}

// DoEmbeddingRequest performs a common HTTP POST request for embeddings.
// Returns the response body and any error.
func (b *BaseEmbeddingProvider) DoEmbeddingRequest(
	ctx context.Context,
	cfg HTTPRequestConfig,
) ([]byte, error) {
	return DoAncillaryJSONRequest(ctx, b.HTTPClient, b.ProviderID, b.APIKey, cfg)
}

// DoAncillaryJSONRequest POSTs a JSON body for one of the ancillary provider
// roles (embedding, rerank) and returns the raw response body.
//
// Shared by both rather than copied, because the error handling is the part
// worth getting right once: a transport failure is wrapped as
// ProviderTransportError, not a bare fmt.Errorf, because that is the type
// whose Error() redacts credential-bearing query parameters. A plain wrap
// formats the raw *url.Error — full URL included — straight into the message,
// which is how a live key once reached the logs. It also makes these
// failures classifiable by IsTransient, like every other provider path.
func DoAncillaryJSONRequest(
	ctx context.Context,
	client *http.Client,
	providerID, apiKey string,
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

	for k, v := range cfg.Headers {
		httpReq.Header.Set(k, v)
	}

	if cfg.UseAPIKey && apiKey != "" {
		httpReq.Header.Set(AuthorizationHeader, BearerPrefix+apiKey)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &ProviderTransportError{Cause: err, Provider: providerID}
	}
	defer resp.Body.Close()

	body, err := ReadResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &ProviderHTTPError{
			StatusCode: resp.StatusCode, URL: cfg.URL,
			Body: string(body), Provider: providerID,
		}
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

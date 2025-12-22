package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Embedding model constants
const (
	// DefaultGeminiEmbeddingModel is the default model for embeddings
	DefaultGeminiEmbeddingModel = "text-embedding-004"

	// EmbeddingModel004 is the current recommended model
	EmbeddingModel004 = "text-embedding-004"

	// EmbeddingModel001 is the legacy embedding model
	EmbeddingModel001 = "embedding-001"
)

// Embedding dimensions
const (
	dimensionsEmbedding004 = 768
	dimensionsEmbedding001 = 768
)

// API constants
const (
	geminiEmbeddingBaseURL    = "https://generativelanguage.googleapis.com/v1beta"
	embedContentPath          = "/models/%s:embedContent"
	batchEmbedContentsPath    = "/models/%s:batchEmbedContents"
	maxGeminiBatch            = 100 // Gemini batch limit
	geminiEmbeddingTimeoutSec = 60
	tokensPerMillion          = 1_000_000
)

// Pricing per 1M tokens (as of late 2024)
const (
	pricingEmbedding004Per1M = 0.00 // Free tier for now
	pricingEmbedding001Per1M = 0.00
)

// EmbeddingProvider implements embedding generation via Gemini API.
type EmbeddingProvider struct {
	model      string
	baseURL    string
	apiKey     string
	client     *http.Client
	dimensions int
}

// EmbeddingOption configures the EmbeddingProvider.
type EmbeddingOption func(*EmbeddingProvider)

// WithGeminiEmbeddingModel sets the embedding model.
func WithGeminiEmbeddingModel(model string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.model = model
		p.dimensions = geminiDimensionsForModel(model)
	}
}

// WithGeminiEmbeddingBaseURL sets a custom base URL.
func WithGeminiEmbeddingBaseURL(url string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.baseURL = url
	}
}

// WithGeminiEmbeddingAPIKey sets the API key explicitly.
func WithGeminiEmbeddingAPIKey(key string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.apiKey = key
	}
}

// WithGeminiEmbeddingHTTPClient sets a custom HTTP client.
func WithGeminiEmbeddingHTTPClient(client *http.Client) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.client = client
	}
}

// NewEmbeddingProvider creates a Gemini embedding provider.
func NewEmbeddingProvider(opts ...EmbeddingOption) (*EmbeddingProvider, error) {
	p := &EmbeddingProvider{
		model:      DefaultGeminiEmbeddingModel,
		baseURL:    geminiEmbeddingBaseURL,
		dimensions: dimensionsEmbedding004,
		client:     &http.Client{Timeout: geminiEmbeddingTimeoutSec * time.Second},
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	// Get API key from environment if not set
	if p.apiKey == "" {
		_, apiKey := providers.NewBaseProviderWithAPIKey("", false, "GEMINI_API_KEY", "GOOGLE_API_KEY")
		p.apiKey = apiKey
	}

	if p.apiKey == "" {
		return nil, fmt.Errorf("gemini API key not found: set GEMINI_API_KEY environment variable")
	}

	return p, nil
}

// Gemini embedding API request/response structures

type geminiEmbedRequest struct {
	Model   string             `json:"model"`
	Content geminiEmbedContent `json:"content"`
}

type geminiEmbedContent struct {
	Parts []geminiEmbedPart `json:"parts"`
}

type geminiEmbedPart struct {
	Text string `json:"text"`
}

type geminiBatchEmbedRequest struct {
	Requests []geminiEmbedRequest `json:"requests"`
}

type geminiEmbedResponse struct {
	Embedding *geminiEmbeddingData `json:"embedding,omitempty"`
	Error     *geminiEmbedError    `json:"error,omitempty"`
}

type geminiBatchEmbedResponse struct {
	Embeddings []geminiEmbeddingData `json:"embeddings,omitempty"`
	Error      *geminiEmbedError     `json:"error,omitempty"`
}

type geminiEmbeddingData struct {
	Values []float32 `json:"values"`
}

type geminiEmbedError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// Embed generates embeddings for the given texts.
func (p *EmbeddingProvider) Embed(
	ctx context.Context,
	req providers.EmbeddingRequest,
) (providers.EmbeddingResponse, error) {
	if len(req.Texts) == 0 {
		return providers.EmbeddingResponse{
			Embeddings: [][]float32{},
			Model:      p.model,
		}, nil
	}

	// Use model override if provided
	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	// Use batch endpoint for multiple texts
	if len(req.Texts) > 1 {
		return p.embedBatch(ctx, req.Texts, model)
	}

	return p.embedSingle(ctx, req.Texts[0], model)
}

// embedSingle embeds a single text using the embedContent endpoint.
func (p *EmbeddingProvider) embedSingle(
	ctx context.Context,
	text, model string,
) (providers.EmbeddingResponse, error) {
	reqBody := geminiEmbedRequest{
		Model: fmt.Sprintf("models/%s", model),
		Content: geminiEmbedContent{
			Parts: []geminiEmbedPart{{Text: text}},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s"+embedContentPath+"?key=%s", p.baseURL, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	start := time.Now()
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return providers.EmbeddingResponse{}, fmt.Errorf("embedding API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embedResp geminiEmbedResponse
	if err := json.Unmarshal(body, &embedResp); err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if embedResp.Error != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("embedding API error: %s", embedResp.Error.Message)
	}

	if embedResp.Embedding == nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("no embedding in response")
	}

	logger.Debug("Gemini embedding request completed",
		"model", model,
		"texts", 1,
		"latency_ms", time.Since(start).Milliseconds(),
	)

	return providers.EmbeddingResponse{
		Embeddings: [][]float32{embedResp.Embedding.Values},
		Model:      model,
	}, nil
}

// embedBatch embeds multiple texts using the batchEmbedContents endpoint.
func (p *EmbeddingProvider) embedBatch(
	ctx context.Context,
	texts []string,
	model string,
) (providers.EmbeddingResponse, error) {
	// Handle batching if over limit
	if len(texts) > maxGeminiBatch {
		return p.embedBatched(ctx, texts, model)
	}

	return p.embedBatchSingle(ctx, texts, model)
}

// embedBatchSingle sends a single batch request.
func (p *EmbeddingProvider) embedBatchSingle(
	ctx context.Context,
	texts []string,
	model string,
) (providers.EmbeddingResponse, error) {
	requests := make([]geminiEmbedRequest, len(texts))
	for i, text := range texts {
		requests[i] = geminiEmbedRequest{
			Model: fmt.Sprintf("models/%s", model),
			Content: geminiEmbedContent{
				Parts: []geminiEmbedPart{{Text: text}},
			},
		}
	}

	reqBody := geminiBatchEmbedRequest{Requests: requests}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s"+batchEmbedContentsPath+"?key=%s", p.baseURL, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	start := time.Now()
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return providers.EmbeddingResponse{}, fmt.Errorf("embedding API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embedResp geminiBatchEmbedResponse
	if err := json.Unmarshal(body, &embedResp); err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if embedResp.Error != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("embedding API error: %s", embedResp.Error.Message)
	}

	if len(embedResp.Embeddings) != len(texts) {
		return providers.EmbeddingResponse{}, fmt.Errorf(
			"expected %d embeddings, got %d", len(texts), len(embedResp.Embeddings))
	}

	embeddings := make([][]float32, len(embedResp.Embeddings))
	for i, emb := range embedResp.Embeddings {
		embeddings[i] = emb.Values
	}

	logger.Debug("Gemini batch embedding request completed",
		"model", model,
		"texts", len(texts),
		"latency_ms", time.Since(start).Milliseconds(),
	)

	return providers.EmbeddingResponse{
		Embeddings: embeddings,
		Model:      model,
	}, nil
}

// embedBatched handles embedding requests that exceed the batch limit.
func (p *EmbeddingProvider) embedBatched(
	ctx context.Context,
	texts []string,
	model string,
) (providers.EmbeddingResponse, error) {
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += maxGeminiBatch {
		end := i + maxGeminiBatch
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]
		resp, err := p.embedBatchSingle(ctx, batch, model)
		if err != nil {
			return providers.EmbeddingResponse{}, fmt.Errorf("batch %d failed: %w", i/maxGeminiBatch, err)
		}

		allEmbeddings = append(allEmbeddings, resp.Embeddings...)
	}

	return providers.EmbeddingResponse{
		Embeddings: allEmbeddings,
		Model:      model,
	}, nil
}

// EmbeddingDimensions returns the dimensionality of embedding vectors.
func (p *EmbeddingProvider) EmbeddingDimensions() int {
	return p.dimensions
}

// MaxBatchSize returns the maximum texts per single API request.
func (p *EmbeddingProvider) MaxBatchSize() int {
	return maxGeminiBatch
}

// ID returns the provider identifier.
func (p *EmbeddingProvider) ID() string {
	return "gemini-embedding"
}

// Model returns the current embedding model.
func (p *EmbeddingProvider) Model() string {
	return p.model
}

// EstimateCost estimates the cost for embedding the given number of tokens.
// Note: Gemini embeddings are currently free tier.
func (p *EmbeddingProvider) EstimateCost(tokens int) float64 {
	pricePerMillion := pricingEmbedding004Per1M

	switch p.model {
	case EmbeddingModel001:
		pricePerMillion = pricingEmbedding001Per1M
	case EmbeddingModel004:
		pricePerMillion = pricingEmbedding004Per1M
	}

	return float64(tokens) * pricePerMillion / tokensPerMillion
}

// geminiDimensionsForModel returns the embedding dimensions for a given model.
// Currently all Gemini models use 768 dimensions, but kept for future extensibility.
//
//nolint:unparam // returns constant for now, but will vary with future models
func geminiDimensionsForModel(model string) int {
	switch model {
	case EmbeddingModel001:
		return dimensionsEmbedding001
	case EmbeddingModel004:
		return dimensionsEmbedding004
	default:
		return dimensionsEmbedding004
	}
}

// Verify interface compliance
var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)

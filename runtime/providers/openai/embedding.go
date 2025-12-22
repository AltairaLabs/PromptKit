package openai

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
	// DefaultEmbeddingModel is the default model for embeddings
	DefaultEmbeddingModel = "text-embedding-3-small"

	// EmbeddingModelAda002 is the legacy ada-002 model
	EmbeddingModelAda002 = "text-embedding-ada-002"

	// EmbeddingModel3Small is the newer small model with better performance
	EmbeddingModel3Small = "text-embedding-3-small"

	// EmbeddingModel3Large is the large model with highest quality
	EmbeddingModel3Large = "text-embedding-3-large"
)

// Embedding dimensions by model
const (
	dimensionsAda002 = 1536
	dimensions3Small = 1536
	dimensions3Large = 3072
)

// API constants
const (
	embeddingsPath      = "/embeddings"
	maxEmbeddingBatch   = 2048 // OpenAI limit
	embeddingTimeoutSec = 60
)

// Pricing per 1M tokens (as of late 2024)
const (
	pricingAda002Per1M  = 0.10
	pricing3SmallPer1M  = 0.02
	pricing3LargePer1M  = 0.13
)

// EmbeddingProvider implements embedding generation via OpenAI API.
type EmbeddingProvider struct {
	*providers.BaseEmbeddingProvider
}

// EmbeddingOption configures the EmbeddingProvider.
type EmbeddingOption func(*EmbeddingProvider)

// WithEmbeddingModel sets the embedding model.
func WithEmbeddingModel(model string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.ProviderModel = model
		p.Dimensions = dimensionsForModel(model)
	}
}

// WithEmbeddingBaseURL sets a custom base URL (for Azure or proxies).
func WithEmbeddingBaseURL(url string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.BaseURL = url
	}
}

// WithEmbeddingAPIKey sets the API key explicitly.
func WithEmbeddingAPIKey(key string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.APIKey = key
	}
}

// WithEmbeddingHTTPClient sets a custom HTTP client.
func WithEmbeddingHTTPClient(client *http.Client) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.HTTPClient = client
	}
}

// NewEmbeddingProvider creates an OpenAI embedding provider.
func NewEmbeddingProvider(opts ...EmbeddingOption) (*EmbeddingProvider, error) {
	p := &EmbeddingProvider{
		BaseEmbeddingProvider: providers.NewBaseEmbeddingProvider(
			"openai-embedding",
			DefaultEmbeddingModel,
			"https://api.openai.com/v1",
			dimensions3Small,
			maxEmbeddingBatch,
			embeddingTimeoutSec*time.Second,
		),
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	// Get API key from environment if not set
	if p.APIKey == "" {
		_, apiKey := providers.NewBaseProviderWithAPIKey("", false, "OPENAI_API_KEY", "OPENAI_TOKEN")
		p.APIKey = apiKey
	}

	if p.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key not found: set OPENAI_API_KEY environment variable")
	}

	return p, nil
}

// embeddingRequest is the OpenAI embeddings API request format.
type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embeddingResponse is the OpenAI embeddings API response format.
type embeddingResponse struct {
	Object string             `json:"object"`
	Data   []embeddingData    `json:"data"`
	Model  string             `json:"model"`
	Usage  embeddingUsage     `json:"usage"`
	Error  *embeddingAPIError `json:"error,omitempty"`
}

type embeddingData struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type embeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type embeddingAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// Embed generates embeddings for the given texts.
func (p *EmbeddingProvider) Embed(
	ctx context.Context, req providers.EmbeddingRequest,
) (providers.EmbeddingResponse, error) {
	return p.EmbedWithEmptyCheck(ctx, req, p.embedTexts)
}

// embedTexts performs the actual embedding request.
func (p *EmbeddingProvider) embedTexts(
	ctx context.Context, texts []string, model string,
) (providers.EmbeddingResponse, error) {
	// Handle batching if needed
	if len(texts) > maxEmbeddingBatch {
		return p.embedBatched(ctx, texts, model)
	}
	return p.embedSingle(ctx, texts, model)
}

// embedSingle sends a single embedding request.
func (p *EmbeddingProvider) embedSingle(
	ctx context.Context, texts []string, model string,
) (providers.EmbeddingResponse, error) {
	reqBody := embeddingRequest{
		Model: model,
		Input: texts,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.BaseURL + embeddingsPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set(authorizationHeader, bearerPrefix+p.APIKey)

	start := time.Now()
	resp, err := p.HTTPClient.Do(httpReq)
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return providers.EmbeddingResponse{},
			fmt.Errorf("embedding API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embedResp embeddingResponse
	if err := json.Unmarshal(body, &embedResp); err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if embedResp.Error != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("embedding API error: %s", embedResp.Error.Message)
	}

	// Extract embeddings in correct order
	embeddings := make([][]float32, len(texts))
	for _, data := range embedResp.Data {
		if data.Index < len(embeddings) {
			embeddings[data.Index] = data.Embedding
		}
	}

	logger.Debug("OpenAI embedding request completed",
		"model", model,
		"texts", len(texts),
		"tokens", embedResp.Usage.TotalTokens,
		"latency_ms", time.Since(start).Milliseconds(),
	)

	return providers.EmbeddingResponse{
		Embeddings: embeddings,
		Model:      embedResp.Model,
		Usage:      &providers.EmbeddingUsage{TotalTokens: embedResp.Usage.TotalTokens},
	}, nil
}

// embedBatched handles embedding requests that exceed the batch limit.
func (p *EmbeddingProvider) embedBatched(
	ctx context.Context, texts []string, model string,
) (providers.EmbeddingResponse, error) {
	var allEmbeddings [][]float32
	var totalTokens int

	for i := 0; i < len(texts); i += maxEmbeddingBatch {
		end := i + maxEmbeddingBatch
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]
		resp, err := p.embedSingle(ctx, batch, model)
		if err != nil {
			return providers.EmbeddingResponse{}, fmt.Errorf("batch %d failed: %w", i/maxEmbeddingBatch, err)
		}

		allEmbeddings = append(allEmbeddings, resp.Embeddings...)
		if resp.Usage != nil {
			totalTokens += resp.Usage.TotalTokens
		}
	}

	return providers.EmbeddingResponse{
		Embeddings: allEmbeddings,
		Model:      model,
		Usage:      &providers.EmbeddingUsage{TotalTokens: totalTokens},
	}, nil
}

// EstimateCost estimates the cost for embedding the given number of tokens.
func (p *EmbeddingProvider) EstimateCost(tokens int) float64 {
	pricePerMillion := pricing3SmallPer1M // Default

	switch p.ProviderModel {
	case EmbeddingModelAda002:
		pricePerMillion = pricingAda002Per1M
	case EmbeddingModel3Small:
		pricePerMillion = pricing3SmallPer1M
	case EmbeddingModel3Large:
		pricePerMillion = pricing3LargePer1M
	}

	return float64(tokens) * pricePerMillion / 1_000_000
}

// dimensionsForModel returns the embedding dimensions for a given model.
func dimensionsForModel(model string) int {
	switch model {
	case EmbeddingModelAda002:
		return dimensionsAda002
	case EmbeddingModel3Small:
		return dimensions3Small
	case EmbeddingModel3Large:
		return dimensions3Large
	default:
		return dimensions3Small // Default to 3-small dimensions
	}
}

// Verify interface compliance
var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)

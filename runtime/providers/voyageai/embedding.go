// Package voyageai provides embedding generation via the Voyage AI API.
// Voyage AI is recommended by Anthropic for embeddings with Claude-based systems.
package voyageai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Model constants for Voyage AI embeddings.
const (
	// DefaultModel is the recommended general-purpose model.
	DefaultModel = "voyage-3.5"

	// ModelVoyage35 is the latest general-purpose model for best performance.
	ModelVoyage35 = "voyage-3.5"

	// ModelVoyage35Lite is an efficient model with lower latency.
	ModelVoyage35Lite = "voyage-3.5-lite"

	// ModelVoyage3Large is a high-capacity model for complex tasks.
	ModelVoyage3Large = "voyage-3-large"

	// ModelVoyageCode3 is optimized for code embeddings.
	ModelVoyageCode3 = "voyage-code-3"

	// ModelVoyageFinance2 is optimized for finance domain.
	ModelVoyageFinance2 = "voyage-finance-2"

	// ModelVoyageLaw2 is optimized for legal domain.
	ModelVoyageLaw2 = "voyage-law-2"
)

// Dimension constants for Voyage AI embeddings.
const (
	Dimensions2048 = 2048
	Dimensions1024 = 1024 // Default
	Dimensions512  = 512
	Dimensions256  = 256
)

// InputType constants for retrieval optimization.
const (
	// InputTypeQuery indicates the input is a search query.
	InputTypeQuery = "query"

	// InputTypeDocument indicates the input is a document to be indexed.
	InputTypeDocument = "document"
)

// API constants.
const (
	defaultBaseURL      = "https://api.voyageai.com/v1"
	embeddingsPath      = "/embeddings"
	defaultTimeout      = 60 * time.Second
	defaultMaxBatchSize = 128 // Reasonable default; actual limit is token-based
)

// EmbeddingProvider implements embedding generation via Voyage AI API.
type EmbeddingProvider struct {
	*providers.BaseEmbeddingProvider
	inputType string // Optional: "query" or "document"
}

// EmbeddingOption configures the EmbeddingProvider.
type EmbeddingOption func(*EmbeddingProvider)

// WithModel sets the embedding model.
func WithModel(model string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.ProviderModel = model
	}
}

// WithDimensions sets the output embedding dimensions.
func WithDimensions(dims int) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.Dimensions = dims
	}
}

// WithBaseURL sets a custom base URL.
func WithBaseURL(url string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.BaseURL = url
	}
}

// WithAPIKey sets the API key explicitly.
func WithAPIKey(key string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.APIKey = key
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.HTTPClient = client
	}
}

// WithInputType sets the input type for retrieval optimization.
// Use "query" for search queries and "document" for documents to be indexed.
func WithInputType(inputType string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.inputType = inputType
	}
}

// NewEmbeddingProvider creates a Voyage AI embedding provider.
func NewEmbeddingProvider(opts ...EmbeddingOption) (*EmbeddingProvider, error) {
	p := &EmbeddingProvider{
		BaseEmbeddingProvider: providers.NewBaseEmbeddingProvider(
			"voyageai-embedding",
			DefaultModel,
			defaultBaseURL,
			Dimensions1024,
			defaultMaxBatchSize,
			defaultTimeout,
		),
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	// Get API key from environment if not set
	if p.APIKey == "" {
		p.APIKey = os.Getenv("VOYAGE_API_KEY")
	}

	if p.APIKey == "" {
		return nil, fmt.Errorf("voyage AI API key not found: set VOYAGE_API_KEY environment variable")
	}

	return p, nil
}

// embeddingRequest is the Voyage AI embeddings API request format.
type embeddingRequest struct {
	Model           string   `json:"model"`
	Input           []string `json:"input"`
	InputType       string   `json:"input_type,omitempty"`
	OutputDimension int      `json:"output_dimension,omitempty"`
	OutputDtype     string   `json:"output_dtype,omitempty"`
}

// embeddingResponse is the Voyage AI embeddings API response format.
type embeddingResponse struct {
	Object string          `json:"object"`
	Data   []embeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  embeddingUsage  `json:"usage"`
}

type embeddingData struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type embeddingUsage struct {
	TotalTokens int `json:"total_tokens"`
}

type errorResponse struct {
	Detail string `json:"detail"`
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
	reqBody := embeddingRequest{
		Model: model,
		Input: texts,
	}

	// Add optional parameters
	if p.inputType != "" {
		reqBody.InputType = p.inputType
	}
	if p.Dimensions > 0 && p.Dimensions != Dimensions1024 {
		reqBody.OutputDimension = p.Dimensions
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

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

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
		var errResp errorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Detail != "" {
			return providers.EmbeddingResponse{},
				fmt.Errorf("voyage AI API error (status %d): %s", resp.StatusCode, errResp.Detail)
		}
		return providers.EmbeddingResponse{},
			fmt.Errorf("voyage AI API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embedResp embeddingResponse
	if err := json.Unmarshal(body, &embedResp); err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Extract embeddings in correct order
	embeddings := make([][]float32, len(texts))
	for _, data := range embedResp.Data {
		if data.Index < len(embeddings) {
			embeddings[data.Index] = data.Embedding
		}
	}

	logger.Debug("Voyage AI embedding request completed",
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

// Verify interface compliance
var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)

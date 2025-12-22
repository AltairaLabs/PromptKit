// Package voyageai provides embedding generation via the Voyage AI API.
// Voyage AI is recommended by Anthropic for embeddings with Claude-based systems.
package voyageai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

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
	voyageBaseURL       = "https://api.voyageai.com/v1"
	voyageEmbedEndpoint = "/embeddings"
	voyageTimeout       = 60 * time.Second
	voyageMaxBatch      = 128 // Reasonable default; actual limit is token-based
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
			voyageBaseURL,
			Dimensions1024,
			voyageMaxBatch,
			voyageTimeout,
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

// voyageRequest is the Voyage AI embeddings API request format.
type voyageRequest struct {
	Model           string   `json:"model"`
	Input           []string `json:"input"`
	InputType       string   `json:"input_type,omitempty"`
	OutputDimension int      `json:"output_dimension,omitempty"`
	OutputDtype     string   `json:"output_dtype,omitempty"`
}

// voyageResponse is the Voyage AI embeddings API response format.
type voyageResponse struct {
	Object string            `json:"object"`
	Data   []voyageEmbedding `json:"data"`
	Model  string            `json:"model"`
	Usage  voyageUsage       `json:"usage"`
}

type voyageEmbedding struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type voyageUsage struct {
	TotalTokens int `json:"total_tokens"`
}

// Embed generates embeddings for the given texts.
func (p *EmbeddingProvider) Embed(
	ctx context.Context, req providers.EmbeddingRequest,
) (providers.EmbeddingResponse, error) {
	return p.EmbedWithEmptyCheck(ctx, req, p.embedTexts)
}

// embedTexts performs the actual embedding request to Voyage AI.
func (p *EmbeddingProvider) embedTexts(
	ctx context.Context, texts []string, model string,
) (providers.EmbeddingResponse, error) {
	voyageReq := voyageRequest{
		Model: model,
		Input: texts,
	}

	// Add Voyage-specific optional parameters
	if p.inputType != "" {
		voyageReq.InputType = p.inputType
	}
	if p.Dimensions > 0 && p.Dimensions != Dimensions1024 {
		voyageReq.OutputDimension = p.Dimensions
	}

	reqBytes, marshalErr := providers.MarshalRequest(voyageReq)
	if marshalErr != nil {
		return providers.EmbeddingResponse{}, marshalErr
	}

	requestStart := time.Now()
	respBytes, httpErr := p.DoEmbeddingRequest(ctx, providers.HTTPRequestConfig{
		URL:       p.BaseURL + voyageEmbedEndpoint,
		Body:      reqBytes,
		UseAPIKey: true,
	})
	if httpErr != nil {
		return providers.EmbeddingResponse{}, httpErr
	}

	var voyageResp voyageResponse
	if unmarshalErr := providers.UnmarshalResponse(respBytes, &voyageResp); unmarshalErr != nil {
		return providers.EmbeddingResponse{}, unmarshalErr
	}

	// Reorder embeddings by index for consistent output
	orderedEmbeddings, _ := providers.ExtractOrderedEmbeddings(
		voyageResp.Data,
		func(v voyageEmbedding) int { return v.Index },
		func(v voyageEmbedding) []float32 { return v.Embedding },
		len(texts),
	)

	providers.LogEmbeddingRequestWithTokens("Voyage AI", model, len(texts), voyageResp.Usage.TotalTokens, requestStart)

	return providers.EmbeddingResponse{
		Embeddings: orderedEmbeddings,
		Model:      voyageResp.Model,
		Usage:      &providers.EmbeddingUsage{TotalTokens: voyageResp.Usage.TotalTokens},
	}, nil
}

// Verify interface compliance
var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)

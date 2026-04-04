package ollama

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Embedding model constants.
const (
	// DefaultEmbeddingModel is the recommended model for general-purpose embeddings.
	DefaultEmbeddingModel = "nomic-embed-text"

	// EmbeddingModelNomicText is the default model (768 dimensions, ~270MB).
	EmbeddingModelNomicText = "nomic-embed-text"

	// EmbeddingModelMxbaiLarge is a larger model for higher quality (1024 dimensions).
	EmbeddingModelMxbaiLarge = "mxbai-embed-large"

	// EmbeddingModelAllMiniLM is a small, fast model (384 dimensions).
	EmbeddingModelAllMiniLM = "all-minilm"
)

// Embedding dimensions by model.
const (
	dimensionsNomicText = 768
	dimensionsMxbai     = 1024
	dimensionsAllMiniLM = 384
)

// API constants.
const (
	ollamaEmbedPath     = "/api/embed"
	maxEmbeddingBatch   = 512 // Ollama handles batching well; keep reasonable
	embeddingTimeoutSec = 120 // Local models may be slow on first load
)

// DefaultOllamaURL is the default Ollama server URL.
const DefaultOllamaURL = "http://localhost:11434"

// EmbeddingProvider implements embedding generation via the Ollama API.
// No API key is needed — Ollama runs locally or on a private network.
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

// WithEmbeddingBaseURL sets a custom Ollama server URL.
func WithEmbeddingBaseURL(url string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.BaseURL = url
	}
}

// WithEmbeddingHTTPClient sets a custom HTTP client.
func WithEmbeddingHTTPClient(client *http.Client) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.HTTPClient = client
	}
}

// WithEmbeddingDimensions overrides the default dimensions for the model.
// Use this for custom or fine-tuned models with non-standard dimensions.
func WithEmbeddingDimensions(dims int) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.Dimensions = dims
	}
}

// NewEmbeddingProvider creates an Ollama embedding provider.
// Ollama runs locally — no API key is required.
func NewEmbeddingProvider(opts ...EmbeddingOption) *EmbeddingProvider {
	p := &EmbeddingProvider{
		BaseEmbeddingProvider: providers.NewBaseEmbeddingProvider(
			"ollama-embedding",
			DefaultEmbeddingModel,
			DefaultOllamaURL,
			dimensionsNomicText,
			maxEmbeddingBatch,
			embeddingTimeoutSec*time.Second,
		),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// ollamaEmbedRequest is the Ollama /api/embed request format.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"` // string or []string
}

// ollamaEmbedResponse is the Ollama /api/embed response format.
type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
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
	// Ollama accepts string or []string for input.
	// Use string for single text (simpler), []string for batches.
	var input any
	if len(texts) == 1 {
		input = texts[0]
	} else {
		input = texts
	}

	reqBody := ollamaEmbedRequest{
		Model: model,
		Input: input,
	}

	jsonBody, err := providers.MarshalRequest(reqBody)
	if err != nil {
		return providers.EmbeddingResponse{}, err
	}

	start := time.Now()
	body, err := p.DoEmbeddingRequest(ctx, providers.HTTPRequestConfig{
		URL:       p.BaseURL + ollamaEmbedPath,
		Body:      jsonBody,
		UseAPIKey: false, // Ollama needs no auth
	})
	if err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("ollama embed: %w", err)
	}

	var embedResp ollamaEmbedResponse
	if err := providers.UnmarshalResponse(body, &embedResp); err != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("ollama embed: %w", err)
	}

	if len(embedResp.Embeddings) != len(texts) {
		return providers.EmbeddingResponse{}, fmt.Errorf(
			"ollama embed: expected %d embeddings, got %d",
			len(texts), len(embedResp.Embeddings),
		)
	}

	providers.LogEmbeddingRequest("ollama", model, len(texts), start)

	return providers.EmbeddingResponse{
		Embeddings: embedResp.Embeddings,
		Model:      embedResp.Model,
	}, nil
}

// dimensionsForModel returns the expected dimensions for a known model.
func dimensionsForModel(model string) int {
	switch model {
	case EmbeddingModelNomicText:
		return dimensionsNomicText
	case EmbeddingModelMxbaiLarge:
		return dimensionsMxbai
	case EmbeddingModelAllMiniLM:
		return dimensionsAllMiniLM
	default:
		return dimensionsNomicText // safe default
	}
}

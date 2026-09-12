package gemini

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Embedding model constants.
//
// Checked against the live model list (v1beta ListModels, 2026-08-30): the only
// models advertising embedContent are gemini-embedding-001, gemini-embedding-2
// and gemini-embedding-2-preview. The two older ids below are gone.
const (
	// EmbeddingModelGemini001 is the current stable embedding model.
	EmbeddingModelGemini001 = "gemini-embedding-001"

	// EmbeddingModelGemini2 is the newer generation, also live.
	EmbeddingModelGemini2 = "gemini-embedding-2"

	// DefaultGeminiEmbeddingModel is the default model for embeddings.
	DefaultGeminiEmbeddingModel = EmbeddingModelGemini001

	// EmbeddingModel004 is retired.
	//
	// Deprecated: the API no longer serves it — v1beta embedContent returns
	// NOT_FOUND ("models/text-embedding-004 is not found for API version
	// v1beta, or is not supported for embedContent"). It was this package's
	// DEFAULT until 2026-08-30, so every caller that did not override the model
	// got a 404. Kept so existing code still compiles; use
	// EmbeddingModelGemini001.
	EmbeddingModel004 = "text-embedding-004"

	// EmbeddingModel001 is retired.
	//
	// Deprecated: absent from the live model list alongside text-embedding-004.
	// Use EmbeddingModelGemini001.
	EmbeddingModel001 = "embedding-001"
)

// Embedding dimensions, measured against the live API rather than assumed.
const (
	// dimensionsGeminiEmbedding is what gemini-embedding-001 and
	// gemini-embedding-2 both return. The previous code claimed 768 for every
	// model — true of the retired ones, and wrong for every model that exists.
	dimensionsGeminiEmbedding = 3072

	// dimensionsEmbedding004 and dimensionsEmbedding001 describe the retired
	// models. Retained so the mapping stays honest about what they were.
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
	*providers.BaseEmbeddingProvider
}

// EmbeddingOption configures the EmbeddingProvider.
type EmbeddingOption func(*EmbeddingProvider)

// WithGeminiEmbeddingModel sets the embedding model.
func WithGeminiEmbeddingModel(model string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.ProviderModel = model
		p.Dimensions = geminiDimensionsForModel(model)
	}
}

// WithGeminiEmbeddingBaseURL sets a custom base URL.
func WithGeminiEmbeddingBaseURL(url string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.BaseURL = url
	}
}

// WithGeminiEmbeddingAPIKey sets the API key explicitly.
func WithGeminiEmbeddingAPIKey(key string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.APIKey = key
	}
}

// WithGeminiEmbeddingHTTPClient sets a custom HTTP client.
func WithGeminiEmbeddingHTTPClient(client *http.Client) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.HTTPClient = client
	}
}

// WithGeminiEmbeddingPlatformAuth marks the provider as authenticated by
// its HTTP client's transport, skipping the empty-API-key guard.
func WithGeminiEmbeddingPlatformAuth() EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.PlatformAuth = true
	}
}

// NewEmbeddingProvider creates a Gemini embedding provider.
func NewEmbeddingProvider(opts ...EmbeddingOption) (*EmbeddingProvider, error) {
	p := &EmbeddingProvider{
		BaseEmbeddingProvider: providers.NewBaseEmbeddingProvider(
			"gemini-embedding",
			DefaultGeminiEmbeddingModel,
			geminiEmbeddingBaseURL,
			dimensionsGeminiEmbedding,
			maxGeminiBatch,
			geminiEmbeddingTimeoutSec*time.Second,
		),
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	// Platform auth is applied by the HTTP client's transport; static key
	// path only applies when not in platform mode.
	if !p.PlatformAuth {
		if p.APIKey == "" {
			_, apiKey := providers.NewBaseProviderWithAPIKey("", false, "GEMINI_API_KEY", "GOOGLE_API_KEY")
			p.APIKey = apiKey
		}
		if p.APIKey == "" {
			return nil, fmt.Errorf("gemini API key not found: set GEMINI_API_KEY environment variable")
		}
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
	return p.EmbedWithEmptyCheck(ctx, req, p.embedTexts)
}

// embedTexts performs the actual embedding request.
func (p *EmbeddingProvider) embedTexts(
	ctx context.Context, texts []string, model string,
) (providers.EmbeddingResponse, error) {
	// Use batch endpoint for multiple texts
	if len(texts) > 1 {
		return p.embedBatch(ctx, texts, model)
	}
	return p.embedSingle(ctx, texts[0], model)
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

	jsonBody, err := providers.MarshalRequest(reqBody)
	if err != nil {
		return providers.EmbeddingResponse{}, err
	}

	start := time.Now()
	url := fmt.Sprintf("%s"+embedContentPath, p.BaseURL, model)
	body, err := p.DoEmbeddingRequest(ctx, providers.HTTPRequestConfig{
		URL:  url,
		Body: jsonBody,
		// Key by header, never in the URL — see gemini.applyAuth.
		//
		// Guarded like applyAuth: under WithGeminiEmbeddingPlatformAuth the key
		// is deliberately empty because the transport supplies the credential,
		// and sending a PRESENT but empty x-goog-api-key is rejected as an
		// invalid key rather than falling through to the transport.
		UseAPIKey: false,
		Headers:   apiKeyHeaders(p.APIKey),
	})
	if err != nil {
		return providers.EmbeddingResponse{}, err
	}

	var embedResp geminiEmbedResponse
	if err := providers.UnmarshalResponse(body, &embedResp); err != nil {
		return providers.EmbeddingResponse{}, err
	}

	if embedResp.Error != nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("embedding API error: %s", embedResp.Error.Message)
	}

	if embedResp.Embedding == nil {
		return providers.EmbeddingResponse{}, fmt.Errorf("no embedding in response")
	}

	providers.LogEmbeddingRequest("Gemini", model, 1, start)

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

	jsonBody, err := providers.MarshalRequest(reqBody)
	if err != nil {
		return providers.EmbeddingResponse{}, err
	}

	start := time.Now()
	url := fmt.Sprintf("%s"+batchEmbedContentsPath, p.BaseURL, model)
	body, err := p.DoEmbeddingRequest(ctx, providers.HTTPRequestConfig{
		URL:  url,
		Body: jsonBody,
		// Key by header, never in the URL — see gemini.applyAuth.
		//
		// Guarded like applyAuth: under WithGeminiEmbeddingPlatformAuth the key
		// is deliberately empty because the transport supplies the credential,
		// and sending a PRESENT but empty x-goog-api-key is rejected as an
		// invalid key rather than falling through to the transport.
		UseAPIKey: false,
		Headers:   apiKeyHeaders(p.APIKey),
	})
	if err != nil {
		return providers.EmbeddingResponse{}, err
	}

	var embedResp geminiBatchEmbedResponse
	if err := providers.UnmarshalResponse(body, &embedResp); err != nil {
		return providers.EmbeddingResponse{}, err
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

	providers.LogEmbeddingRequest("Gemini batch", model, len(texts), start)

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

// EstimateCost estimates the cost for embedding the given number of tokens.
// Note: Gemini embeddings are currently free tier.
func (p *EmbeddingProvider) EstimateCost(tokens int) float64 {
	pricePerMillion := pricingEmbedding004Per1M

	switch p.ProviderModel {
	case EmbeddingModel001:
		pricePerMillion = pricingEmbedding001Per1M
	case EmbeddingModel004:
		pricePerMillion = pricingEmbedding004Per1M
	}

	return float64(tokens) * pricePerMillion / tokensPerMillion
}

// geminiDimensionsForModel returns the embedding dimensions for a given model.
//
// The live models return 3072, verified against the API. The two 768 entries
// are the retired models, kept accurate rather than removed. Defaulting to 3072
// is the right guess for anything new, since that is what the current
// generation returns.
func geminiDimensionsForModel(model string) int {
	switch model {
	case EmbeddingModel001:
		return dimensionsEmbedding001
	case EmbeddingModel004:
		return dimensionsEmbedding004
	default:
		return dimensionsGeminiEmbedding
	}
}

// Verify interface compliance
var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)

// apiKeyHeaders returns the AI Studio auth header, or nil when there is no key
// to send. Nil matters: an empty x-goog-api-key is an invalid key, not an
// absent one, so it must not be set on the platform-auth path where the
// transport carries the credential instead.
func apiKeyHeaders(key string) map[string]string {
	if key == "" {
		return nil
	}
	return map[string]string{apiKeyHeader: key}
}

package imagen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const (
	// Use Gemini API endpoint which supports API keys
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	// Default timeout for image generation requests
	defaultTimeout = 120 * time.Second
	// Cost per image in USD
	costPerImage = 0.04
	// defaultLocation is the Google Cloud region used when none is configured.
	defaultLocation = "us-central1"
	// unitImage is the pricing/quantity unit for one generated image.
	unitImage = "image"
	// roleUser is the chat role of the prompting message.
	roleUser = "user"
)

// defaultImagenPricing exposes Imagen's per-image cost as a PricingDescriptor
// so the base registry and cost rollup can read it without hard-coding the rate.
var defaultImagenPricing = &base.PricingDescriptor{
	Source:   base.PricingSourceInline,
	Currency: "usd",
	Items: []base.PriceItem{
		{Unit: unitImage, Rate: costPerImage},
	},
}

// Provider implements the Provider interface for Google's Imagen image generation
type Provider struct {
	providers.BaseProvider
	modelName string
	BaseURL   string
	// ApiKey is the key NewProvider was given. It is fixed at construction,
	// as every provider's credential is: the BaseProvider sends the key it
	// was built with (see SetCredential), so changing ApiKey later has no
	// effect.
	ApiKey    string
	ProjectID string
	Location  string
	Defaults  providers.ProviderDefaults
	// HTTPClient is the client the provider was constructed with. Requests go
	// through GetHTTPClient, which reflects the spec's tuning.
	HTTPClient *http.Client
}

// Model returns the model name/identifier used by this provider.
func (p *Provider) Model() string {
	return p.modelName
}

// Type returns base.ProviderTypeImage. Imagen is an image generation provider,
// not an inference provider — overriding the default from the embedded
// providers.BaseProvider helper.
func (p *Provider) Type() base.ProviderType {
	return base.ProviderTypeImage
}

// Pricing returns Imagen's per-image pricing descriptor.
func (p *Provider) Pricing() *base.PricingDescriptor {
	return defaultImagenPricing
}

// imagenRequest represents the request to Imagen API
type imagenRequest struct {
	Instances  []imagenInstance `json:"instances"`
	Parameters imagenParameters `json:"parameters"`
}

type imagenInstance struct {
	Prompt string `json:"prompt"`
}

type imagenParameters struct {
	SampleCount      int    `json:"sampleCount"`
	AspectRatio      string `json:"aspectRatio,omitempty"`
	SafetyFilter     string `json:"safetyFilterLevel,omitempty"`
	PersonGeneration string `json:"personGeneration,omitempty"`
}

// imagenResponse represents the response from Imagen API
type imagenResponse struct {
	Predictions []imagenPrediction `json:"predictions"`
	Metadata    *imagenMetadata    `json:"metadata,omitempty"`
}

type imagenPrediction struct {
	BytesBase64Encoded string `json:"bytesBase64Encoded"`
	MimeType           string `json:"mimeType"`
}

type imagenMetadata struct {
	// Add metadata fields if needed
}

// Config holds configuration for creating an Imagen provider
type Config struct {
	ID               string
	Model            string
	BaseURL          string
	ApiKey           string
	ProjectID        string
	Location         string
	IncludeRawOutput bool
	Defaults         providers.ProviderDefaults
}

// NewProvider creates a new Imagen provider
func NewProvider(config Config) *Provider {
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if config.Model == "" {
		config.Model = "imagen-4.0-generate-001"
	}
	if config.Location == "" {
		config.Location = defaultLocation
	}

	httpClient := &http.Client{Timeout: defaultTimeout}
	p := &Provider{
		BaseProvider: providers.NewBaseProvider(config.ID, config.IncludeRawOutput, httpClient),
		modelName:    config.Model,
		BaseURL:      config.BaseURL,
		ApiKey:       config.ApiKey,
		ProjectID:    config.ProjectID,
		Location:     config.Location,
		HTTPClient:   httpClient,
	}
	p.Defaults = config.Defaults
	// The BaseProvider applies the key (AuthorizeRequest), after the wait
	// under a rate limit.
	p.SetCredential(providers.HeaderKeyCredential(config.ApiKey, "x-goog-api-key", ""))
	return p
}

// extractPrompt extracts the text prompt from a message
func extractPrompt(req providers.PredictionRequest) (string, error) {
	if len(req.Messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != roleUser {
		return "", fmt.Errorf("last message must be from user")
	}

	// Extract prompt from Content field or from Parts
	prompt := lastMsg.Content
	if prompt == "" && len(lastMsg.Parts) > 0 {
		// Extract text from first text part
		for _, part := range lastMsg.Parts {
			if part.Type == "text" && part.Text != nil {
				prompt = *part.Text
				break
			}
		}
	}

	if prompt == "" {
		return "", fmt.Errorf("no text prompt found in message")
	}

	return prompt, nil
}

// Predict generates images based on the last user message
func (p *Provider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	start := time.Now()

	if p.Credential() == nil {
		return providers.PredictionResponse{}, fmt.Errorf(
			"imagen: no credential: set GOOGLE_API_KEY or GEMINI_API_KEY, or configure one")
	}

	prompt, err := extractPrompt(req)
	if err != nil {
		return providers.PredictionResponse{}, err
	}

	imagenResp, respBody, err := p.predictImages(ctx, prompt, defaultImagenParameters(1, aspectSquare))
	if err != nil {
		return providers.PredictionResponse{}, err
	}

	// Build content parts with the generated image
	prediction := imagenResp.Predictions[0]
	var contentParts []types.ContentPart

	// Add a text part confirming generation
	textPart := types.NewTextPart("Generated image based on your prompt.")
	contentParts = append(contentParts, textPart)

	// Add the image part
	mimeType := prediction.MimeType
	if mimeType == "" {
		mimeType = "image/png"
	}
	imagePart := types.NewImagePartFromData(prediction.BytesBase64Encoded, mimeType, nil)
	contentParts = append(contentParts, imagePart)

	latency := time.Since(start)

	// Estimate costs (Imagen pricing is per image, not token-based)
	// Imagen 4.0: $0.04 per image
	costBreakdown := types.CostInfo{
		InputTokens:  0,
		OutputTokens: 0,
		TotalCost:    costPerImage,

		// Unified cost fields for the new aggregation path:
		Quantities:     map[string]float64{unitImage: 1},
		DimensionMatch: map[string]string{"size": "1024x1024", "quality": "standard"},
		ProviderName:   p.Name(),
		Capability:     string(base.ProviderTypeImage),
		Latency:        latency,
	}

	return providers.PredictionResponse{
		Content:  "Generated image based on your prompt.",
		Parts:    contentParts,
		CostInfo: &costBreakdown,
		Latency:  latency,
		Raw:      respBody,
	}, nil
}

// Generate satisfies base.ImageProvider. It sends req.Size as the supported
// aspect ratio nearest the size's width/height ratio (see aspectRatioForSize)
// and req.Count as sampleCount, and returns every image the API generated,
// base64-encoded as the API sends them.
//
// Imagen has no quality setting, so req.Quality is not sent: image quality is
// chosen by the model (for example imagen-4.0-ultra-generate-001 rather than
// imagen-4.0-fast-generate-001).
func (p *Provider) Generate(ctx context.Context, req base.ImageRequest) (base.ImageResponse, error) {
	start := time.Now()
	if p.Credential() == nil {
		return base.ImageResponse{}, fmt.Errorf(
			"imagen: no credential: set GOOGLE_API_KEY or GEMINI_API_KEY, or configure one")
	}
	if req.Prompt == "" {
		return base.ImageResponse{}, fmt.Errorf("imagen: no text prompt")
	}
	count, err := sampleCountFor(req.Count)
	if err != nil {
		return base.ImageResponse{}, err
	}
	aspect, err := aspectRatioForSize(req.Size)
	if err != nil {
		return base.ImageResponse{}, err
	}

	resp, _, err := p.predictImages(ctx, req.Prompt, defaultImagenParameters(count, aspect))
	if err != nil {
		return base.ImageResponse{}, err
	}

	images := make([][]byte, 0, len(resp.Predictions))
	mime := ""
	for _, pred := range resp.Predictions {
		if pred.BytesBase64Encoded == "" {
			continue
		}
		images = append(images, []byte(pred.BytesBase64Encoded))
		if mime == "" {
			mime = pred.MimeType
		}
	}
	if mime == "" {
		mime = types.MIMETypeImagePNG
	}

	latency := time.Since(start)
	n := float64(len(images))
	return base.ImageResponse{
		Images:   images,
		MIMEType: mime,
		Cost: &types.CostInfo{
			TotalCost:      costPerImage * n,
			Quantities:     map[string]float64{unitImage: n},
			DimensionMatch: map[string]string{"aspect_ratio": aspect},
			ProviderName:   p.Name(),
			Capability:     string(base.ProviderTypeImage),
			Latency:        latency,
		},
		Latency: latency,
	}, nil
}

const (
	// aspectSquare is Imagen's default aspect ratio.
	aspectSquare = "1:1"
	// maxSampleCount is the most images one Imagen :predict call generates.
	maxSampleCount = 4
)

// imagenAspectRatios are the aspect ratios the Imagen API accepts, with their
// width/height values.
var imagenAspectRatios = []struct {
	name  string
	ratio float64
}{
	{aspectSquare, 1},
	{"3:4", 3.0 / 4},
	{"4:3", 4.0 / 3},
	{"9:16", 9.0 / 16},
	{"16:9", 16.0 / 9},
}

// defaultImagenParameters is the parameters block every call sends.
func defaultImagenParameters(count int, aspect string) imagenParameters {
	return imagenParameters{
		SampleCount:      count,
		AspectRatio:      aspect,
		SafetyFilter:     "block_some",
		PersonGeneration: "allow_adult",
	}
}

// sampleCountFor returns the sampleCount for a requested image count: 0 means
// one image, and more than the API generates in one call is an error.
func sampleCountFor(count int) (int, error) {
	switch {
	case count == 0:
		return 1, nil
	case count < 0 || count > maxSampleCount:
		return 0, fmt.Errorf("imagen: count %d is out of range: one call generates 1 to %d images",
			count, maxSampleCount)
	default:
		return count, nil
	}
}

// aspectRatioForSize maps a "WIDTHxHEIGHT" size to the supported aspect ratio
// nearest its width/height ratio, measured on a log scale so that 2:1 and 1:2
// are equally far from 1:1. An empty size is 1:1.
func aspectRatioForSize(size string) (string, error) {
	if size == "" {
		return aspectSquare, nil
	}
	w, h, ok := parseSize(size)
	if !ok {
		return "", fmt.Errorf("imagen: size %q is not WIDTHxHEIGHT, e.g. \"1024x1024\"", size)
	}
	want := math.Log(float64(w) / float64(h))
	best, bestDist := aspectSquare, math.Inf(1)
	for _, ar := range imagenAspectRatios {
		if d := math.Abs(want - math.Log(ar.ratio)); d < bestDist {
			best, bestDist = ar.name, d
		}
	}
	return best, nil
}

// parseSize parses "WIDTHxHEIGHT" with positive integers.
func parseSize(size string) (w, h int, ok bool) {
	ws, hs, found := strings.Cut(strings.ToLower(strings.TrimSpace(size)), "x")
	if !found {
		return 0, 0, false
	}
	w, errW := strconv.Atoi(ws)
	h, errH := strconv.Atoi(hs)
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// predictImages sends one :predict call and returns the parsed response, with
// at least one prediction, and its raw body.
func (p *Provider) predictImages(
	ctx context.Context, prompt string, params imagenParameters,
) (*imagenResponse, []byte, error) {
	imagenReq := imagenRequest{
		Instances:  []imagenInstance{{Prompt: prompt}},
		Parameters: params,
	}

	reqBody, err := json.Marshal(imagenReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL for Gemini API: models/{model}:predict
	url := fmt.Sprintf("%s/models/%s:predict", p.BaseURL, p.modelName)

	logger.Debug("API request", "provider", "Imagen", "method", "POST", "url", url)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// x-goog-api-key header for API key authentication (similar to Gemini API)
	if authErr := p.AuthorizeRequest(ctx, httpReq); authErr != nil {
		return nil, nil, fmt.Errorf("failed to apply authentication: %w", authErr)
	}
	// Provider headers (spec.Headers) go on every request, as on the other
	// providers; a collision with the credential's header is refused.
	if hdrErr := p.ApplyCustomHeaders(httpReq); hdrErr != nil {
		return nil, nil, hdrErr
	}

	// Make request
	// GetHTTPClient, not HTTPClient: CreateProviderFromSpec tunes the
	// BaseProvider client (request_timeout replaces it outright), so only it
	// carries the configured timeout, transport, headers and rate limit.
	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := providers.ReadResponseBody(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	logger.Debug("API response", "provider", "Imagen", "status_code", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var imagenResp imagenResponse
	if err := json.Unmarshal(respBody, &imagenResp); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(imagenResp.Predictions) == 0 {
		return nil, nil, fmt.Errorf("no images generated")
	}

	return &imagenResp, respBody, nil
}

// CalculateCost calculates cost breakdown (simplified for Imagen)
func (p *Provider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalCost:    costPerImage, // Per image cost
	}
}

// PredictStream is not supported for image generation
func (p *Provider) PredictStream(
	ctx context.Context,
	req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	return nil, fmt.Errorf("streaming not supported for Imagen")
}

// SupportsStreaming returns false for Imagen
func (p *Provider) SupportsStreaming() bool {
	return false
}

// Close cleans up resources
func (p *Provider) Close() error {
	return nil
}

// Compile-time check that Imagen satisfies base.ImageProvider.
var _ base.ImageProvider = (*Provider)(nil)

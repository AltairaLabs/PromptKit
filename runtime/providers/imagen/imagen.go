package imagen

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
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// Use Gemini API endpoint which supports API keys
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	// Default timeout for image generation requests
	defaultTimeout = 120 * time.Second
	// Cost per image in USD
	costPerImage = 0.04
)

// Provider implements the Provider interface for Google's Imagen image generation
type Provider struct {
	providers.BaseProvider
	Model      string
	BaseURL    string
	ApiKey     string
	ProjectID  string
	Location   string
	Defaults   providers.ProviderDefaults
	HTTPClient *http.Client
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

// ImagenConfig holds configuration for creating an Imagen provider
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
		config.Location = "us-central1"
	}

	httpClient := &http.Client{Timeout: defaultTimeout}
	p := &Provider{
		BaseProvider: providers.NewBaseProvider(config.ID, config.IncludeRawOutput, httpClient),
		Model:        config.Model,
		BaseURL:      config.BaseURL,
		ApiKey:       config.ApiKey,
		ProjectID:    config.ProjectID,
		Location:     config.Location,
		HTTPClient:   httpClient,
	}
	p.Defaults = config.Defaults
	return p
}

// extractPrompt extracts the text prompt from a message
func extractPrompt(req providers.PredictionRequest) (string, error) {
	if len(req.Messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != "user" {
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

	prompt, err := extractPrompt(req)
	if err != nil {
		return providers.PredictionResponse{}, err
	}

	// Build request
	imagenReq := imagenRequest{
		Instances: []imagenInstance{{Prompt: prompt}},
		Parameters: imagenParameters{
			SampleCount:      1,
			AspectRatio:      "1:1",
			SafetyFilter:     "block_some",
			PersonGeneration: "allow_adult",
		},
	}

	reqBody, err := json.Marshal(imagenReq)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL for Gemini API: models/{model}:predict
	url := fmt.Sprintf("%s/models/%s:predict", p.BaseURL, p.Model)

	logger.Debug("🔵 API Request", "provider", "Imagen", "method", "POST", "url", url)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// Use x-goog-api-key header for API key authentication (similar to Gemini API)
	httpReq.Header.Set("x-goog-api-key", p.ApiKey)

	// Make request
	resp, err := p.HTTPClient.Do(httpReq)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	logger.Debug("🟢 API Response", "provider", "Imagen", "status_code", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return providers.PredictionResponse{}, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var imagenResp imagenResponse
	if err := json.Unmarshal(respBody, &imagenResp); err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(imagenResp.Predictions) == 0 {
		return providers.PredictionResponse{}, fmt.Errorf("no images generated")
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
	}

	return providers.PredictionResponse{
		Content:  "Generated image based on your prompt.",
		Parts:    contentParts,
		CostInfo: &costBreakdown,
		Latency:  latency,
		Raw:      respBody,
	}, nil
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

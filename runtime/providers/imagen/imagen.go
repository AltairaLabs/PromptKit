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

// ImagenProvider implements the Provider interface for Google's Imagen image generation
type ImagenProvider struct {
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

// NewImagenProvider creates a new Imagen provider
func NewImagenProvider(
	id, model, baseURL, apiKey, projectID, location string,
	includeRawOutput bool,
	defaults providers.ProviderDefaults,
) *ImagenProvider {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if model == "" {
		model = "imagen-4.0-generate-001"
	}
	if location == "" {
		location = "us-central1"
	}

	httpClient := &http.Client{Timeout: defaultTimeout}
	p := &ImagenProvider{
		BaseProvider: providers.NewBaseProvider(id, includeRawOutput, httpClient),
		Model:        model,
		BaseURL:      baseURL,
		ApiKey:       apiKey,
		ProjectID:    projectID,
		Location:     location,
		HTTPClient:   httpClient,
	}
	p.Defaults = defaults
	return p
}

// extractPrompt extracts the text prompt from a message
func extractPrompt(req providers.ChatRequest) (string, error) {
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

// Chat generates images based on the last user message
func (p *ImagenProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	start := time.Now()

	prompt, err := extractPrompt(req)
	if err != nil {
		return providers.ChatResponse{}, err
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
		return providers.ChatResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL for Gemini API: models/{model}:predict
	url := fmt.Sprintf("%s/models/%s:predict", p.BaseURL, p.Model)

	logger.Debug("🔵 API Request", "provider", "Imagen", "method", "POST", "url", url)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// Use x-goog-api-key header for API key authentication (similar to Gemini API)
	httpReq.Header.Set("x-goog-api-key", p.ApiKey)

	// Make request
	resp, err := p.HTTPClient.Do(httpReq)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	logger.Debug("🟢 API Response", "provider", "Imagen", "status_code", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return providers.ChatResponse{}, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var imagenResp imagenResponse
	if err := json.Unmarshal(respBody, &imagenResp); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(imagenResp.Predictions) == 0 {
		return providers.ChatResponse{}, fmt.Errorf("no images generated")
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

	return providers.ChatResponse{
		Content:  "Generated image based on your prompt.",
		Parts:    contentParts,
		CostInfo: &costBreakdown,
		Latency:  latency,
		Raw:      respBody,
	}, nil
}

// CalculateCost calculates cost breakdown (simplified for Imagen)
func (p *ImagenProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalCost:    costPerImage, // Per image cost
	}
}

// ChatStream is not supported for image generation
func (p *ImagenProvider) ChatStream(
	ctx context.Context,
	req providers.ChatRequest,
) (<-chan providers.StreamChunk, error) {
	return nil, fmt.Errorf("streaming not supported for Imagen")
}

// SupportsStreaming returns false for Imagen
func (p *ImagenProvider) SupportsStreaming() bool {
	return false
}

// Close cleans up resources
func (p *ImagenProvider) Close() error {
	return nil
}

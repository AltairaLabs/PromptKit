package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)// GetMultimodalCapabilities returns Gemini's multimodal support capabilities
func (p *GeminiProvider) GetMultimodalCapabilities() MultimodalCapabilities {
// Gemini supports images, audio, and video
return MultimodalCapabilities{
SupportsImages: true,
SupportsAudio:  true,
SupportsVideo:  true,
ImageFormats: []string{
types.MIMETypeImageJPEG,
types.MIMETypeImagePNG,
types.MIMETypeImageWebP,
types.MIMETypeImageGIF,
"image/heic",
"image/heif",
},
AudioFormats: []string{
"audio/wav",
"audio/mp3",
"audio/aiff",
"audio/aac",
"audio/ogg",
"audio/flac",
},
VideoFormats: []string{
"video/mp4",
"video/mpeg",
"video/mov",
"video/avi",
"video/flv",
"video/mpg",
"video/webm",
"video/wmv",
"video/3gpp",
},
MaxImageSizeMB: 20,
MaxAudioSizeMB: 20,
MaxVideoSizeMB: 20,
}
}

// ChatMultimodal performs a chat request with multimodal content
func (p *GeminiProvider) ChatMultimodal(ctx context.Context, req ChatRequest) (ChatResponse, error) {
// Validate that messages are compatible with Gemini's capabilities
	for i := range req.Messages {
		if err := ValidateMultimodalMessage(p, req.Messages[i]); err != nil {
			return ChatResponse{}, err
		}
	}

	// Convert messages to Gemini format (handles both legacy and multimodal)
	contents, systemInstruction, err := convertMessagesToGemini(req.Messages, req.System)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Use the common chat implementation
	return p.chatWithContents(ctx, contents, systemInstruction, req.Temperature, req.TopP, req.MaxTokens, req.Seed)
}

// ChatMultimodalStream performs a streaming chat request with multimodal content
func (p *GeminiProvider) ChatMultimodalStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	// Validate that messages are compatible with Gemini's capabilities
for i := range req.Messages {
if err := ValidateMultimodalMessage(p, req.Messages[i]); err != nil {
return nil, err
}
}

// Convert messages to Gemini format (handles both legacy and multimodal)
contents, systemInstruction, err := convertMessagesToGemini(req.Messages, req.System)
if err != nil {
return nil, fmt.Errorf("failed to convert messages: %w", err)
}

// Use the common streaming implementation
return p.chatStreamWithContents(ctx, contents, systemInstruction, req.Temperature, req.TopP, req.MaxTokens, req.Seed)
}

// convertMessagesToGemini converts PromptKit messages to Gemini format
// Handles both legacy text-only and new multimodal messages
func convertMessagesToGemini(messages []types.Message, systemPrompt string) ([]geminiContent, *geminiContent, error) {
var contents []geminiContent
var systemInstruction *geminiContent

// Handle system message
if systemPrompt != "" {
systemInstruction = &geminiContent{
Parts: []geminiPart{{Text: systemPrompt}},
}
}

// Convert each message
for _, msg := range messages {
content, err := convertMessageToGemini(msg)
if err != nil {
return nil, nil, err
}
contents = append(contents, content)
}

return contents, systemInstruction, nil
}

// convertMessageToGemini converts a single PromptKit message to Gemini format
func convertMessageToGemini(msg types.Message) (geminiContent, error) {
// Handle legacy text-only messages
if !msg.IsMultimodal() {
role := msg.Role
// Gemini uses "user" and "model" roles
if role == "assistant" {
role = "model"
}
return geminiContent{
Role:  role,
Parts: []geminiPart{{Text: msg.GetContent()}},
}, nil
}

// Handle multimodal messages with parts
role := msg.Role
if role == "assistant" {
role = "model"
}

var parts []geminiPart
for _, part := range msg.Parts {
gPart, err := convertPartToGemini(part)
if err != nil {
return geminiContent{}, err
}
parts = append(parts, gPart)
}

return geminiContent{
Role:  role,
Parts: parts,
}, nil
}

// convertPartToGemini converts a ContentPart to Gemini's format
func convertPartToGemini(part types.ContentPart) (geminiPart, error) {
	switch part.Type {
	case types.ContentTypeText:
		if part.Text == nil || *part.Text == "" {
			return geminiPart{}, fmt.Errorf("text part has empty text")
		}
		return geminiPart{Text: *part.Text}, nil

	case types.ContentTypeImage, types.ContentTypeAudio, types.ContentTypeVideo:
		return convertMediaPartToGemini(part)

	default:
		return geminiPart{}, fmt.Errorf("unsupported part type: %s", part.Type)
	}
}

// convertMediaPartToGemini converts image/audio/video parts to Gemini format
func convertMediaPartToGemini(part types.ContentPart) (geminiPart, error) {
	if part.Media == nil {
		return geminiPart{}, fmt.Errorf("%s part missing media field", part.Type)
	}

	// Get MIME type
	mimeType := part.Media.MIMEType
	if mimeType == "" {
		return geminiPart{}, fmt.Errorf("%s part missing mime_type", part.Type)
	}

	// Get base64 data based on source
	var base64Data string
	var err error

	if part.Media.Data != nil && *part.Media.Data != "" {
		// Direct base64 data
		base64Data = *part.Media.Data
	} else if part.Media.URL != nil && *part.Media.URL != "" {
		// Gemini doesn't support direct URLs, need to fetch and convert
return geminiPart{}, fmt.Errorf("gemini does not support media URLs, please use inline data or file paths")
} else if part.Media.FilePath != nil && *part.Media.FilePath != "" {
// Read file and convert to base64
base64Data, err = readFileAsBase64(*part.Media.FilePath)
if err != nil {
return geminiPart{}, fmt.Errorf("failed to read file: %w", err)
}
} else {
return geminiPart{}, fmt.Errorf("%s part missing data source (data, url, or file_path)", part.Type)
}

// Create Gemini inline data part
return geminiPart{
InlineData: &geminiInlineData{
MimeType: mimeType,
Data:     base64Data,
},
}, nil
}

// readFileAsBase64 reads a file and returns its base64-encoded content
func readFileAsBase64(filePath string) (string, error) {
// Expand ~ to home directory
if strings.HasPrefix(filePath, "~/") {
home, err := os.UserHomeDir()
if err != nil {
return "", fmt.Errorf("failed to get home directory: %w", err)
}
filePath = filepath.Join(home, filePath[2:])
}

data, err := os.ReadFile(filePath)
if err != nil {
return "", fmt.Errorf("failed to read file: %w", err)
}

return base64.StdEncoding.EncodeToString(data), nil
}

// chatWithContents is a helper method for both regular and multimodal chat
// It's similar to Chat() but accepts pre-converted contents
func (p *GeminiProvider) chatWithContents(ctx context.Context, contents []geminiContent, systemInstruction *geminiContent, temperature, topP float32, maxTokens int, seed *int) (ChatResponse, error) {
	start := time.Now()

	// Apply provider defaults for zero values
	if temperature == 0 {
		temperature = p.Defaults.Temperature
	}

	if topP == 0 {
		topP = p.Defaults.TopP
	}

	if maxTokens == 0 {
		maxTokens = p.Defaults.MaxTokens
	}

	// Create request
	geminiReq := geminiRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
		GenerationConfig: geminiGenConfig{
			Temperature:     temperature,
			TopP:            topP,
			MaxOutputTokens: maxTokens,
		},
		SafetySettings: []geminiSafety{
			{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "BLOCK_NONE"},
		},
	}

	// Note: Gemini doesn't support seed parameter like OpenAI does

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response with raw request if configured
	chatResp := ChatResponse{
		Latency: time.Since(start),
	}
	if p.includeRawOutput {
		chatResp.RawRequest = geminiReq
	}

	// Build URL with API key
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.BaseURL, p.Model, p.ApiKey)

	// Debug log the request
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	logger.APIRequest("Gemini", "POST", url, headers, geminiReq)

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		logger.APIResponse("Gemini", 0, "", err)
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.APIResponse("Gemini", resp.StatusCode, "", err)
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to read response: %w", err)
	}

	// Debug log the response
	logger.APIResponse("Gemini", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check for prompt feedback errors
	if geminiResp.PromptFeedback != nil && geminiResp.PromptFeedback.BlockReason != "" {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("prompt blocked by Gemini: %s", geminiResp.PromptFeedback.BlockReason)
	}

	// Check for candidates
	if len(geminiResp.Candidates) == 0 {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("no candidates in Gemini response")
	}

	candidate := geminiResp.Candidates[0]

	// Check for content parts
	if len(candidate.Content.Parts) == 0 {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		// Handle different finish reasons
		switch candidate.FinishReason {
		case "MAX_TOKENS":
			return chatResp, fmt.Errorf("gemini returned MAX_TOKENS error")
		case "SAFETY":
			return chatResp, fmt.Errorf("response blocked by Gemini safety filters")
		case "RECITATION":
			return chatResp, fmt.Errorf("response blocked due to recitation concerns")
		default:
			return chatResp, fmt.Errorf("no content parts in response (finish reason: %s)", candidate.FinishReason)
		}
	}

	// Extract token counts
	var tokensIn, tokensOut, cachedTokens int
	if geminiResp.UsageMetadata != nil {
		tokensIn = geminiResp.UsageMetadata.PromptTokenCount
		tokensOut = geminiResp.UsageMetadata.CandidatesTokenCount
		cachedTokens = geminiResp.UsageMetadata.CachedContentTokenCount
	}

	latency := time.Since(start)

	// Calculate cost breakdown
	costBreakdown := p.CalculateCost(tokensIn, tokensOut, cachedTokens)

	chatResp.Content = candidate.Content.Parts[0].Text
	chatResp.CostInfo = &costBreakdown
	chatResp.Latency = latency
	chatResp.Raw = respBody

	return chatResp, nil
}

// chatStreamWithContents is a helper method for both regular and multimodal streaming
func (p *GeminiProvider) chatStreamWithContents(ctx context.Context, contents []geminiContent, systemInstruction *geminiContent, temperature, topP float32, maxTokens int, seed *int) (<-chan StreamChunk, error) {
	// Apply provider defaults for zero values
	if temperature == 0 {
		temperature = p.Defaults.Temperature
	}

	if topP == 0 {
		topP = p.Defaults.TopP
	}

	if maxTokens == 0 {
		maxTokens = p.Defaults.MaxTokens
	}

	// Create streaming request
	geminiReq := geminiRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
		GenerationConfig: geminiGenConfig{
			Temperature:     temperature,
			TopP:            topP,
			MaxOutputTokens: maxTokens,
		},
		SafetySettings: []geminiSafety{
			{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "BLOCK_NONE"},
		},
	}

	// Note: Gemini doesn't support seed parameter

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL for streaming
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", p.BaseURL, p.Model, p.ApiKey)

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(body))
	}

	outChan := make(chan StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}
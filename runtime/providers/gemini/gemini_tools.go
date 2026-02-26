package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	roleUser        = "user"
	providerNameLog = "Gemini-Tools"
)

// ToolProvider extends GeminiProvider with tool support
type ToolProvider struct {
	*Provider
	currentTools   providers.ProviderTools      // Store current tools for continuation
	currentRequest *providers.PredictionRequest // Store current request context for continuation
}

// NewToolProvider creates a new Gemini provider with tool support
func NewToolProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *ToolProvider {
	return &ToolProvider{
		Provider: NewProvider(id, model, baseURL, defaults, includeRawOutput),
	}
}

// NewToolProviderWithCredential creates a Gemini tool provider with explicit credential.
func NewToolProviderWithCredential(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, cred providers.Credential,
	platform string, platformConfig *providers.PlatformConfig,
) *ToolProvider {
	return &ToolProvider{
		Provider: NewProviderWithCredential(id, model, baseURL, defaults, includeRawOutput, cred, platform, platformConfig),
	}
}

// Gemini-specific tool structures
type geminiToolDeclaration struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"function_declarations"`
}

type geminiFunctionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiFunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type geminiToolPart struct {
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
	Text             string                  `json:"text,omitempty"`
}

// Tool-specific response structures that include function calls
type geminiToolContent struct {
	Role  string           `json:"role,omitempty"`
	Parts []geminiToolPart `json:"parts"`
}

type geminiToolCandidate struct {
	Content       geminiToolContent    `json:"content"`
	FinishReason  string               `json:"finishReason"`
	Index         int                  `json:"index"`
	SafetyRatings []geminiSafetyRating `json:"safetyRatings,omitempty"`
}

type geminiToolResponse struct {
	Candidates     []geminiToolCandidate `json:"candidates"`
	UsageMetadata  *geminiUsage          `json:"usageMetadata,omitempty"`
	PromptFeedback *geminiPromptFeedback `json:"promptFeedback,omitempty"`
}

// BuildTooling converts tool descriptors to Gemini format
func (p *ToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	if len(descriptors) == 0 {
		return nil, nil
	}

	functions := make([]geminiFunctionDeclaration, len(descriptors))
	for i, desc := range descriptors {
		functions[i] = geminiFunctionDeclaration{
			Name:        desc.Name,
			Description: desc.Description,
			Parameters:  desc.InputSchema,
		}
	}

	return geminiToolDeclaration{
		FunctionDeclarations: functions,
	}, nil
}

// PredictWithTools performs a predict request with tool support
//
//nolint:gocritic // hugeParam: interface signature requires value receiver
func (p *ToolProvider) PredictWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools providers.ProviderTools,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Store tools and request context for potential continuation
	p.currentTools = tools
	p.currentRequest = &req

	// Build Gemini request with tools
	geminiReq := p.buildToolRequest(req, tools, toolChoice)

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = geminiReq
	}

	// Make the API call
	respBytes, err := p.makeRequest(ctx, geminiReq)
	if err != nil {
		return predictResp, nil, err
	}

	// Parse response and extract tool calls
	return p.parseToolResponse(respBytes, predictResp)
}

// processToolMessage converts a tool result message to Gemini's functionResponse format
//
//nolint:gocritic // hugeParam: types.Message is part of established API
func processToolMessage(msg types.Message) map[string]any {
	// Use ToolResult.Content (not msg.Content which is empty for tool result messages)
	content := msg.ToolResult.Content

	var response any
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		// If unmarshal fails, wrap the content in an object
		response = map[string]any{
			"result": content,
		}
	} else {
		// Successfully unmarshaled, but check if it's a map (object) or primitive
		if _, isMap := response.(map[string]any); !isMap {
			response = map[string]any{
				"result": response,
			}
		}
	}

	// Debug: log tool message details
	logger.Debug("Processing tool message",
		"name", msg.ToolResult.Name,
		"content_length", len(content),
		"tool_result_id", msg.ToolResult.ID)

	if msg.ToolResult.Name == "" {
		logger.Warn("Tool message has empty Name field - functionResponse will be invalid")
	}

	return map[string]any{
		"functionResponse": map[string]any{
			"name":     msg.ToolResult.Name,
			"response": response,
		},
	}
}

// buildMessageParts creates parts array for a message including text, images, and tool calls
//
//nolint:gocritic // hugeParam: types.Message is part of established API
func buildMessageParts(msg types.Message, pendingToolResults []map[string]any) []any {
	parts := make([]any, 0)

	// Add pending tool results first if this is a user message
	if msg.Role == roleUser {
		for _, tr := range pendingToolResults {
			parts = append(parts, tr)
		}
	}

	// Check if message has multimodal content (images, etc.)
	if msg.HasMediaContent() {
		// Process each part including images
		for _, part := range msg.Parts {
			switch part.Type {
			case types.ContentTypeText:
				if part.Text != nil && *part.Text != "" {
					parts = append(parts, map[string]any{
						"text": *part.Text,
					})
				}
			case types.ContentTypeImage, types.ContentTypeAudio, types.ContentTypeVideo, types.ContentTypeDocument:
				if part.Media != nil {
					mediaMap := convertMediaPartToMap(part)
					if mediaMap != nil {
						parts = append(parts, mediaMap)
					}
				}
			}
		}
	} else {
		// Add text content - use GetContent() to properly handle both Content field and Parts
		textContent := msg.GetContent()
		if textContent != "" {
			parts = append(parts, map[string]any{
				"text": textContent,
			})
		}
	}

	// Add tool calls if this is a model message
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		for _, toolCall := range msg.ToolCalls {
			var args any
			if err := json.Unmarshal(toolCall.Args, &args); err != nil {
				args = string(toolCall.Args)
			}
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"name": toolCall.Name,
					"args": args,
				},
			})
		}
	}

	return parts
}

// convertMediaPartToMap converts a media ContentPart to Gemini's inline_data format
func convertMediaPartToMap(part types.ContentPart) map[string]any {
	if part.Media == nil {
		return nil
	}

	// Get MIME type
	mimeType := part.Media.MIMEType
	if mimeType == "" {
		// Default mime types based on content type
		switch part.Type {
		case types.ContentTypeImage:
			mimeType = "image/png"
		case types.ContentTypeAudio:
			mimeType = "audio/wav"
		case types.ContentTypeVideo:
			mimeType = "video/mp4"
		case types.ContentTypeDocument:
			mimeType = "application/pdf"
		default:
			return nil
		}
	}

	// Get base64 data - try Data field first, then URL
	var base64Data string
	if part.Media.Data != nil && *part.Media.Data != "" {
		base64Data = *part.Media.Data
	} else if part.Media.URL != nil && *part.Media.URL != "" {
		// For URL-based media, use the MediaLoader
		loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})
		data, err := loader.GetBase64Data(context.Background(), part.Media)
		if err != nil {
			logger.Warn("Failed to load media from URL", "url", *part.Media.URL, "error", err)
			return nil
		}
		base64Data = data
	} else {
		return nil
	}

	return map[string]any{
		"inlineData": map[string]any{
			"mimeType": mimeType,
			"data":     base64Data,
		},
	}
}

// addToolConfig adds tool configuration to the request based on toolChoice
func addToolConfig(request map[string]any, tools any, toolChoice string) {
	request["tools"] = []any{tools}

	mode := "AUTO" // default
	if toolChoice != "" {
		switch toolChoice {
		case "auto":
			mode = "AUTO"
		case "required", "any":
			mode = "ANY"
		case "none":
			mode = "NONE"
		default:
			mode = "ANY"
		}
	}

	request["tool_config"] = map[string]any{
		"function_calling_config": map[string]any{
			"mode": mode,
		},
	}
}

//nolint:gocritic // hugeParam: method uses req value throughout
func (p *ToolProvider) buildToolRequest(
	req providers.PredictionRequest, tools any, toolChoice string,
) map[string]any {
	// Convert messages to Gemini format
	contents := make([]map[string]any, 0, len(req.Messages))
	var pendingToolResults []map[string]any

	for i := range req.Messages {
		if req.Messages[i].Role == "tool" {
			pendingToolResults = append(pendingToolResults, processToolMessage(req.Messages[i]))
			continue
		}

		// If we have pending tool results, add them as a user message before non-user messages
		if len(pendingToolResults) > 0 && req.Messages[i].Role != "user" {
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": pendingToolResults,
			})
			pendingToolResults = nil
		}

		parts := buildMessageParts(req.Messages[i], pendingToolResults)
		if req.Messages[i].Role == roleUser {
			pendingToolResults = nil
		}

		if len(parts) == 0 {
			continue
		}

		role := req.Messages[i].Role
		if role == "assistant" {
			role = "model"
		}

		contents = append(contents, map[string]any{
			"role":  role,
			"parts": parts,
		})
	}

	// If there are still pending tool results at the end, add them as a final user message
	if len(pendingToolResults) > 0 {
		contents = append(contents, map[string]any{
			"role":  "user",
			"parts": pendingToolResults,
		})
	}

	// Apply defaults to zero-valued request parameters
	temperature, topP, maxTokens := p.applyRequestDefaults(req)

	request := map[string]any{
		"contents": contents,
		"generationConfig": map[string]any{
			"temperature":     temperature,
			"topP":            topP,
			"maxOutputTokens": maxTokens,
		},
	}

	if req.System != "" {
		request["systemInstruction"] = map[string]any{
			"parts": []any{
				map[string]any{"text": req.System},
			},
		}
	}

	if tools != nil {
		addToolConfig(request, tools, toolChoice)
	}

	return request
}

func (p *ToolProvider) parseToolResponse(respBytes []byte, predictResp providers.PredictionResponse) (providers.PredictionResponse, []types.MessageToolCall, error) {
	start := time.Now()

	var resp geminiToolResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBytes
		return predictResp, nil, fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	if len(resp.Candidates) == 0 {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBytes
		return predictResp, nil, fmt.Errorf("no candidates in Gemini response")
	}

	candidate := resp.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBytes
		// Handle different finish reasons
		switch candidate.FinishReason {
		case "MAX_TOKENS":
			// Don't use fallback - return error to see when this happens
			return predictResp, nil, fmt.Errorf("gemini returned MAX_TOKENS error (this should not happen with reasonable limits)")
		case "SAFETY":
			return predictResp, nil, fmt.Errorf("response blocked by Gemini safety filters")
		case "RECITATION":
			return predictResp, nil, fmt.Errorf("response blocked due to recitation concerns")
		default:
			return predictResp, nil, fmt.Errorf("no parts in Gemini candidate (finish reason: %s)", candidate.FinishReason)
		}
	}

	// Extract text content and tool calls
	var textContent string
	var toolCalls []types.MessageToolCall

	for i, part := range candidate.Content.Parts {
		// Check for text content
		if part.Text != "" {
			textContent += part.Text
		}

		// Check for function call
		if part.FunctionCall != nil {
			toolCall := types.MessageToolCall{
				ID:   fmt.Sprintf("call_%d", i), // Gemini doesn't provide IDs, so we generate them
				Name: part.FunctionCall.Name,
			}

			if part.FunctionCall.Args != nil {
				// Marshal can't fail for map[string]any
				argsBytes, _ := json.Marshal(part.FunctionCall.Args)
				toolCall.Args = json.RawMessage(argsBytes)
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	var tokensIn, tokensOut int
	if resp.UsageMetadata != nil {
		tokensIn = resp.UsageMetadata.PromptTokenCount
		tokensOut = resp.UsageMetadata.CandidatesTokenCount
	}

	// Calculate cost breakdown (Gemini doesn't support cached tokens yet)
	costBreakdown := p.Provider.CalculateCost(tokensIn, tokensOut, 0)

	predictResp.Content = textContent
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = time.Since(start)
	predictResp.Raw = respBytes
	predictResp.ToolCalls = toolCalls

	return predictResp, toolCalls, nil
}

func (p *ToolProvider) makeRequest(ctx context.Context, request any) ([]byte, error) {
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL with API key for Gemini
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.BaseURL, p.modelName, p.ApiKey)

	// Debug log the request
	var requestObj any
	if err := json.Unmarshal(requestBytes, &requestObj); err != nil {
		// If unmarshal fails for logging, use raw bytes as fallback
		requestObj = string(requestBytes)
	}
	headers := map[string]string{
		contentTypeHeader: applicationJSON,
	}
	logger.APIRequest(providerNameLog, "POST", url, headers, requestObj)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(requestBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set(contentTypeHeader, applicationJSON)

	resp, err := p.GetHTTPClient().Do(req)
	if err != nil {
		logger.APIResponse(providerNameLog, 0, "", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.APIResponse(providerNameLog, resp.StatusCode, "", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Debug log the response
	logger.APIResponse(providerNameLog, resp.StatusCode, string(respBytes), nil)

	if resp.StatusCode != http.StatusOK {
		if p.platform != "" {
			return nil, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, respBytes)
		}
		return nil, fmt.Errorf("API request to %s failed with status %d: %s",
			logger.RedactSensitiveData(url), resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

// PredictStreamWithTools performs a streaming predict request with tool support
func (p *ToolProvider) PredictStreamWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools any,
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	// Build Gemini request with tools
	geminiReq := p.buildToolRequest(req, tools, toolChoice)

	requestBytes, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Use streamGenerateContent endpoint
	url := fmt.Sprintf(
		"%s/models/%s:streamGenerateContent?key=%s",
		p.BaseURL, p.modelName, p.ApiKey,
	)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(requestBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if p.platform != "" {
			return nil, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, body)
		}
		return nil, fmt.Errorf("API request to %s failed with status %d: %s",
			logger.RedactSensitiveData(url), resp.StatusCode, string(body))
	}

	outChan := make(chan providers.StreamChunk)
	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

// Ensure ToolProvider implements StreamInputSupport by forwarding to embedded Provider
var _ providers.StreamInputSupport = (*ToolProvider)(nil)

// CreateStreamSession forwards to the embedded Provider's CreateStreamSession.
// This enables duplex streaming with tool support.
func (p *ToolProvider) CreateStreamSession(
	ctx context.Context,
	req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return p.Provider.CreateStreamSession(ctx, req)
}

// SupportsStreamInput forwards to the embedded Provider's SupportsStreamInput.
func (p *ToolProvider) SupportsStreamInput() []string {
	return p.Provider.SupportsStreamInput()
}

// GetStreamingCapabilities forwards to the embedded Provider's GetStreamingCapabilities.
func (p *ToolProvider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return p.Provider.GetStreamingCapabilities()
}

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterProviderFactory("gemini", providers.CredentialFactory(
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			return NewToolProviderWithCredential(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults,
				spec.IncludeRawOutput, spec.Credential,
				spec.Platform, spec.PlatformConfig,
			), nil
		},
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			return NewToolProvider(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults, spec.IncludeRawOutput,
			), nil
		},
	))
}

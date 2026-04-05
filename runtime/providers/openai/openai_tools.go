package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ToolProvider extends OpenAIProvider with tool support
type ToolProvider struct {
	*Provider
}

// NewToolProvider creates a new OpenAI provider with tool support
func NewToolProvider(
	id, model, baseURL string,
	defaults providers.ProviderDefaults,
	includeRawOutput bool,
	additionalConfig map[string]any,
	unsupportedParams []string,
) *ToolProvider {
	return &ToolProvider{
		Provider: NewProviderFromConfig(&ProviderConfig{
			ID: id, Model: model, BaseURL: baseURL, Defaults: defaults,
			IncludeRawOutput: includeRawOutput, AdditionalConfig: additionalConfig,
			UnsupportedParams: unsupportedParams,
		}),
	}
}

// NewToolProviderWithCredential creates an OpenAI tool provider with explicit credential.
func NewToolProviderWithCredential(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, additionalConfig map[string]any, cred providers.Credential,
	platform string, platformConfig *providers.PlatformConfig,
	unsupportedParams []string,
) *ToolProvider {
	return &ToolProvider{
		Provider: NewProviderFromConfig(&ProviderConfig{
			ID: id, Model: model, BaseURL: baseURL, Defaults: defaults,
			IncludeRawOutput: includeRawOutput, Credential: cred,
			AdditionalConfig: additionalConfig, Platform: platform,
			PlatformConfig: platformConfig, UnsupportedParams: unsupportedParams,
		}),
	}
}

// OpenAI-specific tool structures
type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // OpenAI returns this as a string, not RawMessage
}

// BuildTooling converts tool descriptors to OpenAI format.
// By default, strict mode is enabled for reliable argument generation.
// Set additional_config.strict_tools: false in provider config to disable.
func (p *ToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	if len(descriptors) == 0 {
		return nil, nil
	}

	strict := p.useStrictTools()
	tools := make([]openAITool, len(descriptors))
	for i, desc := range descriptors {
		params := desc.InputSchema
		if strict {
			params = ensureStrictSchema(params)
		}
		tools[i] = openAITool{
			Type: "function",
			Function: openAIToolFunction{
				Name:        desc.Name,
				Description: desc.Description,
				Parameters:  params,
				Strict:      strict,
			},
		}
	}

	return tools, nil
}

// ensureAdditionalPropertiesFalse injects "additionalProperties": false into a
// JSON schema object if not already present. Required by OpenAI strict mode.
// ensureStrictSchema modifies a JSON schema for OpenAI strict mode:
// - Sets additionalProperties: false on all object types (recursively)
// - Ensures all properties are listed in required
func ensureStrictSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 {
		return schema
	}
	var obj map[string]any
	if err := json.Unmarshal(schema, &obj); err != nil {
		return schema
	}

	applyStrictToObject(obj)

	result, err := json.Marshal(obj)
	if err != nil {
		return schema
	}
	return result
}

// applyStrictToObject recursively applies strict mode constraints to a schema object.
func applyStrictToObject(obj map[string]any) {
	if props, ok := obj["properties"].(map[string]any); ok {
		obj["additionalProperties"] = false

		// Require all properties
		allKeys := make([]string, 0, len(props))
		for k := range props {
			allKeys = append(allKeys, k)
		}
		obj["required"] = allKeys

		// Recurse into each property
		for _, v := range props {
			if propObj, ok := v.(map[string]any); ok {
				applyStrictToObject(propObj)
			}
		}
	} else if objType, _ := obj["type"].(string); objType == "object" {
		// Bare object with no properties — add empty properties and
		// additionalProperties:false for strict compatibility.
		obj["properties"] = map[string]any{}
		obj["additionalProperties"] = false
		obj["required"] = []string{}
	}

	// Handle items in array types
	if items, ok := obj["items"].(map[string]any); ok {
		applyStrictToObject(items)
	}
}

// useStrictTools returns whether tools should use strict schema mode.
// Defaults to true; override with additional_config.strict_tools: false.
func (p *ToolProvider) useStrictTools() bool {
	if p.additionalConfig == nil {
		return true
	}
	if v, ok := p.additionalConfig["strict_tools"].(bool); ok {
		return v
	}
	return true
}

// PredictWithTools performs a prediction request with tool support
//
//nolint:gocritic // hugeParam: interface signature requires value receiver
func (p *ToolProvider) PredictWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools providers.ProviderTools,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Route to appropriate API based on mode
	if p.apiMode == APIModeResponses {
		return p.predictWithResponses(ctx, req, tools, toolChoice)
	}

	// Legacy chat completions API
	return p.predictWithCompletions(ctx, req, tools, toolChoice)
}

// predictWithCompletions performs a prediction using the chat completions API
//
//nolint:gocritic // hugeParam: interface signature requires value receiver for compatibility
func (p *ToolProvider) predictWithCompletions(
	ctx context.Context,
	req providers.PredictionRequest,
	tools providers.ProviderTools,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Track latency - START timing
	start := time.Now()

	// Build OpenAI request with tools
	openaiReq := p.buildToolRequest(req, tools, toolChoice)

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = openaiReq
	}

	// Make the API call
	respBytes, err := p.makeRequest(ctx, openaiReq)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, nil, err
	}

	// Calculate latency immediately after API call completes
	latency := time.Since(start)

	// Parse response
	resp, toolCalls, err := p.parseToolResponse(respBytes)
	if err != nil {
		resp.Latency = latency
		resp.RawRequest = predictResp.RawRequest
		return resp, nil, err
	}

	// Set latency on response
	resp.Latency = latency
	resp.RawRequest = predictResp.RawRequest

	return resp, toolCalls, nil
}

// buildToolRequest constructs the OpenAI API request with tools
func (p *ToolProvider) buildToolRequest(req providers.PredictionRequest, tools interface{}, toolChoice string) map[string]interface{} {
	messages := p.convertRequestMessagesToOpenAI(req)

	// Apply defaults to zero-valued request parameters
	temperature, topP, maxTokens := p.applyRequestDefaults(req)

	// Build request
	openaiReq := map[string]interface{}{
		"model":    p.model,
		"messages": messages,
	}
	// Add max tokens with the correct parameter name for the model type
	addMaxTokensToRequest(openaiReq, p.unsupportedParams, maxTokens)
	// Add sampling parameters (temperature, top_p) if model supports them
	addSamplingParamsToRequest(openaiReq, p.unsupportedParams, temperature, topP)

	if req.Seed != nil {
		openaiReq["seed"] = *req.Seed
	}

	// Add modalities for audio models when audio content is present
	if p.apiMode == APIModeCompletions && isAudioModel(p.model) && requestContainsAudio(&req) {
		openaiReq["modalities"] = []string{"text", "audio"}
		openaiReq["audio"] = map[string]interface{}{
			"voice":  "alloy",
			"format": "wav",
		}
	}

	// Add tools if provided
	if tools != nil {
		openaiReq["tools"] = tools
		p.addToolChoiceToRequest(openaiReq, toolChoice)
	}

	return openaiReq
}

// convertRequestMessagesToOpenAI converts all messages in a request to OpenAI format
func (p *ToolProvider) convertRequestMessagesToOpenAI(req providers.PredictionRequest) []map[string]interface{} {
	messages := make([]map[string]interface{}, 0, len(req.Messages)+1)

	// Add system message if present
	if req.System != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": req.System,
		})
	}

	// Add conversation messages
	for i := range req.Messages {
		openaiMsg := p.convertSingleMessageForTools(req.Messages[i])
		messages = append(messages, openaiMsg)
	}

	return messages
}

// convertSingleMessageForTools converts a single message to OpenAI format including tool metadata
func (p *ToolProvider) convertSingleMessageForTools(msg types.Message) map[string]interface{} {
	// Convert message to OpenAI format (handles both legacy and multimodal)
	convertedMsg, err := p.convertMessageToOpenAI(msg)
	if err != nil {
		// Log error but continue with best effort (use Content as fallback)
		// This allows tool calls to work even if multimodal conversion fails
		convertedMsg = openAIMessage{
			Role:    msg.Role,
			Content: msg.GetContent(),
		}
	}

	// Build the message map with content
	openaiMsg := map[string]interface{}{
		"role":    convertedMsg.Role,
		"content": convertedMsg.Content,
	}

	// Add tool-related fields if present
	if len(msg.ToolCalls) > 0 {
		openaiMsg["tool_calls"] = p.convertToolCallsToOpenAI(msg.ToolCalls)
	}

	// Handle tool result messages
	if msg.Role == "tool" && msg.ToolResult != nil {
		openaiMsg["tool_call_id"] = msg.ToolResult.ID
		openaiMsg["name"] = msg.ToolResult.Name
		openaiMsg["content"] = convertToolResultContent(msg.ToolResult)
	}

	return openaiMsg
}

// convertToolResultContent converts a MessageToolResult's Parts into OpenAI content format.
// For text-only results (single text part), it returns a plain string.
// For multimodal results (containing images), it returns an array of content blocks.
func convertToolResultContent(result *types.MessageToolResult) interface{} {
	if !result.HasMedia() {
		// Text-only: return plain string for backward compatibility
		return result.GetTextContent()
	}

	// Multimodal: build content array
	parts := make([]map[string]interface{}, 0, len(result.Parts))
	for _, part := range result.Parts {
		switch part.Type {
		case types.ContentTypeText:
			if part.Text != nil {
				parts = append(parts, map[string]interface{}{
					"type": "text",
					"text": *part.Text,
				})
			}
		case types.ContentTypeImage:
			if part.Media != nil {
				if imgURL := resolveImageURL(part.Media); imgURL != "" {
					parts = append(parts, map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": imgURL},
					})
				}
			}
		default:
			// Unsupported media types in tool results are skipped
		}
	}

	if len(parts) == 0 {
		return result.GetTextContent()
	}
	return parts
}

// resolveImageURL returns the URL string for an image media content.
// For URL-referenced images, it returns the URL directly.
// For base64 data, it returns a data URI (data:<mime>;base64,<data>).
// For file paths, it reads and encodes the file as base64.
func resolveImageURL(media *types.MediaContent) string {
	if media.URL != nil && *media.URL != "" {
		return *media.URL
	}
	if media.Data != nil && *media.Data != "" {
		return "data:" + media.MIMEType + ";base64," + *media.Data
	}
	if media.FilePath != nil && *media.FilePath != "" {
		b64, err := media.GetBase64Data() //nolint:staticcheck // simple file read; MediaLoader not needed
		if err != nil {
			return ""
		}
		return "data:" + media.MIMEType + ";base64," + b64
	}
	return ""
}

// convertToolCallsToOpenAI converts ToolCalls to OpenAI format
func (p *ToolProvider) convertToolCallsToOpenAI(toolCalls []types.MessageToolCall) []map[string]interface{} {
	result := make([]map[string]interface{}, len(toolCalls))
	for i, tc := range toolCalls {
		result[i] = map[string]interface{}{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      tc.Name,
				"arguments": string(tc.Args),
			},
		}
	}
	return result
}

// addToolChoiceToRequest adds tool_choice parameter based on the choice string
func (p *ToolProvider) addToolChoiceToRequest(openaiReq map[string]interface{}, toolChoice string) {
	if toolChoice == "" || toolChoice == "auto" {
		return
	}

	switch toolChoice {
	case "required":
		openaiReq["tool_choice"] = "required"
	case "none":
		openaiReq["tool_choice"] = "none"
	default:
		// Specific function name
		openaiReq["tool_choice"] = map[string]interface{}{
			"type": "function",
			"function": map[string]string{
				"name": toolChoice,
			},
		}
	}
}

// parseToolResponse parses the OpenAI response and extracts tool calls
func (p *ToolProvider) parseToolResponse(respBytes []byte) (providers.PredictionResponse, []types.MessageToolCall, error) {
	var openaiResp struct {
		Choices []struct {
			Message struct {
				Content   string           `json:"content"`
				ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			PromptTokensDetails *struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBytes, &openaiResp); err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return providers.PredictionResponse{}, nil, fmt.Errorf("no choices in OpenAI response")
	}

	choice := openaiResp.Choices[0]

	// Get cached tokens if available
	cachedTokens := 0
	if openaiResp.Usage.PromptTokensDetails != nil {
		cachedTokens = openaiResp.Usage.PromptTokensDetails.CachedTokens
	}

	// Calculate cost breakdown
	costBreakdown := p.Provider.CalculateCost(openaiResp.Usage.PromptTokens, openaiResp.Usage.CompletionTokens, cachedTokens)

	resp := providers.PredictionResponse{
		Content:  choice.Message.Content,
		CostInfo: &costBreakdown,
		Raw:      respBytes,
	}

	// Extract tool calls
	var toolCalls []types.MessageToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, types.MessageToolCall{
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments), // Convert string to RawMessage
			ID:   tc.ID,
		})
	}

	return resp, toolCalls, nil
}

// makeRequest makes an HTTP request to the OpenAI API
func (p *ToolProvider) makeRequest(ctx context.Context, request interface{}) ([]byte, error) {
	url := p.baseURL + openAIPredictCompletionsPath
	headers := providers.RequestHeaders{
		contentTypeHeader:   applicationJSON,
		authorizationHeader: bearerPrefix + p.apiKey,
	}
	return p.MakeJSONRequest(ctx, url, request, headers, "OpenAI")
}

// PredictStreamWithTools performs a streaming predict request with tool support
func (p *ToolProvider) PredictStreamWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	// Route to appropriate API based on mode
	if p.apiMode == APIModeResponses {
		return p.predictStreamWithResponses(ctx, req, tools, toolChoice)
	}

	// Legacy chat completions API
	return p.predictStreamWithCompletions(ctx, req, tools, toolChoice)
}

// predictStreamWithCompletions performs a streaming predict using chat completions API
//
//nolint:gocritic // hugeParam: interface signature requires value receiver for compatibility
func (p *ToolProvider) predictStreamWithCompletions(
	ctx context.Context,
	req providers.PredictionRequest,
	tools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	// Build OpenAI request with tools (same as non-streaming)
	openaiReq := p.buildToolRequest(req, tools, toolChoice)

	// Add streaming options
	openaiReq["stream"] = true
	openaiReq["stream_options"] = map[string]interface{}{
		"include_usage": true,
	}

	// When streaming, OpenAI chat/completions only accepts "pcm16" for audio.format.
	// buildToolRequest defaults to "wav" (valid for non-streaming); override here.
	if audio, ok := openaiReq["audio"].(map[string]interface{}); ok {
		audio["format"] = "pcm16"
	}

	reqBody, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	url := p.baseURL + openAIPredictCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set(authorizationHeader, bearerPrefix+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if err := providers.CheckHTTPError(resp, url); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}

	outChan := make(chan providers.StreamChunk, providers.DefaultStreamBufferSize)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterProviderFactory("openai", providers.CredentialFactory(
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			return NewToolProviderWithCredential(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults,
				spec.IncludeRawOutput, spec.AdditionalConfig, spec.Credential,
				spec.Platform, spec.PlatformConfig, spec.UnsupportedParams,
			), nil
		},
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			return NewToolProvider(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults,
				spec.IncludeRawOutput, spec.AdditionalConfig, spec.UnsupportedParams,
			), nil
		},
	))
}

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

const roleUser = "user"

// GeminiToolProvider extends GeminiProvider with tool support
type GeminiToolProvider struct {
	*GeminiProvider
	currentTools   interface{}                  // Store current tools for continuation
	currentRequest *providers.PredictionRequest // Store current request context for continuation
}

// NewGeminiToolProvider creates a new Gemini provider with tool support
func NewGeminiToolProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *GeminiToolProvider {
	return &GeminiToolProvider{
		GeminiProvider: NewGeminiProvider(id, model, baseURL, defaults, includeRawOutput),
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
func (p *GeminiToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (interface{}, error) {
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
func (p *GeminiToolProvider) PredictWithTools(ctx context.Context, req providers.PredictionRequest, tools interface{}, toolChoice string) (providers.PredictionResponse, []types.MessageToolCall, error) {
	logger.Debug("PredictWithTools called",
		"toolChoice", toolChoice,
		"messages", len(req.Messages))

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
func processToolMessage(msg types.Message) map[string]interface{} {
	var response interface{}
	if err := json.Unmarshal([]byte(msg.Content), &response); err != nil {
		// If unmarshal fails, wrap the content in an object
		response = map[string]interface{}{
			"result": msg.Content,
		}
	} else {
		// Successfully unmarshaled, but check if it's a map (object) or primitive
		if _, isMap := response.(map[string]interface{}); !isMap {
			response = map[string]interface{}{
				"result": response,
			}
		}
	}

	// Debug: log tool message details
	logger.Debug("Processing tool message",
		"name", msg.ToolResult.Name,
		"content_length", len(msg.Content),
		"tool_result_id", msg.ToolResult.ID)

	if msg.ToolResult.Name == "" {
		logger.Warn("Tool message has empty Name field - functionResponse will be invalid")
	}

	return map[string]interface{}{
		"functionResponse": map[string]interface{}{
			"name":     msg.ToolResult.Name,
			"response": response,
		},
	}
}

// buildMessageParts creates parts array for a message including text and tool calls
func buildMessageParts(msg types.Message, pendingToolResults []map[string]interface{}) []interface{} {
	parts := make([]interface{}, 0)

	// Add pending tool results first if this is a user message
	if msg.Role == roleUser {
		for _, tr := range pendingToolResults {
			parts = append(parts, tr)
		}
	}

	// Add text content
	if msg.Content != "" {
		parts = append(parts, map[string]interface{}{
			"text": msg.Content,
		})
	}

	// Add tool calls if this is a model message
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		for _, toolCall := range msg.ToolCalls {
			var args interface{}
			if err := json.Unmarshal(toolCall.Args, &args); err != nil {
				args = string(toolCall.Args)
			}
			parts = append(parts, map[string]interface{}{
				"functionCall": map[string]interface{}{
					"name": toolCall.Name,
					"args": args,
				},
			})
		}
	}

	return parts
}

// addToolConfig adds tool configuration to the request based on toolChoice
func addToolConfig(request map[string]interface{}, tools interface{}, toolChoice string) {
	request["tools"] = []interface{}{tools}

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

	request["tool_config"] = map[string]interface{}{
		"function_calling_config": map[string]interface{}{
			"mode": mode,
		},
	}
}

func (p *GeminiToolProvider) buildToolRequest(req providers.PredictionRequest, tools interface{}, toolChoice string) map[string]interface{} {
	// Convert messages to Gemini format
	contents := make([]map[string]interface{}, 0, len(req.Messages))
	var pendingToolResults []map[string]interface{}

	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			pendingToolResults = append(pendingToolResults, processToolMessage(msg))
			continue
		}

		// If we have pending tool results, add them as a user message before non-user messages
		if len(pendingToolResults) > 0 && msg.Role != "user" {
			contents = append(contents, map[string]interface{}{
				"role":  "user",
				"parts": pendingToolResults,
			})
			pendingToolResults = nil
		}

		parts := buildMessageParts(msg, pendingToolResults)
		if msg.Role == roleUser {
			pendingToolResults = nil
		}

		if len(parts) == 0 {
			continue
		}

		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		contents = append(contents, map[string]interface{}{
			"role":  role,
			"parts": parts,
		})
	}

	// If there are still pending tool results at the end, add them as a final user message
	if len(pendingToolResults) > 0 {
		contents = append(contents, map[string]interface{}{
			"role":  "user",
			"parts": pendingToolResults,
		})
	}

	request := map[string]interface{}{
		"contents": contents,
		"generationConfig": map[string]interface{}{
			"temperature":     req.Temperature,
			"topP":            req.TopP,
			"maxOutputTokens": req.MaxTokens,
		},
	}

	if req.System != "" {
		request["systemInstruction"] = map[string]interface{}{
			"parts": []interface{}{
				map[string]interface{}{"text": req.System},
			},
		}
	}

	if tools != nil {
		addToolConfig(request, tools, toolChoice)
	}

	return request
}

func (p *GeminiToolProvider) parseToolResponse(respBytes []byte, predictResp providers.PredictionResponse) (providers.PredictionResponse, []types.MessageToolCall, error) {
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
	costBreakdown := p.GeminiProvider.CalculateCost(tokensIn, tokensOut, 0)

	predictResp.Content = textContent
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = time.Since(start)
	predictResp.Raw = respBytes
	predictResp.ToolCalls = toolCalls

	return predictResp, toolCalls, nil
}

func (p *GeminiToolProvider) makeRequest(ctx context.Context, request interface{}) ([]byte, error) {
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL with API key for Gemini
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.BaseURL, p.Model, p.ApiKey)

	// Debug log the request
	var requestObj interface{}
	if err := json.Unmarshal(requestBytes, &requestObj); err != nil {
		// If unmarshal fails for logging, use raw bytes as fallback
		requestObj = string(requestBytes)
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	logger.APIRequest("Gemini-Tools", "POST", url, headers, requestObj)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(requestBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.GetHTTPClient().Do(req)
	if err != nil {
		logger.APIResponse("Gemini-Tools", 0, "", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.APIResponse("Gemini-Tools", resp.StatusCode, "", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Debug log the response
	logger.APIResponse("Gemini-Tools", resp.StatusCode, string(respBytes), nil)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

func init() {
	providers.RegisterProviderFactory("gemini", func(spec providers.ProviderSpec) (providers.Provider, error) {
		return NewGeminiToolProvider(spec.ID, spec.Model, spec.BaseURL, spec.Defaults, spec.IncludeRawOutput), nil
	})
}

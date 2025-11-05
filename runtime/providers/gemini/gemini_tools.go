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

// GeminiToolProvider extends GeminiProvider with tool support
type GeminiToolProvider struct {
	*GeminiProvider
	currentTools   interface{}            // Store current tools for continuation
	currentRequest *providers.ChatRequest // Store current request context for continuation
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

// ChatWithTools performs a chat request with tool support
func (p *GeminiToolProvider) ChatWithTools(ctx context.Context, req providers.ChatRequest, tools interface{}, toolChoice string) (providers.ChatResponse, []types.MessageToolCall, error) {
	logger.Debug("ChatWithTools called",
		"toolChoice", toolChoice,
		"messages", len(req.Messages))

	// Store tools and request context for potential continuation
	p.currentTools = tools
	p.currentRequest = &req

	// Build Gemini request with tools
	geminiReq := p.buildToolRequest(req, tools, toolChoice)

	// Prepare response with raw request if configured (set early to preserve on error)
	chatResp := providers.ChatResponse{}
	if p.includeRawOutput {
		chatResp.RawRequest = geminiReq
	}

	// Make the API call
	respBytes, err := p.makeRequest(ctx, geminiReq)
	if err != nil {
		return chatResp, nil, err
	}

	// Parse response and extract tool calls
	return p.parseToolResponse(respBytes, chatResp)
}

func (p *GeminiToolProvider) buildToolRequest(req providers.ChatRequest, tools interface{}, toolChoice string) map[string]interface{} {
	// Convert messages to Gemini format
	// Gemini requires functionResponse parts to be in a user message immediately after the model message with functionCall
	contents := make([]map[string]interface{}, 0, len(req.Messages))

	// Collect any pending tool results to group together
	var pendingToolResults []map[string]interface{}

	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			// Collect tool result to be added to the next user message or grouped together
			var response interface{}
			if err := json.Unmarshal([]byte(msg.Content), &response); err != nil {
				// If unmarshal fails, wrap the content in an object
				// Gemini requires functionResponse.response to be a Struct (object)
				response = map[string]interface{}{
					"result": msg.Content,
				}
			} else {
				// Successfully unmarshaled, but check if it's a map (object) or primitive
				// Gemini requires responses to be objects
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

			functionResponse := map[string]interface{}{
				"functionResponse": map[string]interface{}{
					"name":     msg.ToolResult.Name,
					"response": response,
				},
			}
			pendingToolResults = append(pendingToolResults, functionResponse)
			continue
		}

		// If we have pending tool results, add them as a user message before this message
		if len(pendingToolResults) > 0 {
			// Tool results must be in a user message
			if msg.Role != "user" {
				// Create a user message with the tool results
				contents = append(contents, map[string]interface{}{
					"role":  "user",
					"parts": pendingToolResults,
				})
				pendingToolResults = nil
			}
		}

		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		parts := make([]interface{}, 0)

		// Add pending tool results first if this is a user message
		if msg.Role == "user" && len(pendingToolResults) > 0 {
			for _, tr := range pendingToolResults {
				parts = append(parts, tr)
			}
			pendingToolResults = nil
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
					// If unmarshal fails, use raw JSON as string fallback
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

		// Skip empty messages
		if len(parts) == 0 {
			continue
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
		request["tools"] = []interface{}{tools}

		// Gemini uses tool_config to control tool usage
		if toolChoice != "" {
			switch toolChoice {
			case "auto":
				request["tool_config"] = map[string]interface{}{
					"function_calling_config": map[string]interface{}{
						"mode": "AUTO", // Let Gemini decide when to use tools or return text
					},
				}
			case "required", "any":
				request["tool_config"] = map[string]interface{}{
					"function_calling_config": map[string]interface{}{
						"mode": "ANY",
					},
				}
			case "none":
				request["tool_config"] = map[string]interface{}{
					"function_calling_config": map[string]interface{}{
						"mode": "NONE",
					},
				}
			default:
				// For specific tool names, use ANY mode (Gemini doesn't support specific tool forcing like OpenAI)
				request["tool_config"] = map[string]interface{}{
					"function_calling_config": map[string]interface{}{
						"mode": "ANY",
					},
				}
			}
		} else {
			// Default to AUTO mode to allow model flexibility
			request["tool_config"] = map[string]interface{}{
				"function_calling_config": map[string]interface{}{
					"mode": "AUTO",
				},
			}
		}
	}

	return request
}

func (p *GeminiToolProvider) parseToolResponse(respBytes []byte, chatResp providers.ChatResponse) (providers.ChatResponse, []types.MessageToolCall, error) {
	start := time.Now()

	var resp geminiToolResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBytes
		return chatResp, nil, fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	if len(resp.Candidates) == 0 {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBytes
		return chatResp, nil, fmt.Errorf("no candidates in Gemini response")
	}

	candidate := resp.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBytes
		// Handle different finish reasons
		switch candidate.FinishReason {
		case "MAX_TOKENS":
			// Don't use fallback - return error to see when this happens
			return chatResp, nil, fmt.Errorf("gemini returned MAX_TOKENS error (this should not happen with reasonable limits)")
		case "SAFETY":
			return chatResp, nil, fmt.Errorf("response blocked by Gemini safety filters")
		case "RECITATION":
			return chatResp, nil, fmt.Errorf("response blocked due to recitation concerns")
		default:
			return chatResp, nil, fmt.Errorf("no parts in Gemini candidate (finish reason: %s)", candidate.FinishReason)
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

	chatResp.Content = textContent
	chatResp.CostInfo = &costBreakdown
	chatResp.Latency = time.Since(start)
	chatResp.Raw = respBytes
	chatResp.ToolCalls = toolCalls

	return chatResp, toolCalls, nil
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

	resp, err := p.Client.Do(req)
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

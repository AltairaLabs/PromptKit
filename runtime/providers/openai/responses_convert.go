// Package openai provides OpenAI LLM provider integration.
package openai

import (
	"encoding/json"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Responses API JSON keys and literal values, named to keep the map-literal
// converters below free of repeated string literals (goconst).
const (
	keyType      = "type"
	keyName      = "name"
	keyRole      = "role"
	keyContent   = "content"
	keyFormat    = "format"
	keyCallID    = "call_id"
	keyStrict    = "strict"
	keyImageURL  = "image_url"
	keyArguments = "arguments"

	typeJSONSchema         = "json_schema"
	typeFunction           = "function"
	typeFunctionCallOutput = "function_call_output"
	typeMessage            = "message"
	typeOutputText         = "output_text"
	typeInputText          = "input_text"
	roleAssistant          = "assistant"

	fcCallIDPrefix          = "fc_"
	chatCallIDPrefix        = "call_"
	responsesProModelSuffix = "-pro"
)

// requiresResponsesAPI returns true if the model is only served by the Responses
// API. OpenAI's "pro" reasoning models (o1-pro, gpt-5-pro, gpt-5.2-pro, ...) all
// 404 on v1/chat/completions and must use v1/responses. This is a genuine,
// OpenAI-defined model property (not a behavior we can default), so it's keyed
// off the "-pro" suffix that those models share rather than enumerated one by one.
func requiresResponsesAPI(model string) bool {
	return strings.HasSuffix(model, responsesProModelSuffix)
}

// transformToResponsesCallID converts a call ID to Responses API format.
// The Responses API requires function call IDs to start with 'fc_'.
// Chat Completions API uses 'call_' prefix which must be transformed.
func transformToResponsesCallID(callID string) string {
	// If already in Responses format, return as-is
	if strings.HasPrefix(callID, fcCallIDPrefix) {
		return callID
	}
	// Transform call_ prefix to fc_ prefix
	if strings.HasPrefix(callID, chatCallIDPrefix) {
		return fcCallIDPrefix + strings.TrimPrefix(callID, chatCallIDPrefix)
	}
	// For any other format, add fc_ prefix
	return fcCallIDPrefix + callID
}

// getAPIMode determines which OpenAI API to use. Priority:
//  1. Explicit config (additional_config.api_mode) — data-driven, always wins.
//  2. requiresResponsesAPI fallback — for Responses-only models when the
//     config doesn't declare a mode.
//  3. Default to the legacy Chat Completions API.
//
// Config-first ordering means a provider config is the source of truth; the
// model-name heuristic is only a best-effort default for undeclared configs.
func getAPIMode(model string, additionalConfig map[string]any) APIMode {
	if additionalConfig != nil {
		if mode, ok := additionalConfig["api_mode"].(string); ok {
			switch strings.ToLower(mode) {
			case "completions", "chat_completions", "legacy":
				return APIModeCompletions
			case "responses":
				return APIModeResponses
			}
		}
	}

	if requiresResponsesAPI(model) {
		return APIModeResponses
	}

	return APIModeCompletions
}

// convertMessagesToResponsesInput converts messages to Responses API input format
// The Responses API expects a flat list where tool calls are separate function_call items
func (p *Provider) convertMessagesToResponsesInput(messages []types.Message) []any {
	// Allocate extra capacity for tool calls which become separate items
	const toolCallCapacityMultiplier = 2
	input := make([]any, 0, len(messages)*toolCallCapacityMultiplier)
	for i := range messages {
		items := p.convertSingleMessageToResponsesInput(&messages[i])
		for _, item := range items {
			input = append(input, item)
		}
	}
	return input
}

// convertSingleMessageToResponsesInput converts a single message to Responses API format
// Returns a slice because assistant messages with tool calls become multiple items
func (p *Provider) convertSingleMessageToResponsesInput(msg *types.Message) []map[string]any {
	// Handle tool results - these are function_call_output items
	// NOTE: The Responses API only supports text output for function_call_output.
	// Multimodal tool results (images, audio) are reduced to text here.
	// Use the Chat Completions API for full multimodal tool result support.
	if msg.Role == roleToolResult && msg.ToolResult != nil {
		// call_id must match the call_id on the corresponding function_call input
		return []map[string]any{{
			keyType:   typeFunctionCallOutput,
			keyCallID: msg.ToolResult.ID,
			"output":  msg.ToolResult.GetTextContent(),
		}}
	}

	// Handle assistant messages with tool calls
	// In Responses API, tool calls become separate function_call items
	if msg.Role == roleAssistant && len(msg.ToolCalls) > 0 {
		return p.assistantToolCallItems(msg)
	}

	// Regular message (user or assistant without tool calls)
	inputMsg := map[string]any{
		keyRole: msg.Role,
	}

	// Handle content (multimodal or simple text)
	inputMsg[keyContent] = p.getMessageContent(msg)

	return []map[string]any{inputMsg}
}

// assistantToolCallItems splits an assistant message that carries tool calls
// into its Responses API items: an optional leading text message followed by
// one function_call item per tool call.
func (p *Provider) assistantToolCallItems(msg *types.Message) []map[string]any {
	items := make([]map[string]any, 0, len(msg.ToolCalls)+1)

	// Add text content as a message if present
	content := msg.GetContent()
	if content != "" {
		items = append(items, map[string]any{
			keyType: typeMessage,
			keyRole: roleAssistant,
			keyContent: []map[string]any{{
				keyType:      typeOutputText,
				partTypeText: content,
			}},
		})
	}

	// Add each tool call as a separate function_call item
	for _, tc := range msg.ToolCalls {
		// Responses API expects arguments as a JSON string, not object
		items = append(items, map[string]any{
			keyType:      typeFunctionCall,
			"id":         transformToResponsesCallID(tc.ID),
			keyCallID:    tc.ID,
			keyName:      tc.Name,
			keyArguments: string(tc.Args),
		})
	}
	return items
}

// getMessageContent extracts content from a message in Responses API format
func (p *Provider) getMessageContent(msg *types.Message) any {
	if len(msg.Parts) == 0 {
		return msg.GetContent()
	}

	parts := make([]map[string]any, 0, len(msg.Parts))
	for i := range msg.Parts {
		if part := p.convertPartToResponsesFormat(&msg.Parts[i]); part != nil {
			parts = append(parts, part)
		}
	}
	return parts
}

// convertPartToResponsesFormat converts a single message part to Responses API format
func (p *Provider) convertPartToResponsesFormat(part *types.ContentPart) map[string]any {
	switch part.Type {
	case partTypeText:
		return map[string]any{
			keyType:      typeInputText,
			partTypeText: part.Text,
		}
	case "image":
		return imageResponsesPart(part.Media)
	}
	return nil
}

// imageResponsesPart renders an image part as a Responses API input_image, or
// nil when there is no usable image source.
func imageResponsesPart(media *types.MediaContent) map[string]any {
	if media == nil {
		return nil
	}
	imageURL := imageURLFromMedia(media)
	if imageURL == "" {
		return nil
	}
	// Responses API expects image_url as a string (the URL directly).
	return map[string]any{
		keyType:     "input_image",
		keyImageURL: imageURL,
	}
}

// imageURLFromMedia resolves an image URL from media, preferring an explicit URL
// and falling back to a base64 data URL. Returns "" when neither is present.
func imageURLFromMedia(media *types.MediaContent) string {
	if media.URL != nil && *media.URL != "" {
		return *media.URL
	}
	if media.Data != nil && *media.Data != "" {
		mimeType := media.MIMEType
		if mimeType == "" {
			mimeType = "image/png" // Default mime type
		}
		return "data:" + mimeType + ";base64," + *media.Data
	}
	return ""
}

// convertToolsToResponsesFormat converts tools to Responses API format
func (p *Provider) convertToolsToResponsesFormat(tools any) []any {
	// Tools format is similar but uses "function" type with slightly different structure
	openAITools, ok := tools.([]openAITool)
	if !ok {
		return nil
	}

	result := make([]any, len(openAITools))
	for i, tool := range openAITools {
		entry := map[string]any{
			keyType:       typeFunction,
			keyName:       tool.Function.Name,
			"description": tool.Function.Description,
			"parameters":  tool.Function.Parameters,
		}
		if tool.Function.Strict {
			entry[keyStrict] = true
		}
		result[i] = entry
	}
	return result
}

// convertResponseFormatToResponses converts response format to Responses API format
func (p *Provider) convertResponseFormatToResponses(rf *providers.ResponseFormat) map[string]any {
	if rf == nil {
		return nil
	}

	result := map[string]any{
		keyFormat: map[string]any{
			keyType: string(rf.Type),
		},
	}

	if rf.Type == providers.ResponseFormatJSONSchema && len(rf.JSONSchema) > 0 {
		var schema any
		if err := json.Unmarshal(rf.JSONSchema, &schema); err == nil {
			schemaName := rf.SchemaName
			if schemaName == "" {
				schemaName = defaultResponseSchema
			}
			result[keyFormat] = map[string]any{
				keyType: typeJSONSchema,
				typeJSONSchema: map[string]any{
					keyName:   schemaName,
					"schema":  schema,
					keyStrict: rf.Strict,
				},
			}
		}
	}

	return result
}

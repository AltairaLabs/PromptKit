package openai

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestRequiresResponsesAPI(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"gpt-5-pro", true},
		{"o1-pro", true},
		{"gpt-5.2-pro", true},
		{"gpt-4o", false},
		{"gpt-4o-realtime-preview", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := requiresResponsesAPI(tt.model); got != tt.want {
			t.Errorf("requiresResponsesAPI(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestTransformToResponsesCallID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"fc_already", "fc_already"},
		{"call_abc123", "fc_abc123"},
		{"weirdformat", "fc_weirdformat"},
		{"", "fc_"},
	}
	for _, tt := range tests {
		if got := transformToResponsesCallID(tt.in); got != tt.want {
			t.Errorf("transformToResponsesCallID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGetAPIMode_ConfigVsHeuristic(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		config map[string]any
		want   APIMode
	}{
		{"nil config, non-pro model defaults to completions", "gpt-4o", nil, APIModeCompletions},
		{"nil config, pro model heuristic → responses", "gpt-5-pro", nil, APIModeResponses},
		{"explicit responses wins", "gpt-4o", map[string]any{"api_mode": "responses"}, APIModeResponses},
		{"explicit completions overrides pro heuristic", "gpt-5-pro", map[string]any{"api_mode": "completions"}, APIModeCompletions},
		{"chat_completions alias", "gpt-4o", map[string]any{"api_mode": "chat_completions"}, APIModeCompletions},
		{"legacy alias", "gpt-4o", map[string]any{"api_mode": "legacy"}, APIModeCompletions},
		{"case-insensitive", "gpt-4o", map[string]any{"api_mode": "RESPONSES"}, APIModeResponses},
		{"unknown api_mode falls through to heuristic (pro)", "gpt-5-pro", map[string]any{"api_mode": "banana"}, APIModeResponses},
		{"unknown api_mode falls through to heuristic (default)", "gpt-4o", map[string]any{"api_mode": "banana"}, APIModeCompletions},
		{"non-string api_mode ignored", "gpt-4o", map[string]any{"api_mode": 42}, APIModeCompletions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getAPIMode(tt.model, tt.config); got != tt.want {
				t.Errorf("getAPIMode(%q, %v) = %q, want %q", tt.model, tt.config, got, tt.want)
			}
		})
	}
}

func TestConvertSingleMessageToResponsesInput_ToolResult(t *testing.T) {
	p := &Provider{}
	tr := types.NewTextToolResult("call_abc", "lookup", "result text")
	msg := &types.Message{Role: roleToolResult, ToolResult: &tr}

	items := p.convertSingleMessageToResponsesInput(msg)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0]["type"] != "function_call_output" {
		t.Errorf("type = %v, want function_call_output", items[0]["type"])
	}
	if items[0]["call_id"] != "call_abc" {
		t.Errorf("call_id = %v, want call_abc", items[0]["call_id"])
	}
	if items[0]["output"] != "result text" {
		t.Errorf("output = %v, want result text", items[0]["output"])
	}
}

func TestConvertSingleMessageToResponsesInput_AssistantToolCallsSplit(t *testing.T) {
	p := &Provider{}
	msg := &types.Message{
		Role:    "assistant",
		Content: "let me check",
		ToolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "get_weather", Args: json.RawMessage(`{"city":"London"}`)},
			{ID: "fc_2", Name: "get_time", Args: json.RawMessage(`{}`)},
		},
	}

	items := p.convertSingleMessageToResponsesInput(msg)
	// 1 message item (text) + 2 function_call items.
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d: %+v", len(items), items)
	}
	if items[0]["type"] != "message" {
		t.Errorf("item[0].type = %v, want message", items[0]["type"])
	}
	if items[1]["type"] != typeFunctionCall {
		t.Errorf("item[1].type = %v, want function_call", items[1]["type"])
	}
	// call_ prefix transformed to fc_ on the id field, call_id preserved.
	if items[1]["id"] != "fc_1" {
		t.Errorf("item[1].id = %v, want fc_1", items[1]["id"])
	}
	if items[1]["call_id"] != "call_1" {
		t.Errorf("item[1].call_id = %v, want call_1", items[1]["call_id"])
	}
	if items[1]["arguments"] != `{"city":"London"}` {
		t.Errorf("item[1].arguments = %v", items[1]["arguments"])
	}
	// Already-fc id preserved.
	if items[2]["id"] != "fc_2" {
		t.Errorf("item[2].id = %v, want fc_2", items[2]["id"])
	}
}

func TestConvertSingleMessageToResponsesInput_AssistantToolCallsNoText(t *testing.T) {
	p := &Provider{}
	msg := &types.Message{
		Role:      "assistant",
		ToolCalls: []types.MessageToolCall{{ID: "call_1", Name: "x", Args: json.RawMessage(`{}`)}},
	}
	items := p.convertSingleMessageToResponsesInput(msg)
	// No text ⇒ no leading message item, just the function_call.
	if len(items) != 1 {
		t.Fatalf("expected 1 item (no text message), got %d", len(items))
	}
	if items[0]["type"] != typeFunctionCall {
		t.Errorf("type = %v, want function_call", items[0]["type"])
	}
}

func TestConvertSingleMessageToResponsesInput_PlainUserMessage(t *testing.T) {
	p := &Provider{}
	msg := &types.Message{Role: "user", Content: "hello"}
	items := p.convertSingleMessageToResponsesInput(msg)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0]["role"] != "user" {
		t.Errorf("role = %v, want user", items[0]["role"])
	}
	if items[0]["content"] != "hello" {
		t.Errorf("content = %v, want hello", items[0]["content"])
	}
}

func TestConvertMessagesToResponsesInput_FlattensAll(t *testing.T) {
	p := &Provider{}
	msgs := []types.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "sure", ToolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "t", Args: json.RawMessage(`{}`)},
		}},
	}
	input := p.convertMessagesToResponsesInput(msgs)
	// user(1) + assistant-text(1) + function_call(1) = 3
	if len(input) != 3 {
		t.Fatalf("expected 3 flattened items, got %d", len(input))
	}
}

func TestGetMessageContent_MultimodalImage(t *testing.T) {
	p := &Provider{}
	text := "look at this"
	data := "aGVsbG8=" // base64 "hello"
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: partTypeText, Text: &text},
			{Type: "image", Media: &types.MediaContent{Data: &data, MIMEType: "image/jpeg"}},
		},
	}

	content := p.getMessageContent(msg)
	parts, ok := content.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0]["type"] != "input_text" {
		t.Errorf("part[0].type = %v, want input_text", parts[0]["type"])
	}
	if parts[1]["type"] != "input_image" {
		t.Errorf("part[1].type = %v, want input_image", parts[1]["type"])
	}
	if parts[1]["image_url"] != "data:image/jpeg;base64,aGVsbG8=" {
		t.Errorf("image_url = %v, want data URL", parts[1]["image_url"])
	}
}

func TestGetMessageContent_NoPartsReturnsText(t *testing.T) {
	p := &Provider{}
	msg := &types.Message{Role: "user", Content: "plain"}
	if got := p.getMessageContent(msg); got != "plain" {
		t.Errorf("getMessageContent = %v, want plain", got)
	}
}

func TestConvertPartToResponsesFormat(t *testing.T) {
	p := &Provider{}
	url := "https://example.com/cat.png"

	tests := []struct {
		name      string
		part      types.ContentPart
		wantNil   bool
		wantType  string
		wantImage string
	}{
		{
			name:     "text part",
			part:     types.ContentPart{Type: partTypeText, Text: strPtr("hi")},
			wantType: "input_text",
		},
		{
			name:      "image with URL",
			part:      types.ContentPart{Type: "image", Media: &types.MediaContent{URL: &url}},
			wantType:  "input_image",
			wantImage: url,
		},
		{
			name:      "image base64 default mime",
			part:      types.ContentPart{Type: "image", Media: &types.MediaContent{Data: strPtr("Zm9v")}},
			wantType:  "input_image",
			wantImage: "data:image/png;base64,Zm9v",
		},
		{
			name:    "image with nil media yields nil",
			part:    types.ContentPart{Type: "image"},
			wantNil: true,
		},
		{
			name:    "image with empty media yields nil",
			part:    types.ContentPart{Type: "image", Media: &types.MediaContent{}},
			wantNil: true,
		},
		{
			name:    "unknown part type yields nil",
			part:    types.ContentPart{Type: "audio"},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.convertPartToResponsesFormat(&tt.part)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got["type"] != tt.wantType {
				t.Errorf("type = %v, want %v", got["type"], tt.wantType)
			}
			if tt.wantImage != "" && got["image_url"] != tt.wantImage {
				t.Errorf("image_url = %v, want %v", got["image_url"], tt.wantImage)
			}
		})
	}
}

func TestConvertToolsToResponsesFormat(t *testing.T) {
	p := &Provider{}

	// Wrong type ⇒ nil.
	if got := p.convertToolsToResponsesFormat("not tools"); got != nil {
		t.Errorf("expected nil for wrong type, got %v", got)
	}

	tools := []openAITool{
		{Type: "function", Function: openAIToolFunction{
			Name: "a", Description: "desc a", Parameters: json.RawMessage(`{"type":"object"}`), Strict: true,
		}},
		{Type: "function", Function: openAIToolFunction{
			Name: "b", Description: "desc b", Parameters: json.RawMessage(`{}`), Strict: false,
		}},
	}
	got := p.convertToolsToResponsesFormat(tools)
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}
	first := got[0].(map[string]any)
	if first["type"] != "function" || first["name"] != "a" {
		t.Errorf("first tool wrong: %+v", first)
	}
	if first["strict"] != true {
		t.Errorf("expected strict=true on first tool")
	}
	second := got[1].(map[string]any)
	if _, hasStrict := second["strict"]; hasStrict {
		t.Errorf("non-strict tool should omit strict key, got %+v", second)
	}
}

func TestConvertResponseFormatToResponses(t *testing.T) {
	p := &Provider{}

	// nil ⇒ nil.
	if got := p.convertResponseFormatToResponses(nil); got != nil {
		t.Errorf("expected nil for nil format, got %v", got)
	}

	// Plain text format.
	textRF := &providers.ResponseFormat{Type: providers.ResponseFormatText}
	got := p.convertResponseFormatToResponses(textRF)
	format := got["format"].(map[string]any)
	if format["type"] != "text" {
		t.Errorf("text format type = %v, want text", format["type"])
	}

	// JSON schema strict mode with explicit name.
	schemaRF := &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`),
		SchemaName: "my_schema",
		Strict:     true,
	}
	got = p.convertResponseFormatToResponses(schemaRF)
	format = got["format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("type = %v, want json_schema", format["type"])
	}
	js := format["json_schema"].(map[string]any)
	if js["name"] != "my_schema" {
		t.Errorf("name = %v, want my_schema", js["name"])
	}
	if js["strict"] != true {
		t.Errorf("strict = %v, want true", js["strict"])
	}
	if js["schema"] == nil {
		t.Errorf("schema should be populated")
	}

	// JSON schema with no name ⇒ default name.
	defRF := &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object"}`),
	}
	got = p.convertResponseFormatToResponses(defRF)
	js = got["format"].(map[string]any)["json_schema"].(map[string]any)
	if js["name"] != defaultResponseSchema {
		t.Errorf("default name = %v, want %v", js["name"], defaultResponseSchema)
	}

	// JSON schema type but empty schema ⇒ falls back to the bare {type:json_schema} format.
	emptyRF := &providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema}
	got = p.convertResponseFormatToResponses(emptyRF)
	format = got["format"].(map[string]any)
	if format["type"] != string(providers.ResponseFormatJSONSchema) {
		t.Errorf("empty-schema format type = %v", format["type"])
	}
	if _, hasJS := format["json_schema"]; hasJS {
		t.Errorf("empty schema should not populate json_schema block")
	}
}

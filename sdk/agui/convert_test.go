package agui

import (
	"encoding/json"
	"testing"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestMessageToAGUI_UserTextMessage(t *testing.T) {
	msg := types.NewUserMessage("Hello, world!")

	result := MessageToAGUI(&msg)

	assert.Equal(t, aguitypes.RoleUser, result.Role)
	assert.Equal(t, "Hello, world!", result.Content)
	assert.NotEmpty(t, result.ID)
	assert.Contains(t, result.ID, "msg-")
	assert.Empty(t, result.ToolCalls)
	assert.Empty(t, result.ToolCallID)
}

func TestMessageToAGUI_AssistantTextMessage(t *testing.T) {
	msg := types.NewAssistantMessage("I can help with that.")

	result := MessageToAGUI(&msg)

	assert.Equal(t, aguitypes.RoleAssistant, result.Role)
	assert.Equal(t, "I can help with that.", result.Content)
	assert.NotEmpty(t, result.ID)
}

func TestMessageToAGUI_SystemMessage(t *testing.T) {
	msg := types.NewSystemMessage("You are a helpful assistant.")

	result := MessageToAGUI(&msg)

	assert.Equal(t, aguitypes.RoleSystem, result.Role)
	assert.Equal(t, "You are a helpful assistant.", result.Content)
}

func TestMessageToAGUI_AssistantWithToolCalls(t *testing.T) {
	msg := types.Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call-123",
				Name: "get_weather",
				Args: json.RawMessage(`{"city":"Seattle"}`),
			},
			{
				ID:   "call-456",
				Name: "get_time",
				Args: json.RawMessage(`{"timezone":"PST"}`),
			},
		},
	}

	result := MessageToAGUI(&msg)

	assert.Equal(t, aguitypes.RoleAssistant, result.Role)
	require.Len(t, result.ToolCalls, 2)

	tc0 := result.ToolCalls[0]
	assert.Equal(t, "call-123", tc0.ID)
	assert.Equal(t, string(aguitypes.ToolCallTypeFunction), tc0.Type)
	assert.Equal(t, "get_weather", tc0.Function.Name)
	assert.Equal(t, `{"city":"Seattle"}`, tc0.Function.Arguments)

	tc1 := result.ToolCalls[1]
	assert.Equal(t, "call-456", tc1.ID)
	assert.Equal(t, "get_time", tc1.Function.Name)
	assert.Equal(t, `{"timezone":"PST"}`, tc1.Function.Arguments)
}

func TestMessageToAGUI_ToolResultMessage(t *testing.T) {
	msg := types.NewToolResultMessage(types.MessageToolResult{
		ID:      "call-123",
		Name:    "get_weather",
		Content: `{"temperature":72,"condition":"sunny"}`,
	})

	result := MessageToAGUI(&msg)

	assert.Equal(t, aguitypes.RoleTool, result.Role)
	assert.Equal(t, "call-123", result.ToolCallID)
	assert.Equal(t, `{"temperature":72,"condition":"sunny"}`, result.Content)
}

func TestMessageToAGUI_MultimodalWithImage(t *testing.T) {
	url := "https://example.com/image.png"
	text := "What is in this image?"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &text},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &url,
					MIMEType: "image/png",
				},
			},
		},
	}

	result := MessageToAGUI(&msg)

	assert.Equal(t, aguitypes.RoleUser, result.Role)

	contents, ok := result.Content.([]aguitypes.InputContent)
	require.True(t, ok, "expected content to be []InputContent")
	require.Len(t, contents, 2)

	assert.Equal(t, string(aguitypes.InputContentTypeText), contents[0].Type)
	assert.Equal(t, "What is in this image?", contents[0].Text)

	assert.Equal(t, string(aguitypes.InputContentTypeBinary), contents[1].Type)
	assert.Equal(t, "image/png", contents[1].MimeType)
	assert.Equal(t, "https://example.com/image.png", contents[1].URL)
}

func TestMessageToAGUI_MultimodalWithBase64Image(t *testing.T) {
	data := "iVBORw0KGgoAAAANSUhEUg=="
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data:     &data,
					MIMEType: "image/png",
				},
			},
		},
	}

	result := MessageToAGUI(&msg)

	contents, ok := result.Content.([]aguitypes.InputContent)
	require.True(t, ok)
	require.Len(t, contents, 1)
	assert.Equal(t, string(aguitypes.InputContentTypeBinary), contents[0].Type)
	assert.Equal(t, "iVBORw0KGgoAAAANSUhEUg==", contents[0].Data)
	assert.Equal(t, "image/png", contents[0].MimeType)
}

func TestMessageFromAGUI_UserTextMessage(t *testing.T) {
	msg := aguitypes.Message{
		ID:      "msg-abc",
		Role:    aguitypes.RoleUser,
		Content: "Hello from AG-UI",
	}

	result := MessageFromAGUI(&msg)

	assert.Equal(t, "user", result.Role)
	assert.Equal(t, "Hello from AG-UI", result.Content)
	assert.Nil(t, result.ToolResult)
	assert.Empty(t, result.ToolCalls)
}

func TestMessageFromAGUI_AssistantTextMessage(t *testing.T) {
	msg := aguitypes.Message{
		ID:      "msg-def",
		Role:    aguitypes.RoleAssistant,
		Content: "Here is your answer.",
	}

	result := MessageFromAGUI(&msg)

	assert.Equal(t, "assistant", result.Role)
	assert.Equal(t, "Here is your answer.", result.Content)
}

func TestMessageFromAGUI_SystemMessage(t *testing.T) {
	msg := aguitypes.Message{
		ID:      "msg-sys",
		Role:    aguitypes.RoleSystem,
		Content: "System prompt",
	}

	result := MessageFromAGUI(&msg)

	assert.Equal(t, "system", result.Role)
	assert.Equal(t, "System prompt", result.Content)
}

func TestMessageFromAGUI_DeveloperMessage(t *testing.T) {
	msg := aguitypes.Message{
		ID:      "msg-dev",
		Role:    aguitypes.RoleDeveloper,
		Content: "Developer instructions",
	}

	result := MessageFromAGUI(&msg)

	assert.Equal(t, "system", result.Role)
	assert.Equal(t, "Developer instructions", result.Content)
}

func TestMessageFromAGUI_ToolResultMessage(t *testing.T) {
	msg := aguitypes.Message{
		ID:         "msg-tool",
		Role:       aguitypes.RoleTool,
		Content:    `{"result":"success"}`,
		ToolCallID: "call-789",
	}

	result := MessageFromAGUI(&msg)

	assert.Equal(t, "tool", result.Role)
	assert.Equal(t, `{"result":"success"}`, result.Content)
	require.NotNil(t, result.ToolResult)
	assert.Equal(t, "call-789", result.ToolResult.ID)
	assert.Equal(t, `{"result":"success"}`, result.ToolResult.Content)
}

func TestMessageFromAGUI_AssistantWithToolCalls(t *testing.T) {
	msg := aguitypes.Message{
		ID:      "msg-tc",
		Role:    aguitypes.RoleAssistant,
		Content: "",
		ToolCalls: []aguitypes.ToolCall{
			{
				ID:   "call-abc",
				Type: string(aguitypes.ToolCallTypeFunction),
				Function: aguitypes.FunctionCall{
					Name:      "search",
					Arguments: `{"query":"test"}`,
				},
			},
		},
	}

	result := MessageFromAGUI(&msg)

	assert.Equal(t, "assistant", result.Role)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "call-abc", result.ToolCalls[0].ID)
	assert.Equal(t, "search", result.ToolCalls[0].Name)
	assert.Equal(t, json.RawMessage(`{"query":"test"}`), result.ToolCalls[0].Args)
}

func TestMessageFromAGUI_MultimodalContent(t *testing.T) {
	msg := aguitypes.Message{
		ID:   "msg-mm",
		Role: aguitypes.RoleUser,
		Content: []aguitypes.InputContent{
			{Type: string(aguitypes.InputContentTypeText), Text: "Describe this image"},
			{
				Type:     string(aguitypes.InputContentTypeBinary),
				MimeType: "image/jpeg",
				URL:      "https://example.com/photo.jpg",
			},
		},
	}

	result := MessageFromAGUI(&msg)

	assert.Equal(t, "user", result.Role)
	require.Len(t, result.Parts, 2)

	assert.Equal(t, types.ContentTypeText, result.Parts[0].Type)
	require.NotNil(t, result.Parts[0].Text)
	assert.Equal(t, "Describe this image", *result.Parts[0].Text)

	assert.Equal(t, types.ContentTypeImage, result.Parts[1].Type)
	require.NotNil(t, result.Parts[1].Media)
	assert.Equal(t, "image/jpeg", result.Parts[1].Media.MIMEType)
	require.NotNil(t, result.Parts[1].Media.URL)
	assert.Equal(t, "https://example.com/photo.jpg", *result.Parts[1].Media.URL)
}

func TestMessageFromAGUI_MultimodalWithBase64(t *testing.T) {
	msg := aguitypes.Message{
		ID:   "msg-b64",
		Role: aguitypes.RoleUser,
		Content: []aguitypes.InputContent{
			{
				Type:     string(aguitypes.InputContentTypeBinary),
				MimeType: "image/png",
				Data:     "base64encodeddata",
			},
		},
	}

	result := MessageFromAGUI(&msg)

	require.Len(t, result.Parts, 1)
	assert.Equal(t, types.ContentTypeImage, result.Parts[0].Type)
	require.NotNil(t, result.Parts[0].Media)
	require.NotNil(t, result.Parts[0].Media.Data)
	assert.Equal(t, "base64encodeddata", *result.Parts[0].Media.Data)
}

func TestRoundTrip_UserTextMessage(t *testing.T) {
	original := types.NewUserMessage("Round trip test")

	aguiMsg := MessageToAGUI(&original)
	roundTripped := MessageFromAGUI(&aguiMsg)

	assert.Equal(t, original.Role, roundTripped.Role)
	assert.Equal(t, original.Content, roundTripped.Content)
}

func TestRoundTrip_AssistantWithToolCalls(t *testing.T) {
	original := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{
				ID:   "call-rt",
				Name: "lookup",
				Args: json.RawMessage(`{"key":"value"}`),
			},
		},
	}

	aguiMsg := MessageToAGUI(&original)
	roundTripped := MessageFromAGUI(&aguiMsg)

	assert.Equal(t, original.Role, roundTripped.Role)
	require.Len(t, roundTripped.ToolCalls, 1)
	assert.Equal(t, original.ToolCalls[0].ID, roundTripped.ToolCalls[0].ID)
	assert.Equal(t, original.ToolCalls[0].Name, roundTripped.ToolCalls[0].Name)
	assert.Equal(t, string(original.ToolCalls[0].Args), string(roundTripped.ToolCalls[0].Args))
}

func TestRoundTrip_ToolResultMessage(t *testing.T) {
	original := types.NewToolResultMessage(types.MessageToolResult{
		ID:      "call-rt2",
		Name:    "fetch",
		Content: "result data",
	})

	aguiMsg := MessageToAGUI(&original)
	roundTripped := MessageFromAGUI(&aguiMsg)

	assert.Equal(t, "tool", roundTripped.Role)
	require.NotNil(t, roundTripped.ToolResult)
	assert.Equal(t, "call-rt2", roundTripped.ToolResult.ID)
	assert.Equal(t, "result data", roundTripped.ToolResult.Content)
}

func TestRoundTrip_MultimodalImageURL(t *testing.T) {
	url := "https://example.com/test.png"
	text := "Check this"
	original := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &text},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &url,
					MIMEType: "image/png",
				},
			},
		},
	}

	aguiMsg := MessageToAGUI(&original)
	roundTripped := MessageFromAGUI(&aguiMsg)

	assert.Equal(t, "user", roundTripped.Role)
	require.Len(t, roundTripped.Parts, 2)
	assert.Equal(t, types.ContentTypeText, roundTripped.Parts[0].Type)
	require.NotNil(t, roundTripped.Parts[0].Text)
	assert.Equal(t, "Check this", *roundTripped.Parts[0].Text)
	assert.Equal(t, types.ContentTypeImage, roundTripped.Parts[1].Type)
	require.NotNil(t, roundTripped.Parts[1].Media)
	require.NotNil(t, roundTripped.Parts[1].Media.URL)
	assert.Equal(t, "https://example.com/test.png", *roundTripped.Parts[1].Media.URL)
	assert.Equal(t, "image/png", roundTripped.Parts[1].Media.MIMEType)
}

func TestMessagesToAGUI(t *testing.T) {
	msgs := []types.Message{
		types.NewSystemMessage("Be helpful."),
		types.NewUserMessage("Hi"),
		types.NewAssistantMessage("Hello!"),
	}

	result := MessagesToAGUI(msgs)

	require.Len(t, result, 3)
	assert.Equal(t, aguitypes.RoleSystem, result[0].Role)
	assert.Equal(t, aguitypes.RoleUser, result[1].Role)
	assert.Equal(t, aguitypes.RoleAssistant, result[2].Role)
	assert.Equal(t, "Be helpful.", result[0].Content)
	assert.Equal(t, "Hi", result[1].Content)
	assert.Equal(t, "Hello!", result[2].Content)
}

func TestMessagesFromAGUI(t *testing.T) {
	msgs := []aguitypes.Message{
		{ID: "1", Role: aguitypes.RoleSystem, Content: "System"},
		{ID: "2", Role: aguitypes.RoleUser, Content: "User"},
		{ID: "3", Role: aguitypes.RoleAssistant, Content: "Assistant"},
	}

	result := MessagesFromAGUI(msgs)

	require.Len(t, result, 3)
	assert.Equal(t, "system", result[0].Role)
	assert.Equal(t, "user", result[1].Role)
	assert.Equal(t, "assistant", result[2].Role)
	assert.Equal(t, "System", result[0].Content)
	assert.Equal(t, "User", result[1].Content)
	assert.Equal(t, "Assistant", result[2].Content)
}

func TestToolsFromAGUI(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"city": map[string]any{
				"type":        "string",
				"description": "The city name",
			},
		},
		"required": []any{"city"},
	}

	aguiTools := []aguitypes.Tool{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a city",
			Parameters:  params,
		},
		{
			Name:        "search",
			Description: "Search the web",
			Parameters:  nil,
		},
	}

	result := ToolsFromAGUI(aguiTools)

	require.Len(t, result, 2)

	assert.Equal(t, "get_weather", result[0].Name)
	assert.Equal(t, "Get the current weather for a city", result[0].Description)
	assert.NotNil(t, result[0].InputSchema)

	// Verify schema is valid JSON
	var schema map[string]any
	err := json.Unmarshal(result[0].InputSchema, &schema)
	require.NoError(t, err)
	assert.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasCityProp := props["city"]
	assert.True(t, hasCityProp)

	assert.Equal(t, "search", result[1].Name)
	assert.Equal(t, "Search the web", result[1].Description)
	assert.Nil(t, result[1].InputSchema)
}

func TestToolsFromAGUI_Empty(t *testing.T) {
	result := ToolsFromAGUI([]aguitypes.Tool{})
	assert.Empty(t, result)
}

func TestMessageToAGUI_EmptyContent(t *testing.T) {
	msg := types.Message{
		Role:    "user",
		Content: "",
	}

	result := MessageToAGUI(&msg)

	assert.Equal(t, aguitypes.RoleUser, result.Role)
	assert.Equal(t, "", result.Content)
}

func TestMessageFromAGUI_NilContent(t *testing.T) {
	msg := aguitypes.Message{
		ID:      "msg-nil",
		Role:    aguitypes.RoleUser,
		Content: nil,
	}

	result := MessageFromAGUI(&msg)

	assert.Equal(t, "user", result.Role)
	assert.Equal(t, "", result.Content)
}

func TestMapRoleToAGUI_UnknownRole(t *testing.T) {
	result := mapRoleToAGUI("unknown")
	assert.Equal(t, aguitypes.RoleUser, result)
}

func TestMapRoleFromAGUI_UnknownRole(t *testing.T) {
	result := mapRoleFromAGUI("custom_role")
	assert.Equal(t, "user", result)
}

func TestContentString_NonStringContent(t *testing.T) {
	msg := aguitypes.Message{
		Content: map[string]any{"key": "value"},
	}

	result := contentString(&msg)
	assert.Contains(t, result, "key")
	assert.Contains(t, result, "value")
}

func TestMessagesToAGUI_Empty(t *testing.T) {
	result := MessagesToAGUI([]types.Message{})
	assert.Empty(t, result)
}

func TestMessagesFromAGUI_Empty(t *testing.T) {
	result := MessagesFromAGUI([]aguitypes.Message{})
	assert.Empty(t, result)
}

func TestToolsFromAGUI_WithParameters(t *testing.T) {
	tool := aguitypes.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}

	result := ToolsFromAGUI([]aguitypes.Tool{tool})
	require.Len(t, result, 1)

	// Verify the InputSchema is valid JSON with expected structure
	var schema map[string]any
	err := json.Unmarshal(result[0].InputSchema, &schema)
	require.NoError(t, err)
	assert.Equal(t, "object", schema["type"])
}

func TestToolFromAGUI_CorrectType(t *testing.T) {
	tool := aguitypes.Tool{
		Name:        "my_tool",
		Description: "desc",
	}

	result := toolFromAGUI(tool)

	assert.IsType(t, &tools.ToolDescriptor{}, result)
	assert.Equal(t, "my_tool", result.Name)
	assert.Equal(t, "desc", result.Description)
}

func TestContentPartToAGUI_TextNilText(t *testing.T) {
	part := types.ContentPart{Type: types.ContentTypeText, Text: nil}
	_, ok := contentPartToAGUI(part)
	assert.False(t, ok)
}

func TestContentPartToAGUI_ImageNilMedia(t *testing.T) {
	part := types.ContentPart{Type: types.ContentTypeImage, Media: nil}
	_, ok := contentPartToAGUI(part)
	assert.False(t, ok)
}

func TestContentPartToAGUI_UnknownType(t *testing.T) {
	part := types.ContentPart{Type: "video"}
	_, ok := contentPartToAGUI(part)
	assert.False(t, ok)
}

func TestTryDecodeInputContents_NonSlice(t *testing.T) {
	result := tryDecodeInputContents("not a slice")
	assert.Nil(t, result)
}

func TestTryDecodeInputContents_UnrecognizedTypes(t *testing.T) {
	// Slice of maps with unrecognized type fields
	input := []any{
		map[string]any{"type": "unknown_type", "text": "hello"},
	}
	result := tryDecodeInputContents(input)
	assert.Nil(t, result)
}

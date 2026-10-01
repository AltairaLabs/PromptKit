package agui

import (
	"context"
	"encoding/json"
	"testing"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover the AG-UI 1.0 message rules: run-input.mdx (messages and their
// content parts), events/tool-calls.mdx (tool messages and frontend tools) and
// the schema's message and part definitions.

// decodeMessage decodes a message from the wire, as a server receives it.
func decodeMessage(t *testing.T, raw string) aguitypes.Message {
	t.Helper()
	var msg aguitypes.Message
	require.NoError(t, json.Unmarshal([]byte(raw), &msg))
	return msg
}

func strPtr(s string) *string { return &s }

// --- Tool message error, both directions ---

func TestMessageFromAGUI_ToolErrorKept(t *testing.T) {
	msg := decodeMessage(t, `{"id":"m","role":"tool","toolCallId":"c1","content":"partial","error":"timeout"}`)

	result := MessageFromAGUI(&msg)

	require.NotNil(t, result.ToolResult)
	assert.Equal(t, "timeout", result.ToolResult.Error)
	assert.Equal(t, "partial", result.ToolResult.GetTextContent(), "a partial result survives a failure")
}

func TestMessageFromAGUI_ToolErrorWithoutContent(t *testing.T) {
	msg := decodeMessage(t, `{"id":"m","role":"tool","toolCallId":"c1","content":"","error":"permission denied"}`)

	result := MessageFromAGUI(&msg)

	require.NotNil(t, result.ToolResult)
	assert.Equal(t, "permission denied", result.ToolResult.Error)
	assert.Equal(t, "Tool execution failed: permission denied", result.ToolResult.GetTextContent(),
		"the model is told why the call failed, in the runtime's own words")
}

func TestMessageToAGUI_ToolErrorKept(t *testing.T) {
	r := types.NewTextToolResult("c1", "lookup", "Tool execution failed: timeout")
	r.Error = "timeout"
	msg := types.NewToolResultMessage(r)

	result := MessageToAGUI(&msg)

	assert.Equal(t, "timeout", result.Error)
	assert.Equal(t, "Tool execution failed: timeout", result.Content)
}

// --- Roles ---

// AG-UI's reasoning role is the model's own reasoning. Mapping it to "user"
// replayed the model's thoughts as user input on the next run.
func TestMessageFromAGUI_ReasoningIsNotUserInput(t *testing.T) {
	msg := aguitypes.Message{ID: "r", Role: aguitypes.RoleReasoning, Content: "Let me think."}

	result := MessageFromAGUI(&msg)

	assert.Equal(t, "assistant", result.Role)
	assert.Empty(t, result.Content)
	require.NotNil(t, result.Reasoning)
	assert.Equal(t, "Let me think.", result.Reasoning.Text)
}

func TestMessagesFromAGUI_FoldsReasoningAndDropsActivity(t *testing.T) {
	msgs := []aguitypes.Message{
		{ID: "1", Role: aguitypes.RoleUser, Content: "Hi"},
		{ID: "2", Role: aguitypes.RoleReasoning, Content: "The user greeted me."},
		{ID: "3", Role: aguitypes.RoleActivity, Content: map[string]any{"progress": 1}},
		{ID: "4", Role: aguitypes.RoleAssistant, Content: "Hello!"},
		{ID: "5", Role: aguitypes.RoleReasoning, Content: "trailing"},
	}

	result := MessagesFromAGUI(msgs)

	require.Len(t, result, 2)
	assert.Equal(t, "user", result[0].Role)
	assert.Equal(t, "assistant", result[1].Role)
	assert.Equal(t, "Hello!", result[1].Content)
	require.NotNil(t, result[1].Reasoning)
	assert.Equal(t, "The user greeted me.", result[1].Reasoning.Text)
}

// --- Inbound 1.0 media parts ---

// An image-only message used to become user text holding the raw JSON of its
// parts, because only text and binary parts were recognized.
func TestMessageFromAGUI_ImageOnlyMessage(t *testing.T) {
	msg := decodeMessage(t, `{"id":"m","role":"user","content":[
		{"type":"image","source":{"type":"url","value":"https://example.com/cat.png","mimeType":"image/png"}}]}`)

	result := MessageFromAGUI(&msg)

	assert.Empty(t, result.Content, "no raw JSON in the text")
	require.Len(t, result.Parts, 1)
	assert.Equal(t, types.ContentTypeImage, result.Parts[0].Type)
	require.NotNil(t, result.Parts[0].Media.URL)
	assert.Equal(t, "https://example.com/cat.png", *result.Parts[0].Media.URL)
	assert.Equal(t, "image/png", result.Parts[0].Media.MIMEType)
}

// Text plus an image used to drop the image.
func TestMessageFromAGUI_TextAndMediaParts(t *testing.T) {
	msg := decodeMessage(t, `{"id":"m","role":"user","content":[
		{"type":"text","text":"What is this?"},
		{"type":"image","source":{"type":"data","value":"aGVsbG8=","mimeType":"image/jpeg"}},
		{"type":"audio","source":{"type":"url","value":"https://example.com/a.wav","mimeType":"audio/wav"}},
		{"type":"video","source":{"type":"url","value":"https://example.com/v.mp4"}},
		{"type":"document","source":{"type":"data","value":"JVBERg==","mimeType":"application/pdf"}}]}`)

	result := MessageFromAGUI(&msg)

	require.Len(t, result.Parts, 5)
	assert.Equal(t, "What is this?", *result.Parts[0].Text)
	assert.Equal(t, types.ContentTypeImage, result.Parts[1].Type)
	assert.Equal(t, "aGVsbG8=", *result.Parts[1].Media.Data)
	assert.Equal(t, types.ContentTypeAudio, result.Parts[2].Type)
	assert.Equal(t, types.ContentTypeVideo, result.Parts[3].Type)
	assert.Equal(t, types.ContentTypeDocument, result.Parts[4].Type)
	assert.Equal(t, "application/pdf", result.Parts[4].Media.MIMEType)
}

// A provider file handle cannot be resolved here; AG-UI says skip the part and
// run, never fail.
func TestMessageFromAGUI_FileSourceSkipped(t *testing.T) {
	msg := decodeMessage(t, `{"id":"m","role":"user","content":[
		{"type":"text","text":"Summarize"},
		{"type":"document","source":{"type":"file","value":"file-abc","provider":"openai"}}]}`)

	result := MessageFromAGUI(&msg)

	require.Len(t, result.Parts, 1)
	assert.Equal(t, types.ContentTypeText, result.Parts[0].Type)
}

// A legacy binary part takes its content type from its MIME type: audio/wav
// used to become an image.
func TestMessageFromAGUI_LegacyBinaryByMIME(t *testing.T) {
	cases := map[string]string{
		"audio/wav":       types.ContentTypeAudio,
		"video/mp4":       types.ContentTypeVideo,
		"image/png":       types.ContentTypeImage,
		"application/pdf": types.ContentTypeDocument,
	}
	for mime, want := range cases {
		t.Run(mime, func(t *testing.T) {
			msg := aguitypes.Message{Role: aguitypes.RoleUser, Content: []aguitypes.InputContent{
				{Type: aguitypes.InputContentTypeBinary, MimeType: mime, URL: "https://example.com/x"},
			}}
			result := MessageFromAGUI(&msg)
			require.Len(t, result.Parts, 1)
			assert.Equal(t, want, result.Parts[0].Type)
		})
	}
}

func TestMessageFromAGUI_BinaryWithOnlyIDSkipped(t *testing.T) {
	msg := aguitypes.Message{Role: aguitypes.RoleUser, Content: []aguitypes.InputContent{
		{Type: aguitypes.InputContentTypeText, Text: "hi"},
		{Type: aguitypes.InputContentTypeBinary, MimeType: "image/png", ID: "upload-1"},
	}}

	result := MessageFromAGUI(&msg)
	require.Len(t, result.Parts, 1, "a part with no bytes to use is skipped")
}

func TestMessageFromAGUI_ToolMessageWithParts(t *testing.T) {
	msg := decodeMessage(t, `{"id":"m","role":"tool","toolCallId":"c1","content":[
		{"type":"text","text":"Screenshot attached."},
		{"type":"image","source":{"type":"data","value":"aGVsbG8=","mimeType":"image/png"}}]}`)

	result := MessageFromAGUI(&msg)

	require.NotNil(t, result.ToolResult)
	assert.Equal(t, "c1", result.ToolResult.ID)
	require.Len(t, result.ToolResult.Parts, 2)
	assert.Equal(t, types.ContentTypeImage, result.ToolResult.Parts[1].Type)
	assert.Equal(t, "Screenshot attached.", result.Content)
}

// --- Outbound: no retired binary part, no lost media ---

func TestMessageToAGUI_AllMediaTypes(t *testing.T) {
	msg := types.Message{Role: "user", Parts: []types.ContentPart{
		{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: strPtr("UklGRg=="), MIMEType: "audio/wav"}},
		{Type: types.ContentTypeVideo, Media: &types.MediaContent{URL: strPtr("https://example.com/v.mp4"), MIMEType: "video/mp4"}},
		{Type: types.ContentTypeDocument, Media: &types.MediaContent{Data: strPtr("JVBERg=="), MIMEType: "application/pdf"}},
	}}

	result := MessageToAGUI(&msg)

	contents, ok := result.Content.([]aguitypes.InputContent)
	require.True(t, ok)
	require.Len(t, contents, 3)
	assert.Equal(t, aguitypes.InputContentTypeAudio, contents[0].Type)
	assert.Equal(t, aguitypes.InputContentTypeVideo, contents[1].Type)
	assert.Equal(t, aguitypes.InputContentTypeDocument, contents[2].Type)
	assert.Equal(t, "application/pdf", contents[2].Source.MimeType)

	wire, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(wire), `"binary"`, "a strict 1.0 consumer rejects the retired binary part")
}

func TestMessageToAGUI_MediaWithoutDataOrURLDropped(t *testing.T) {
	text := "see file"
	msg := types.Message{Role: "user", Parts: []types.ContentPart{
		{Type: types.ContentTypeText, Text: &text},
		{Type: types.ContentTypeImage, Media: &types.MediaContent{FilePath: strPtr("/tmp/x.png"), MIMEType: "image/png"}},
	}}

	result := MessageToAGUI(&msg)

	contents, ok := result.Content.([]aguitypes.InputContent)
	require.True(t, ok)
	require.Len(t, contents, 1)
	assert.Equal(t, aguitypes.InputContentTypeText, contents[0].Type)
}

// AG-UI 1.0 assistant content is a string; parts are reserved for a later
// version.
func TestMessageToAGUI_AssistantContentIsText(t *testing.T) {
	text := "Here you go."
	msg := types.Message{Role: "assistant", Parts: []types.ContentPart{
		{Type: types.ContentTypeText, Text: &text},
		{Type: types.ContentTypeImage, Media: &types.MediaContent{URL: strPtr("https://example.com/i.png"), MIMEType: "image/png"}},
	}}

	result := MessageToAGUI(&msg)
	assert.Equal(t, "Here you go.", result.Content)
}

func TestMessageToAGUI_UserTextPartsStayText(t *testing.T) {
	text := "plain"
	msg := types.Message{Role: "user", Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &text}}}

	result := MessageToAGUI(&msg)
	assert.Equal(t, "plain", result.Content)
}

func TestMessageToAGUI_ToolResultWithMedia(t *testing.T) {
	text := "chart"
	r := types.MessageToolResult{ID: "c1", Parts: []types.ContentPart{
		{Type: types.ContentTypeText, Text: &text},
		{Type: types.ContentTypeImage, Media: &types.MediaContent{Data: strPtr("aGVsbG8="), MIMEType: "image/png"}},
	}}
	msg := types.NewToolResultMessage(r)

	result := MessageToAGUI(&msg)

	contents, ok := result.Content.([]aguitypes.InputContent)
	require.True(t, ok, "a tool result carrying media travels as parts")
	require.Len(t, contents, 2)
	assert.Equal(t, aguitypes.InputContentTypeImage, contents[1].Type)
}

// --- Frontend tools ---

// ToolsFromAGUI used to return Mode "", which the registry routes to the mock
// executor: a frontend tool call got a canned mock result.
func TestToolsFromAGUI_RoutesToClientExecutor(t *testing.T) {
	descs := ToolsFromAGUI([]aguitypes.Tool{{Name: "confirm_order", Description: "Ask the user"}})

	require.Len(t, descs, 1)
	assert.Equal(t, "client", descs[0].Mode)

	// A conversation registers its client executor under "client"; the call
	// must reach it rather than a mock.
	reg := tools.NewRegistry()
	client := &recordingExecutor{name: "client"}
	reg.RegisterExecutor(client)
	require.NoError(t, reg.Register(descs[0]))
	_, err := reg.Execute(t.Context(), "confirm_order", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"confirm_order"}, client.calls)
}

// recordingExecutor is a tools.Executor that records the tools it ran.
type recordingExecutor struct {
	name  string
	calls []string
}

func (r *recordingExecutor) Name() string { return r.name }

func (r *recordingExecutor) Execute(
	_ context.Context, desc *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	r.calls = append(r.calls, desc.Name)
	return json.RawMessage(`"ok"`), nil
}

func TestToolResultsFromAGUI(t *testing.T) {
	msgs := []aguitypes.Message{
		{ID: "1", Role: aguitypes.RoleUser, Content: "Order it"},
		{ID: "2", Role: aguitypes.RoleAssistant, ToolCalls: []aguitypes.ToolCall{
			{ID: "c1", Type: "function", Function: aguitypes.FunctionCall{Name: "confirm", Arguments: "{}"}},
			{ID: "c2", Type: "function", Function: aguitypes.FunctionCall{Name: "locate", Arguments: "{}"}},
			{ID: "c3", Type: "function", Function: aguitypes.FunctionCall{Name: "pay", Arguments: "{}"}},
			{ID: "c4", Type: "function", Function: aguitypes.FunctionCall{Name: "note", Arguments: "{}"}},
		}},
		{ID: "3", Role: aguitypes.RoleTool, ToolCallID: "c1", Content: `{"confirmed":true}`},
		{ID: "4", Role: aguitypes.RoleTool, ToolCallID: "c2", Content: "in Paris"},
		{ID: "5", Role: aguitypes.RoleTool, ToolCallID: "c3", Content: "", Error: "card declined"},
		{ID: "6", Role: aguitypes.RoleTool, ToolCallID: "c4", Content: []any{
			map[string]any{"type": "text", "text": "noted"},
		}},
	}

	results := ToolResultsFromAGUI(msgs)

	require.Len(t, results, 4)
	assert.Equal(t, ToolResult{CallID: "c1", Result: json.RawMessage(`{"confirmed":true}`)}, results[0])
	assert.Equal(t, ToolResult{CallID: "c2", Result: "in Paris"}, results[1])
	assert.Equal(t, ToolResult{CallID: "c3", Error: "card declined"}, results[2])
	parts, ok := results[3].Result.([]types.ContentPart)
	require.True(t, ok)
	assert.Equal(t, "noted", *parts[0].Text)
}

// Tool messages earlier in the history answered calls a previous run already
// resumed with; only a trailing run of them is pending.
func TestToolResultsFromAGUI_OnlyTrailingToolMessages(t *testing.T) {
	answered := []aguitypes.Message{
		{Role: aguitypes.RoleTool, ToolCallID: "old", Content: "done"},
		{Role: aguitypes.RoleAssistant, Content: "Thanks."},
		{Role: aguitypes.RoleUser, Content: "next"},
	}
	assert.Len(t, ToolResultsFromAGUI(answered), 0)

	pending := append(append([]aguitypes.Message{}, answered...),
		aguitypes.Message{Role: aguitypes.RoleTool, ToolCallID: "new", Content: "x"})
	got := ToolResultsFromAGUI(pending)
	require.Len(t, got, 1)
	assert.Equal(t, "new", got[0].CallID)
}

func TestContentString_PartsYieldText(t *testing.T) {
	msg := aguitypes.Message{Content: []any{
		map[string]any{"type": "text", "text": "a"},
		map[string]any{"type": "image", "source": map[string]any{"type": "url", "value": "https://x"}},
		map[string]any{"type": "text", "text": "b"},
	}}
	assert.Equal(t, "ab", contentString(&msg))
}

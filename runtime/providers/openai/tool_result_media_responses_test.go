package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// responsesOutputOf sends a round whose snapshot call returned parts through
// the Responses API tool path and returns the function_call_output item the
// request carried for it.
func responsesOutputOf(t *testing.T, parts []types.ContentPart) map[string]any {
	t.Helper()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		_, _ = w.Write([]byte(`{"id":"r","object":"response","status":"completed","output":[{"type":"message",` +
			`"role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "k")
	tp := NewToolProvider("media", "gpt-4o", srv.URL, providers.ProviderDefaults{}, false,
		map[string]any{"api_mode": "responses"}, nil)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "snapshot", Description: "snapshot", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	require.NoError(t, err)
	_, _, err = tp.PredictWithTools(context.Background(), providers.PredictionRequest{MaxTokens: 64, Messages: []types.Message{
		{Role: "user", Content: "check the yard"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "call-1", Name: "snapshot", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call-1", Name: "snapshot", Parts: parts}},
	}}, tools, "auto")
	require.NoError(t, err)
	for _, item := range body["input"].([]any) {
		m := item.(map[string]any)
		if m["type"] == "function_call_output" {
			return m
		}
	}
	t.Fatalf("no function_call_output in %v", body["input"])
	return nil
}

// The Responses API takes text, images and files in a function call's output,
// so a tool's image and document reach the model there, in order, instead of
// being reduced to the result's text.
func TestResponsesToolResult_CarriesImagesAndDocuments(t *testing.T) {
	out := responsesOutputOf(t, []types.ContentPart{
		types.NewTextPart("camera 3"),
		types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil),
		types.NewDocumentPartFromData("cGRm", "application/pdf"),
	})

	assert.Equal(t, "call-1", out["call_id"])
	items, ok := out["output"].([]any)
	require.True(t, ok, "a result with media is sent as a list, got %#v", out["output"])
	require.Len(t, items, 3)
	assert.Equal(t, map[string]any{"type": "input_text", "text": "camera 3"}, items[0])
	assert.Equal(t, map[string]any{"type": "input_image", "image_url": "data:image/png;base64,aW1n"}, items[1])
	assert.Equal(t, map[string]any{
		"type": "input_file", "filename": "snapshot-call-1-3.pdf", "file_data": "data:application/pdf;base64,cGRm",
	}, items[2])
}

// A text-only result stays a plain string.
func TestResponsesToolResult_TextOnlyIsAString(t *testing.T) {
	out := responsesOutputOf(t, []types.ContentPart{types.NewTextPart("all clear")})
	assert.Equal(t, "all clear", out["output"])
}

// Media the Responses API does not take (video) is described in the output,
// naming where it is stored.
func TestResponsesToolResult_VideoIsDescribed(t *testing.T) {
	out := responsesOutputOf(t, []types.ContentPart{
		types.NewTextPart("camera 3"),
		types.NewVideoPartFromStorageRef("media/clip-7", "video/mp4"),
	})
	items, ok := out["output"].([]any)
	require.True(t, ok)
	require.Len(t, items, 2)
	assert.Equal(t, map[string]any{"type": "input_text", "text": "[This tool returned a video (video/mp4, stored as " +
		"media/clip-7) that was not sent: OpenAI Responses does not accept video input.]"}, items[1])
}

// Media a tool left on disk is loaded and sent with its bytes, as a user's
// media is.
func TestResponsesToolResult_LoadsMediaFromFiles(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "snap.png")
	docPath := filepath.Join(dir, "report.pdf")
	require.NoError(t, os.WriteFile(imgPath, []byte("img"), 0o600))
	require.NoError(t, os.WriteFile(docPath, []byte("pdf"), 0o600))

	out := responsesOutputOf(t, []types.ContentPart{
		{Type: types.ContentTypeImage, Media: &types.MediaContent{FilePath: &imgPath, MIMEType: types.MIMETypeImagePNG}},
		{Type: types.ContentTypeDocument, Media: &types.MediaContent{FilePath: &docPath, MIMEType: "application/pdf"}},
	})

	items, ok := out["output"].([]any)
	require.True(t, ok)
	require.Len(t, items, 2)
	assert.Equal(t, "data:image/png;base64,aW1n", items[0].(map[string]any)["image_url"])
	assert.Equal(t, "data:application/pdf;base64,cGRm", items[1].(map[string]any)["file_data"])
}

// A document other than a PDF is described: only PDFs are sent as input_file.
func TestResponsesToolResult_NonPDFDocumentIsDescribed(t *testing.T) {
	out := responsesOutputOf(t, []types.ContentPart{types.NewDocumentPartFromData("YSxi", "text/csv")})
	items, ok := out["output"].([]any)
	require.True(t, ok)
	require.Len(t, items, 1)
	assert.Equal(t, map[string]any{"type": "input_text", "text": "[This tool returned a document (text/csv) that was " +
		"not sent: OpenAI Responses does not accept text/csv documents.]"}, items[0])
}

// Media that is described is never loaded, so a document the API does not
// take cannot fail the request by being unreadable.
func TestResponsesToolResult_DescribedMediaIsNotLoaded(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.csv")
	out := responsesOutputOf(t, []types.ContentPart{
		{Type: types.ContentTypeDocument, Media: &types.MediaContent{FilePath: &missing, MIMEType: "text/csv"}},
	})
	items, ok := out["output"].([]any)
	require.True(t, ok)
	assert.Equal(t, "[This tool returned a document (text/csv) that was not sent: OpenAI Responses does not "+
		"accept text/csv documents.]", items[0].(map[string]any)["text"])
}

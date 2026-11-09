package tools_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func strPtr(s string) *string { return &s }

// base64File returns the base64 encoding of the file at path.
func base64File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(data)
}

// TestRegistryExecute_MockToolReturnsMockParts runs mock tools with MockParts
// through the registry, the way a conversation calls them, and checks that the
// parts reach the tool result with file_path media resolved to base64. The
// registry only sees parts when the mock executor is a MultimodalExecutor.
func TestRegistryExecute_MockToolReturnsMockParts(t *testing.T) {
	imgPath := filepath.Join("testdata", "sample-chart.png")
	wantData := base64File(t, imgPath)

	for _, tc := range []struct {
		name     string
		desc     tools.ToolDescriptor
		args     string
		wantJSON string
	}{
		{
			name:     "static mock",
			desc:     tools.ToolDescriptor{Name: "chart_static", Mode: "mock", MockResult: json.RawMessage(`{"ok":true}`)},
			args:     `{}`,
			wantJSON: `{"ok":true}`,
		},
		{
			name:     "scripted mock",
			desc:     tools.ToolDescriptor{Name: "chart_scripted", Mode: "mock", MockTemplate: `{"metric":"{{.metric}}"}`},
			args:     `{"metric":"revenue"}`,
			wantJSON: `{"metric":"revenue"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desc := tc.desc
			desc.MockParts = []types.ContentPart{
				{Type: types.ContentTypeText, Text: strPtr("Chart generated")},
				{Type: types.ContentTypeImage, Media: &types.MediaContent{
					FilePath: strPtr(imgPath), MIMEType: types.MIMETypeImagePNG,
				}},
			}
			reg := tools.NewRegistry()
			require.NoError(t, reg.Register(&desc))

			res, err := reg.Execute(context.Background(), desc.Name, json.RawMessage(tc.args))
			require.NoError(t, err)
			require.Empty(t, res.Error)
			assert.JSONEq(t, tc.wantJSON, string(res.Result))
			require.Len(t, res.Parts, 2)
			assert.Equal(t, "Chart generated", *res.Parts[0].Text)
			require.NotNil(t, res.Parts[1].Media)
			require.NotNil(t, res.Parts[1].Media.Data)
			assert.Equal(t, wantData, *res.Parts[1].Media.Data)
			assert.Nil(t, res.Parts[1].Media.FilePath)

			async, err := reg.ExecuteAsync(context.Background(), desc.Name, json.RawMessage(tc.args))
			require.NoError(t, err)
			assert.Equal(t, tools.ToolStatusComplete, async.Status)
			assert.JSONEq(t, tc.wantJSON, string(async.Content))
			require.Len(t, async.Parts, 2)
			assert.Equal(t, wantData, *async.Parts[1].Media.Data)
		})
	}
}

func TestMockStaticExecutor_ExecuteMultimodal_NoParts(t *testing.T) {
	exec := tools.NewMockStaticExecutor()
	desc := &tools.ToolDescriptor{
		Name:       "simple_tool",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"message": "hello"}`),
	}

	result, parts, err := exec.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"message": "hello"}`, string(result))
	assert.Nil(t, parts, "parts should be nil when MockParts is empty")
}

func TestMockStaticExecutor_ExecuteMultimodal_TextParts(t *testing.T) {
	exec := tools.NewMockStaticExecutor()
	desc := &tools.ToolDescriptor{
		Name:       "text_tool",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"message": "Chart generated"}`),
		MockParts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: strPtr("Chart generated successfully")},
		},
	}

	result, parts, err := exec.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"message": "Chart generated"}`, string(result))
	require.Len(t, parts, 1)
	assert.Equal(t, types.ContentTypeText, parts[0].Type)
	assert.Equal(t, "Chart generated successfully", *parts[0].Text)
	assert.Nil(t, parts[0].Media)
}

func TestMockStaticExecutor_ExecuteMultimodal_FilePathResolved(t *testing.T) {
	exec := tools.NewMockStaticExecutor()
	imgPath := filepath.Join("testdata", "sample-chart.png")

	// Verify file exists
	imgData, err := os.ReadFile(imgPath)
	require.NoError(t, err, "testdata/sample-chart.png must exist")
	expectedBase64 := base64.StdEncoding.EncodeToString(imgData)

	desc := &tools.ToolDescriptor{
		Name:       "chart_tool",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"message": "Chart generated"}`),
		MockParts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: strPtr("Chart generated successfully")},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					FilePath: strPtr(imgPath),
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	result, parts, err := exec.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"message": "Chart generated"}`, string(result))
	require.Len(t, parts, 2)

	// Text part unchanged
	assert.Equal(t, types.ContentTypeText, parts[0].Type)
	assert.Equal(t, "Chart generated successfully", *parts[0].Text)

	// Image part: file_path resolved to base64, file_path cleared
	assert.Equal(t, types.ContentTypeImage, parts[1].Type)
	require.NotNil(t, parts[1].Media)
	assert.Nil(t, parts[1].Media.FilePath, "file_path should be nil after resolution")
	require.NotNil(t, parts[1].Media.Data)
	assert.Equal(t, expectedBase64, *parts[1].Media.Data)
	assert.Equal(t, types.MIMETypeImagePNG, parts[1].Media.MIMEType)
}

func TestMockStaticExecutor_ExecuteMultimodal_URLPassedThrough(t *testing.T) {
	exec := tools.NewMockStaticExecutor()
	desc := &tools.ToolDescriptor{
		Name:       "url_tool",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"message": "ok"}`),
		MockParts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      strPtr("https://example.com/chart.png"),
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	_, parts, err := exec.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "https://example.com/chart.png", *parts[0].Media.URL)
	assert.Nil(t, parts[0].Media.Data, "URL parts should not be resolved to data")
}

func TestMockStaticExecutor_ExecuteMultimodal_Base64PassedThrough(t *testing.T) {
	exec := tools.NewMockStaticExecutor()
	b64 := "aGVsbG8=" // "hello"
	desc := &tools.ToolDescriptor{
		Name:       "b64_tool",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"message": "ok"}`),
		MockParts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data:     strPtr(b64),
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	_, parts, err := exec.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, b64, *parts[0].Media.Data, "base64 data should pass through unchanged")
}

func TestMockStaticExecutor_ExecuteMultimodal_MissingFile(t *testing.T) {
	exec := tools.NewMockStaticExecutor()
	desc := &tools.ToolDescriptor{
		Name:       "missing_file_tool",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"message": "ok"}`),
		MockParts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					FilePath: strPtr("/nonexistent/path/image.png"),
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	_, _, err := exec.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve mock_parts")
}

func TestMockStaticExecutor_ExecuteMultimodal_ExecuteError(t *testing.T) {
	exec := tools.NewMockStaticExecutor()
	desc := &tools.ToolDescriptor{
		Name: "bad_tool",
		Mode: "live", // Invalid mode for mock executor
		MockParts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: strPtr("test")},
		},
	}

	result, parts, err := exec.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{}`))
	require.ErrorIs(t, err, tools.ErrMockExecutorOnly)
	assert.Nil(t, result)
	assert.Nil(t, parts, "a failed call must not return the mock parts")
}

func TestResolveMockParts_EmptySlice(t *testing.T) {
	parts, err := tools.ResolveMockParts(nil)
	require.NoError(t, err)
	assert.Nil(t, parts)
}

func TestResolveMockParts_MixedParts(t *testing.T) {
	imgPath := filepath.Join("testdata", "sample-chart.png")
	imgData, err := os.ReadFile(imgPath)
	require.NoError(t, err)
	expectedBase64 := base64.StdEncoding.EncodeToString(imgData)

	parts := []types.ContentPart{
		{Type: types.ContentTypeText, Text: strPtr("description")},
		{
			Type: types.ContentTypeImage,
			Media: &types.MediaContent{
				FilePath: strPtr(imgPath),
				MIMEType: types.MIMETypeImagePNG,
			},
		},
		{
			Type: types.ContentTypeImage,
			Media: &types.MediaContent{
				URL:      strPtr("https://example.com/img.png"),
				MIMEType: types.MIMETypeImagePNG,
			},
		},
	}

	resolved, err := tools.ResolveMockParts(parts)
	require.NoError(t, err)
	require.Len(t, resolved, 3)

	// Text part unchanged
	assert.Equal(t, "description", *resolved[0].Text)

	// File path resolved
	require.NotNil(t, resolved[1].Media.Data)
	assert.Equal(t, expectedBase64, *resolved[1].Media.Data)
	assert.Nil(t, resolved[1].Media.FilePath)

	// URL unchanged
	assert.Equal(t, "https://example.com/img.png", *resolved[2].Media.URL)
	assert.Nil(t, resolved[2].Media.Data)
}

func TestRegistry_Execute_WithMockParts(t *testing.T) {
	registry := tools.NewRegistry()
	imgPath := filepath.Join("testdata", "sample-chart.png")

	descriptor := &tools.ToolDescriptor{
		Name:         "generate_chart",
		Description:  "Generates a chart image",
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"message": {"type": "string"}}}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{"message": "Chart generated"}`),
		MockParts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: strPtr("Chart generated successfully")},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					FilePath: strPtr(imgPath),
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	err := registry.Register(descriptor)
	require.NoError(t, err)

	result, err := registry.Execute(context.Background(), "generate_chart", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Empty(t, result.Error)
	assert.JSONEq(t, `{"message": "Chart generated"}`, string(result.Result))
	require.Len(t, result.Parts, 2)
	assert.Equal(t, types.ContentTypeText, result.Parts[0].Type)
	assert.Equal(t, types.ContentTypeImage, result.Parts[1].Type)
	assert.NotNil(t, result.Parts[1].Media.Data, "file_path should be resolved to base64 data")
}

func TestRegistry_Execute_WithoutMockParts(t *testing.T) {
	registry := tools.NewRegistry()

	descriptor := &tools.ToolDescriptor{
		Name:         "simple_tool",
		Description:  "A simple tool",
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"message": {"type": "string"}}}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{"message": "hello"}`),
	}

	err := registry.Register(descriptor)
	require.NoError(t, err)

	result, err := registry.Execute(context.Background(), "simple_tool", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Empty(t, result.Error)
	assert.Nil(t, result.Parts, "parts should be nil when no MockParts configured")
}

func TestRegistry_ExecuteAsync_WithMockParts(t *testing.T) {
	registry := tools.NewRegistry()
	imgPath := filepath.Join("testdata", "sample-chart.png")

	descriptor := &tools.ToolDescriptor{
		Name:         "async_chart",
		Description:  "Generates a chart image",
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"message": {"type": "string"}}}`),
		Mode:         "mock",
		MockResult:   json.RawMessage(`{"message": "Chart generated"}`),
		MockParts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					FilePath: strPtr(imgPath),
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	err := registry.Register(descriptor)
	require.NoError(t, err)

	result, err := registry.ExecuteAsync(context.Background(), "async_chart", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, result.Status)
	assert.Empty(t, result.Error)
	require.Len(t, result.Parts, 1)
	assert.Equal(t, types.ContentTypeImage, result.Parts[0].Type)
	assert.NotNil(t, result.Parts[0].Media.Data)
}

func TestToolDescriptor_MockParts_JSONRoundTrip(t *testing.T) {
	desc := tools.ToolDescriptor{
		Name:        "chart_tool",
		Description: "Generates a chart",
		Mode:        "mock",
		MockResult:  json.RawMessage(`{"message": "Chart generated"}`),
		MockParts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: strPtr("Chart generated successfully")},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					FilePath: strPtr("testdata/sample-chart.png"),
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object"}`),
	}

	data, err := json.Marshal(desc)
	require.NoError(t, err)

	var decoded tools.ToolDescriptor
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.Len(t, decoded.MockParts, 2)
	assert.Equal(t, types.ContentTypeText, decoded.MockParts[0].Type)
	assert.Equal(t, "Chart generated successfully", *decoded.MockParts[0].Text)
	assert.Equal(t, types.ContentTypeImage, decoded.MockParts[1].Type)
	assert.Equal(t, "testdata/sample-chart.png", *decoded.MockParts[1].Media.FilePath)
}

func TestMockScriptedExecutor_ExecuteMultimodal_NoParts(t *testing.T) {
	exec := tools.NewMockScriptedExecutor()
	desc := &tools.ToolDescriptor{
		Name:         "templated_tool",
		Mode:         "mock",
		MockTemplate: `{"greeting": "hello {{.name}}"}`,
	}

	result, parts, err := exec.ExecuteMultimodal(
		context.Background(), desc, json.RawMessage(`{"name":"world"}`),
	)
	require.NoError(t, err)
	assert.JSONEq(t, `{"greeting": "hello world"}`, string(result))
	assert.Nil(t, parts, "parts should be nil when MockParts is empty")
}

func TestMockScriptedExecutor_ExecuteMultimodal_WithParts(t *testing.T) {
	exec := tools.NewMockScriptedExecutor()
	imgPath := filepath.Join("testdata", "sample-chart.png")
	desc := &tools.ToolDescriptor{
		Name:         "chart_tool",
		Mode:         "mock",
		MockTemplate: `{"metric": "{{.metric}}"}`,
		MockParts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: strPtr("Chart generated")},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					FilePath: strPtr(imgPath),
					MIMEType: types.MIMETypeImagePNG,
				},
			},
		},
	}

	result, parts, err := exec.ExecuteMultimodal(
		context.Background(), desc, json.RawMessage(`{"metric":"revenue"}`),
	)
	require.NoError(t, err)
	assert.JSONEq(t, `{"metric": "revenue"}`, string(result))
	require.Len(t, parts, 2)
	assert.Equal(t, types.ContentTypeText, parts[0].Type)
	assert.Equal(t, types.ContentTypeImage, parts[1].Type)
	imgData, err := os.ReadFile(imgPath)
	require.NoError(t, err)
	require.NotNil(t, parts[1].Media.Data, "file_path should be resolved to base64 data")
	assert.Equal(t, base64.StdEncoding.EncodeToString(imgData), *parts[1].Media.Data)
	assert.Nil(t, parts[1].Media.FilePath, "file_path should be cleared after resolution")
}

func TestMockScriptedExecutor_ExecuteMultimodal_NonMockMode(t *testing.T) {
	exec := tools.NewMockScriptedExecutor()
	desc := &tools.ToolDescriptor{
		Name:         "live_tool",
		Mode:         "live",
		MockTemplate: `{"result": "nope"}`,
	}

	_, _, err := exec.ExecuteMultimodal(
		context.Background(), desc, json.RawMessage(`{}`),
	)
	assert.ErrorIs(t, err, tools.ErrMockExecutorOnly)
}

func TestToolDescriptor_MockParts_Empty(t *testing.T) {
	desc := tools.ToolDescriptor{
		Name:         "no_parts",
		Description:  "No parts tool",
		Mode:         "mock",
		InputSchema:  json.RawMessage(`{"type": "object"}`),
		OutputSchema: json.RawMessage(`{"type": "object"}`),
	}

	data, err := json.Marshal(desc)
	require.NoError(t, err)

	// MockParts should be omitted from JSON when empty
	assert.NotContains(t, string(data), "mock_parts")

	var decoded tools.ToolDescriptor
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Nil(t, decoded.MockParts)
}

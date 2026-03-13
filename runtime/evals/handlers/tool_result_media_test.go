package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- ToolResultHasMedia ---

func TestToolResultHasMediaHandler_Type(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	if h.Type() != "tool_result_has_media" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolResultHasMediaHandler_Pass(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "generate_chart",
				Result: []types.ContentPart{
					types.NewImagePartFromData("abc123", "image/png", nil),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":       "generate_chart",
		"media_type": "image",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolResultHasMediaHandler_PassAudio(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "synthesize_speech",
				Result: []types.ContentPart{
					types.NewAudioPartFromData("audiodata", "audio/mp3"),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":       "synthesize_speech",
		"media_type": "audio",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolResultHasMediaHandler_FailWrongType(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "generate_chart",
				Result: []types.ContentPart{
					types.NewImagePartFromData("abc123", "image/png", nil),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":       "generate_chart",
		"media_type": "audio",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail for wrong media type")
	}
}

func TestToolResultHasMediaHandler_FailWrongTool(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "other_tool",
				Result: []types.ContentPart{
					types.NewImagePartFromData("abc123", "image/png", nil),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":       "generate_chart",
		"media_type": "image",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail for wrong tool name")
	}
}

func TestToolResultHasMediaHandler_NoMediaType(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail with no media_type")
	}
}

func TestToolResultHasMediaHandler_NilResult(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "generate_chart", Result: nil},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":       "generate_chart",
		"media_type": "image",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail for nil result")
	}
}

func TestToolResultHasMediaHandler_StringResult(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "generate_chart", Result: "plain string result"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":       "generate_chart",
		"media_type": "image",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail for string result")
	}
}

func TestToolResultHasMediaHandler_NoToolFilter(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "any_tool",
				Result: []types.ContentPart{
					types.NewVideoPartFromData("videodata", "video/mp4"),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"media_type": "video",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatalf("expected pass without tool filter: %s", result.Explanation)
	}
}

func TestToolResultHasMediaHandler_CaseInsensitive(t *testing.T) {
	h := &ToolResultHasMediaHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "gen",
				Result: []types.ContentPart{
					types.NewImagePartFromData("data", "image/png", nil),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":       "gen",
		"media_type": "Image",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatalf("expected pass with case-insensitive media_type: %s", result.Explanation)
	}
}

// --- ToolResultMediaType ---

func TestToolResultMediaTypeHandler_Type(t *testing.T) {
	h := &ToolResultMediaTypeHandler{}
	if h.Type() != "tool_result_media_type" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolResultMediaTypeHandler_Pass(t *testing.T) {
	h := &ToolResultMediaTypeHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "generate_chart",
				Result: []types.ContentPart{
					types.NewImagePartFromData("abc123", "image/png", nil),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":      "generate_chart",
		"mime_type": "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolResultMediaTypeHandler_FailWrongMIME(t *testing.T) {
	h := &ToolResultMediaTypeHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "generate_chart",
				Result: []types.ContentPart{
					types.NewImagePartFromData("abc123", "image/png", nil),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":      "generate_chart",
		"mime_type": "image/jpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail for wrong MIME type")
	}
}

func TestToolResultMediaTypeHandler_NoMIMEType(t *testing.T) {
	h := &ToolResultMediaTypeHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail with no mime_type")
	}
}

func TestToolResultMediaTypeHandler_SkipsTextParts(t *testing.T) {
	h := &ToolResultMediaTypeHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "generate_chart",
				Result: []types.ContentPart{
					types.NewTextPart("chart description"),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":      "generate_chart",
		"mime_type": "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail when only text parts exist")
	}
}

func TestToolResultMediaTypeHandler_NilResult(t *testing.T) {
	h := &ToolResultMediaTypeHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "generate_chart", Result: nil},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":      "generate_chart",
		"mime_type": "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail for nil result")
	}
}

func TestToolResultMediaTypeHandler_NoToolFilter(t *testing.T) {
	h := &ToolResultMediaTypeHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "any_tool",
				Result: []types.ContentPart{
					types.NewAudioPartFromData("audio", "audio/mpeg"),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"mime_type": "audio/mpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatalf("expected pass without tool filter: %s", result.Explanation)
	}
}

func TestToolResultMediaTypeHandler_CaseInsensitive(t *testing.T) {
	h := &ToolResultMediaTypeHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "gen",
				Result: []types.ContentPart{
					types.NewImagePartFromData("data", "image/png", nil),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":      "gen",
		"mime_type": "Image/PNG",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatalf("expected pass with case-insensitive mime_type: %s", result.Explanation)
	}
}

func TestToolResultMediaTypeHandler_WrongTool(t *testing.T) {
	h := &ToolResultMediaTypeHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "other_tool",
				Result: []types.ContentPart{
					types.NewImagePartFromData("data", "image/png", nil),
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":      "generate_chart",
		"mime_type": "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail for wrong tool name")
	}
}

// --- extractContentParts ---

func TestExtractContentParts_MapBased(t *testing.T) {
	// Simulate JSON-deserialized result ([]any with map[string]any)
	result := []any{
		map[string]any{
			"type": "image",
			"media": map[string]any{
				"mime_type": "image/png",
				"data":      "base64data",
			},
		},
		map[string]any{
			"type": "text",
			"text": "a caption",
		},
	}

	parts := extractContentParts(result)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}

	if parts[0].Type != "image" {
		t.Fatalf("expected image type, got %s", parts[0].Type)
	}
	if parts[0].Media == nil || parts[0].Media.MIMEType != "image/png" {
		t.Fatal("expected media with MIME type image/png")
	}

	if parts[1].Type != "text" || parts[1].Text == nil || *parts[1].Text != "a caption" {
		t.Fatal("expected text part with 'a caption'")
	}
}

func TestExtractContentParts_MapWithURLAndFilePath(t *testing.T) {
	result := []any{
		map[string]any{
			"type": "document",
			"media": map[string]any{
				"mime_type": "application/pdf",
				"file_path": "/tmp/doc.pdf",
			},
		},
		map[string]any{
			"type": "image",
			"media": map[string]any{
				"mime_type": "image/jpeg",
				"url":       "https://example.com/img.jpg",
			},
		},
	}

	parts := extractContentParts(result)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Media == nil || parts[0].Media.FilePath == nil || *parts[0].Media.FilePath != "/tmp/doc.pdf" {
		t.Fatal("expected file_path on first part")
	}
	if parts[1].Media == nil || parts[1].Media.URL == nil || *parts[1].Media.URL != "https://example.com/img.jpg" {
		t.Fatal("expected url on second part")
	}
}

func TestExtractContentParts_InvalidSliceItems(t *testing.T) {
	// Non-map items and maps without type should be skipped
	result := []any{
		"not a map",
		42,
		map[string]any{"no_type": "value"},
		map[string]any{"type": "image", "media": map[string]any{"mime_type": "image/png"}},
	}

	parts := extractContentParts(result)
	if len(parts) != 1 {
		t.Fatalf("expected 1 valid part, got %d", len(parts))
	}
}

func TestExtractContentParts_NonSlice(t *testing.T) {
	parts := extractContentParts("just a string")
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts for string input, got %d", len(parts))
	}
}

// --- Registration ---

func TestToolResultMediaHandlersRegistered(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&ToolResultHasMediaHandler{})
	registry.Register(&ToolResultMediaTypeHandler{})

	if _, err := registry.Get("tool_result_has_media"); err != nil {
		t.Fatalf("tool_result_has_media not registered: %v", err)
	}
	if _, err := registry.Get("tool_result_media_type"); err != nil {
		t.Fatalf("tool_result_media_type not registered: %v", err)
	}
}

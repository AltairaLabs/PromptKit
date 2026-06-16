package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestImageFormat_ToolResultPart guards that media produced by a tool (e.g.
// image__generate), which lands in a tool-role message's ToolResult.Parts
// rather than an assistant message's inline Parts, is visible to the media
// format evals. Before the extractMediaParts fix these were silently ignored.
func TestImageFormat_ToolResultPart(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Parts: []types.ContentPart{types.NewTextPart("make an image")}},
		{Role: "assistant"},
		{Role: "tool", ToolResult: &types.MessageToolResult{
			Name:  "image__generate",
			Parts: []types.ContentPart{types.NewImagePartFromData("iVBORw0KGgo=", "image/png", nil)},
		}},
	}

	h := &ImageFormatHandler{}
	res, err := h.Eval(context.Background(), &evals.EvalContext{Messages: msgs},
		map[string]any{"formats": []any{"png"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Score == nil || *res.Score != 1 {
		t.Fatalf("expected pass (score 1) for tool-result image, got %v: %s", res.Score, res.Explanation)
	}
}

// TestImageFormat_NoImagesAnywhere keeps the negative path honest: no image in
// assistant or tool messages still fails.
func TestImageFormat_NoImagesAnywhere(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Parts: []types.ContentPart{types.NewTextPart("hi")}},
		{Role: "assistant", Parts: []types.ContentPart{types.NewTextPart("hello")}},
	}
	h := &ImageFormatHandler{}
	res, err := h.Eval(context.Background(), &evals.EvalContext{Messages: msgs},
		map[string]any{"formats": []any{"png"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Score == nil || *res.Score != 0 {
		t.Fatalf("expected fail (score 0) with no images, got %v", res.Score)
	}
}

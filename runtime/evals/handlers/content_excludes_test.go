package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestContentExcludesHandler_MatchModeSubstring(t *testing.T) {
	h := &ContentExcludesHandler{}
	ctx := context.Background()

	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "this is unforbidden content"},
		},
	}

	// Substring mode (default): "forbidden" matches inside "unforbidden"
	result, err := h.Eval(ctx, evalCtx, map[string]any{
		"patterns": []string{"forbidden"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected substring match to find 'forbidden' in 'unforbidden'")
	}
}

func TestContentExcludesHandler_MatchModeWordBoundary(t *testing.T) {
	h := &ContentExcludesHandler{}
	ctx := context.Background()

	// "unforbidden" should NOT match "forbidden" with word_boundary mode
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "this is unforbidden content"},
		},
	}

	result, err := h.Eval(ctx, evalCtx, map[string]any{
		"patterns":   []string{"forbidden"},
		"match_mode": "word_boundary",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("word_boundary should NOT match 'forbidden' inside 'unforbidden'")
	}

	// But exact word "forbidden" should match
	evalCtx2 := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "this is forbidden content"},
		},
	}

	result2, err := h.Eval(ctx, evalCtx2, map[string]any{
		"patterns":   []string{"forbidden"},
		"match_mode": "word_boundary",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Passed {
		t.Error("word_boundary should match exact word 'forbidden'")
	}
}

func TestContentExcludesHandler_MatchModeWordBoundaryCaseInsensitive(t *testing.T) {
	h := &ContentExcludesHandler{}
	ctx := context.Background()

	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "this is FORBIDDEN content"},
		},
	}

	result, err := h.Eval(ctx, evalCtx, map[string]any{
		"patterns":   []string{"forbidden"},
		"match_mode": "word_boundary",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("word_boundary should be case-insensitive")
	}
}

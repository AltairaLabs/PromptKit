package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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
	if result.Score != nil && *result.Score >= 1.0 {
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
	if !(result.Score != nil && *result.Score >= 1.0) {
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
	if result2.Score != nil && *result2.Score >= 1.0 {
		t.Error("word_boundary should match exact word 'forbidden'")
	}
}

func TestContentExcludesHandler_EvalPartial_Pass(t *testing.T) {
	h := &ContentExcludesHandler{}
	result, err := h.EvalPartial(context.Background(), "this is safe content", map[string]any{
		"patterns": []string{"forbidden", "banned"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Error("expected pass: no forbidden patterns in content")
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Errorf("expected score 1.0, got %v", result.Score)
	}
}

func TestContentExcludesHandler_EvalPartial_Fail(t *testing.T) {
	h := &ContentExcludesHandler{}
	result, err := h.EvalPartial(context.Background(), "this has forbidden words", map[string]any{
		"patterns": []string{"forbidden", "banned"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("expected fail: 'forbidden' found in content")
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Errorf("expected score 0.0, got %v", result.Score)
	}
}

func TestContentExcludesHandler_EvalPartial_NoPatterns(t *testing.T) {
	h := &ContentExcludesHandler{}
	result, err := h.EvalPartial(context.Background(), "anything", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Error("expected pass: no patterns to check")
	}
}

func TestContentExcludesHandler_EvalPartial_UsesSubstringMode(t *testing.T) {
	h := &ContentExcludesHandler{}
	// EvalPartial always uses substring mode, so "forbidden" matches inside "unforbidden"
	result, err := h.EvalPartial(context.Background(), "this is unforbidden content", map[string]any{
		"patterns":   []string{"forbidden"},
		"match_mode": "word_boundary", // ignored by EvalPartial
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("EvalPartial should use substring mode, matching 'forbidden' inside 'unforbidden'")
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
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("word_boundary should be case-insensitive")
	}
}

// case_sensitive: true matches a pattern only in its own case, in every match
// mode and while streaming. Unset or false keeps case-insensitive matching.
func TestContentExcludesHandler_CaseSensitive(t *testing.T) {
	h := &ContentExcludesHandler{}
	ctx := context.Background()
	tests := []struct {
		name      string
		content   string
		mode      string
		sensitive any
		wantClean bool
	}{
		{"substring other case", "this is FORBIDDEN content", "", true, true},
		{"substring same case", "this is forbidden content", "", true, false},
		{"word boundary other case", "this is FORBIDDEN content", matchModeWordBoundary, true, true},
		{"word boundary same case", "this is forbidden content", matchModeWordBoundary, true, false},
		{"explicit false stays insensitive", "this is FORBIDDEN content", "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]any{"patterns": []string{"forbidden"}, "case_sensitive": tt.sensitive}
			if tt.mode != "" {
				params["match_mode"] = tt.mode
			}
			evalCtx := &evals.EvalContext{Messages: []types.Message{{Role: "assistant", Content: tt.content}}}
			result, err := h.Eval(ctx, evalCtx, params)
			if err != nil {
				t.Fatal(err)
			}
			if clean := *result.Score == 1.0; clean != tt.wantClean {
				t.Errorf("Eval score %v, want clean=%v", *result.Score, tt.wantClean)
			}
			partial, err := h.EvalPartial(ctx, tt.content, params)
			if err != nil {
				t.Fatal(err)
			}
			if clean := *partial.Score == 1.0; clean != tt.wantClean {
				t.Errorf("EvalPartial score %v, want clean=%v", *partial.Score, tt.wantClean)
			}
		})
	}
}

// banned_words is an alias of content_excludes and honors case_sensitive too.
func TestBannedWords_CaseSensitive(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	h, err := reg.Get("banned_words")
	if err != nil {
		t.Fatal(err)
	}
	params := evals.NormalizeParams("banned_words",
		evals.ApplyDefaults("banned_words", map[string]any{"words": []string{"Acme"}, "case_sensitive": true}))
	evalCtx := &evals.EvalContext{Messages: []types.Message{{Role: "assistant", Content: "we love acme here"}}}
	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if *result.Score != 1.0 {
		t.Errorf("case_sensitive banned word matched another case: score %v", *result.Score)
	}
}

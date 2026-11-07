package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

func TestSentenceCountHandler_Type(t *testing.T) {
	h := &SentenceCountHandler{}
	if h.Type() != "sentence_count" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestSentenceCountHandler_EmptyString(t *testing.T) {
	h := &SentenceCountHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{CurrentOutput: ""}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatal("expected pass for measurement handler")
	}
	count, ok := result.Details["count"].(int)
	if !ok {
		t.Fatal("expected count in details")
	}
	if count != 0 {
		t.Fatalf("expected 0 sentences, got %d", count)
	}
}

func TestSentenceCountHandler_SingleSentence(t *testing.T) {
	h := &SentenceCountHandler{}
	result, err := h.Eval(
		context.Background(),
		&evals.EvalContext{CurrentOutput: "Hello world."},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	count := result.Details["count"].(int)
	if count != 1 {
		t.Fatalf("expected 1 sentence, got %d", count)
	}
}

func TestSentenceCountHandler_MultipleSentences(t *testing.T) {
	h := &SentenceCountHandler{}
	result, err := h.Eval(
		context.Background(),
		&evals.EvalContext{CurrentOutput: "First sentence. Second sentence! Third?"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	count := result.Details["count"].(int)
	if count != 3 {
		t.Fatalf("expected 3 sentences, got %d", count)
	}
}

func TestSentenceCountHandler_NoTrailingPunctuation(t *testing.T) {
	h := &SentenceCountHandler{}
	result, err := h.Eval(
		context.Background(),
		&evals.EvalContext{CurrentOutput: "Hello world"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	count := result.Details["count"].(int)
	if count != 1 {
		t.Fatalf("expected 1 sentence for text without punctuation, got %d", count)
	}
}

func TestSentenceCountHandler_ScoreAlwaysOne(t *testing.T) {
	h := &SentenceCountHandler{}
	result, err := h.Eval(
		context.Background(),
		&evals.EvalContext{CurrentOutput: "A. B. C."},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected score 1.0 for measurement, got %v", result.Score)
	}
}

// max shapes the score the way tool_efficiency's max_calls does: the remaining
// headroom, (max + 1 - count) / (max + 1), within the cap and 0 above it.
func TestSentenceCountHandler_MaxShapesScore(t *testing.T) {
	h := &SentenceCountHandler{}
	tests := []struct {
		name   string
		output string
		params map[string]any
		want   float64
	}{
		{"no max scores 1", "A. B. C.", nil, 1.0},
		{"under max", "A.", map[string]any{"max": 3}, 0.75},
		{"at max", "A. B. C.", map[string]any{"max": 3}, 0.25},
		{"over max", "A. B. C. D.", map[string]any{"max": 3}, 0},
		{"empty output", "", map[string]any{"max": 3}, 1.0},
		{"float max from YAML/JSON", "A. B.", map[string]any{"max": 3.0}, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := h.Eval(context.Background(), &evals.EvalContext{CurrentOutput: tt.output}, tt.params)
			if err != nil {
				t.Fatal(err)
			}
			if result.Score == nil || *result.Score != tt.want {
				t.Fatalf("score = %v, want %v", result.Score, tt.want)
			}
		})
	}
}

// max_sentences, the alias's legacy param name, reaches the handler as max.
func TestMaxSentences_AliasEnforcesMax(t *testing.T) {
	h, err := evals.NewEvalTypeRegistry().Get("max_sentences")
	if err != nil {
		t.Fatal(err)
	}
	params := evals.NormalizeParams("max_sentences", map[string]any{"max_sentences": 1})
	result, err := h.Eval(context.Background(), &evals.EvalContext{CurrentOutput: "One. Two."}, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 0 {
		t.Fatalf("score = %v, want 0 for 2 sentences over max_sentences 1", result.Score)
	}
}

func TestSentenceCountHandler_ValidateParams(t *testing.T) {
	h := &SentenceCountHandler{}
	if err := h.ValidateParams(map[string]any{}); err != nil {
		t.Errorf("no max: %v", err)
	}
	if err := h.ValidateParams(map[string]any{"max": 2}); err != nil {
		t.Errorf("max 2: %v", err)
	}
	for _, bad := range []any{0, -1, "three"} {
		if err := h.ValidateParams(map[string]any{"max": bad}); err == nil {
			t.Errorf("max %v: expected an error", bad)
		}
	}
}

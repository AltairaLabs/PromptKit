package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
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
	if !result.Passed {
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

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// hfTextTestServer mints an httptest.Server that returns the supplied
// labels in the nested HF text-classification shape — `[[{label, score},
// ...]]`. That matches what HF emits when `return_all_scores: true` is
// set, which is the mode the handler always requests.
func hfTextTestServer(t *testing.T, scores []classify.LabelScore) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		nested := [][]classify.LabelScore{scores}
		_ = json.NewEncoder(w).Encode(nested)
	}))
}

// ctxWithTextRegistry builds a context carrying a registry that routes
// text classification to a single HF client pointed at srvURL.
func ctxWithTextRegistry(t *testing.T, srvURL string) context.Context {
	t.Helper()
	client, err := classifyhf.NewClient(classifyhf.Config{APIKey: "test-token", BaseURL: srvURL})
	if err != nil {
		t.Fatalf("hf client: %v", err)
	}
	reg := classify.NewRegistry()
	reg.RegisterText("hf", client)
	if err := reg.SetDefaultText("hf"); err != nil {
		t.Fatalf("SetDefaultText: %v", err)
	}
	return classify.WithRegistry(context.Background(), reg)
}

// textMessage returns a single message carrying inline text content.
// Uses the Content field (the common shape produced by chat providers)
// rather than a Parts entry so the test exercises the merged extraction
// path.
func textMessage(role, body string) types.Message {
	return types.Message{Role: role, Content: body}
}

func TestTextSentiment_PassesWhenLabelMeetsThreshold(t *testing.T) {
	srv := hfTextTestServer(t, []classify.LabelScore{
		{Label: "POSITIVE", Score: 0.91},
		{Label: "NEGATIVE", Score: 0.09},
	})
	defer srv.Close()

	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextSentimentHandler{}
	res, err := h.Eval(ctx, &evals.EvalContext{
		Messages: []types.Message{textMessage("assistant", "Glad to help! Have a wonderful day.")},
	}, map[string]any{
		"model":          "distilbert-base-uncased-finetuned-sst-2-english",
		"expected_label": "POSITIVE",
		"min_score":      0.7,
	})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected pass, got skipped: %s", res.SkipReason)
	}
	if res.Error != "" {
		t.Fatalf("expected pass, got error: %s", res.Error)
	}
	if res.Score == nil || *res.Score < 0.7 {
		t.Errorf("score = %v, want >= 0.7", res.Score)
	}
}

func TestTextSentiment_FailsBelowThreshold(t *testing.T) {
	srv := hfTextTestServer(t, []classify.LabelScore{
		{Label: "POSITIVE", Score: 0.40},
		{Label: "NEGATIVE", Score: 0.60},
	})
	defer srv.Close()

	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextSentimentHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{
		Messages: []types.Message{textMessage("assistant", "Whatever, I tried.")},
	}, map[string]any{
		"model":          "x/y",
		"expected_label": "POSITIVE",
		"min_score":      0.7,
	})
	val, _ := res.Value.(map[string]any)
	if val["actual_score"].(float64) != 0.40 {
		t.Errorf("actual_score = %v, want 0.40", val["actual_score"])
	}
	if passed, _ := val["passed"].(bool); passed {
		t.Errorf("passed=true when score 0.40 < threshold 0.70")
	}
}

func TestTextSentiment_RejectsMaxScore(t *testing.T) {
	ctx := classify.WithRegistry(context.Background(), classify.NewRegistry())
	h := &TextSentimentHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{}, map[string]any{
		"model":          "x/y",
		"expected_label": "POSITIVE",
		"max_score":      0.5,
	})
	if !strings.Contains(res.Error, "max_score is not supported") {
		t.Errorf("error %q should reject max_score for sentiment", res.Error)
	}
}

func TestTextToxicity_PassesWhenBelowMaxScore(t *testing.T) {
	srv := hfTextTestServer(t, []classify.LabelScore{
		{Label: "toxic", Score: 0.05},
		{Label: "severe_toxic", Score: 0.01},
	})
	defer srv.Close()

	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextToxicityHandler{}
	res, err := h.Eval(ctx, &evals.EvalContext{
		Messages: []types.Message{textMessage("assistant", "Here is the information you requested.")},
	}, map[string]any{
		"model":          "unitary/toxic-bert",
		"expected_label": "toxic",
		"max_score":      0.3,
	})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected pass, got skipped: %s", res.SkipReason)
	}
	val, _ := res.Value.(map[string]any)
	if passed, _ := val["passed"].(bool); !passed {
		t.Errorf("expected passed=true (toxic 0.05 < max 0.30); got %+v", val)
	}
	if !strings.Contains(res.Explanation, "<") {
		t.Errorf("explanation should use < comparator for max_score mode: %q", res.Explanation)
	}
}

func TestTextToxicity_FailsWhenAboveMaxScore(t *testing.T) {
	srv := hfTextTestServer(t, []classify.LabelScore{
		{Label: "toxic", Score: 0.84},
	})
	defer srv.Close()

	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextToxicityHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{
		Messages: []types.Message{textMessage("assistant", "You are awful.")},
	}, map[string]any{
		"model":          "unitary/toxic-bert",
		"expected_label": "toxic",
		"max_score":      0.3,
	})
	val, _ := res.Value.(map[string]any)
	if passed, _ := val["passed"].(bool); passed {
		t.Errorf("expected passed=false (toxic 0.84 > max 0.30); got %+v", val)
	}
	if res.Score == nil || *res.Score != 0.84 {
		t.Errorf("score = %v, want 0.84 (the actual model score)", res.Score)
	}
}

func TestTextToxicity_MinScoreModeAlsoWorks(t *testing.T) {
	// s-nlp/roberta_toxicity_classifier emits "neutral"/"toxic" — the
	// natural framing is "neutral score must be high", which uses the
	// inherited min_score path.
	srv := hfTextTestServer(t, []classify.LabelScore{
		{Label: "neutral", Score: 0.88},
		{Label: "toxic", Score: 0.12},
	})
	defer srv.Close()

	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextToxicityHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{
		Messages: []types.Message{textMessage("assistant", "Here is your answer.")},
	}, map[string]any{
		"model":          "s-nlp/roberta_toxicity_classifier",
		"expected_label": "neutral",
		"min_score":      0.7,
	})
	val, _ := res.Value.(map[string]any)
	if passed, _ := val["passed"].(bool); !passed {
		t.Errorf("expected passed=true (neutral 0.88 >= 0.70); got %+v", val)
	}
}

func TestTextToxicity_RejectsBothMinAndMaxScore(t *testing.T) {
	ctx := classify.WithRegistry(context.Background(), classify.NewRegistry())
	h := &TextToxicityHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{}, map[string]any{
		"model":          "x/y",
		"expected_label": "toxic",
		"min_score":      0.5,
		"max_score":      0.5,
	})
	if !strings.Contains(res.Error, "mutually exclusive") {
		t.Errorf("error %q should reject combining min_score and max_score", res.Error)
	}
}

func TestTextClassify_LabelMissingFromModelOutput(t *testing.T) {
	srv := hfTextTestServer(t, []classify.LabelScore{
		{Label: "neutral", Score: 0.95},
	})
	defer srv.Close()

	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextSentimentHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{
		Messages: []types.Message{textMessage("assistant", "ok")},
	}, map[string]any{
		"model":          "x/y",
		"expected_label": "POSITIVE",
	})
	if !strings.Contains(res.Explanation, "not returned by model") {
		t.Errorf("explanation %q should call out missing label", res.Explanation)
	}
	if !strings.Contains(res.Explanation, "neutral") {
		t.Errorf("explanation %q should list returned labels", res.Explanation)
	}
}

func TestTextClassify_NoRegistryInContextSkips(t *testing.T) {
	// Keyless-CI path: no inference provider declared, registry is nil,
	// assertion skips cleanly with a pointer at the missing wiring.
	h := &TextSentimentHandler{}
	res, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"model":          "x/y",
		"expected_label": "POSITIVE",
	})
	if err != nil {
		t.Fatalf("Eval returned Go error: %v", err)
	}
	if !res.Skipped {
		t.Fatalf("expected Skipped; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "no classify registry configured") {
		t.Errorf("SkipReason %q should point at the missing wiring", res.SkipReason)
	}
}

func TestTextClassify_NoTextInMessagesSkips(t *testing.T) {
	srv := hfTextTestServer(t, []classify.LabelScore{{Label: "POSITIVE", Score: 0.9}})
	defer srv.Close()

	ctx := ctxWithTextRegistry(t, srv.URL)
	// Audio-only message — no extractable text. Don't fail the assertion;
	// surface as Skipped so audio-only scenarios stay clean.
	audioOnly := types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{{
			Type: types.ContentTypeAudio,
			Media: &types.MediaContent{
				MIMEType: "audio/wav",
			},
		}},
	}
	h := &TextSentimentHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{audioOnly}}, map[string]any{
		"model":          "x/y",
		"expected_label": "POSITIVE",
	})
	if !res.Skipped {
		t.Fatalf("expected Skipped when role has no text; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "no text message") {
		t.Errorf("SkipReason %q should explain why no text was scored", res.SkipReason)
	}
}

func TestTextClassify_MissingParamsErrors(t *testing.T) {
	ctx := classify.WithRegistry(context.Background(), classify.NewRegistry())
	h := &TextSentimentHandler{}

	// Missing model
	res, _ := h.Eval(ctx, &evals.EvalContext{}, map[string]any{"expected_label": "POSITIVE"})
	if !strings.Contains(res.Error, "model is required") {
		t.Errorf("error %q should mention model required", res.Error)
	}

	// Missing expected_label
	res, _ = h.Eval(ctx, &evals.EvalContext{}, map[string]any{"model": "x/y"})
	if !strings.Contains(res.Error, "expected_label is required") {
		t.Errorf("error %q should mention expected_label required", res.Error)
	}
}

func TestTextClassify_MessageIndexPicksSpecificMessage(t *testing.T) {
	srv := hfTextTestServer(t, []classify.LabelScore{{Label: "POSITIVE", Score: 0.9}})
	defer srv.Close()
	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextSentimentHandler{}
	msgs := []types.Message{
		textMessage("assistant", "first reply"),
		textMessage("assistant", "second reply"),
	}
	for _, idx := range []int{0, 1, -1} {
		res, _ := h.Eval(ctx, &evals.EvalContext{Messages: msgs}, map[string]any{
			"model":          "x/y",
			"expected_label": "POSITIVE",
			"message_index":  idx,
			"min_score":      0.5,
		})
		if res.Error != "" {
			t.Errorf("idx=%d unexpected error: %s", idx, res.Error)
		}
	}
}

func TestTextClassify_MessageIndexOutOfRangeErrors(t *testing.T) {
	srv := hfTextTestServer(t, []classify.LabelScore{{Label: "POSITIVE", Score: 0.9}})
	defer srv.Close()
	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextSentimentHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{
		Messages: []types.Message{textMessage("assistant", "only one")},
	}, map[string]any{
		"model":          "x/y",
		"expected_label": "POSITIVE",
		"message_index":  5,
	})
	if !strings.Contains(res.Error, "out of range") {
		t.Errorf("error %q should mention out-of-range", res.Error)
	}
}

func TestTextClassify_MultipartTextMerged(t *testing.T) {
	// Multi-part message: Content + a ContentTypeText part. Verify both
	// contribute by sending a model that always returns POSITIVE 0.99
	// regardless of input — we're just asserting no Error path is hit.
	srv := hfTextTestServer(t, []classify.LabelScore{{Label: "POSITIVE", Score: 0.99}})
	defer srv.Close()
	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextSentimentHandler{}
	textPart := "Additional sentence from a part."
	msg := types.Message{
		Role:    "assistant",
		Content: "Top-level content sentence.",
		Parts: []types.ContentPart{{
			Type: types.ContentTypeText,
			Text: &textPart,
		}},
	}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{msg}}, map[string]any{
		"model":          "x/y",
		"expected_label": "POSITIVE",
		"min_score":      0.5,
	})
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Skipped {
		t.Fatalf("expected pass, got skipped: %s", res.SkipReason)
	}
}

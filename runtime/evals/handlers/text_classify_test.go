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

// text_toxicity / text_sentiment are PURE EVAL PRIMITIVES — they emit
// the score for the configured expected_label and never apply a
// threshold. Threshold judgment is the job of `type: assertion`
// wrappers; see runtime/evals/handlers/CLAUDE.md.
//
// These tests exercise the pure-eval contract: Score = raw model score
// for expected_label (or 0 when absent), threshold params rejected,
// pure plumbing (registry resolution, role-based message selection,
// text extraction).

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
func textMessage(role, body string) types.Message {
	return types.Message{Role: role, Content: body}
}

func TestTextSentiment_EmitsScoreForExpectedLabel(t *testing.T) {
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
	})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected emit, got skipped: %s", res.SkipReason)
	}
	if res.Error != "" {
		t.Fatalf("expected emit, got error: %s", res.Error)
	}
	if res.Score == nil || *res.Score != 0.91 {
		t.Errorf("Score = %v, want 0.91", res.Score)
	}
	if res.Details["actual_score"].(float64) != 0.91 {
		t.Errorf("Details.actual_score = %v, want 0.91", res.Details["actual_score"])
	}
}

func TestTextToxicity_EmitsScoreForExpectedLabel(t *testing.T) {
	srv := hfTextTestServer(t, []classify.LabelScore{
		{Label: "toxic", Score: 0.84},
		{Label: "severe_toxic", Score: 0.41},
	})
	defer srv.Close()

	ctx := ctxWithTextRegistry(t, srv.URL)
	h := &TextToxicityHandler{}
	res, err := h.Eval(ctx, &evals.EvalContext{
		Messages: []types.Message{textMessage("assistant", "You are awful.")},
	}, map[string]any{
		"model":          "unitary/toxic-bert",
		"expected_label": "toxic",
	})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.Score == nil || *res.Score != 0.84 {
		t.Errorf("Score = %v, want 0.84", res.Score)
	}
}

func TestTextClassify_RejectsThresholdParams(t *testing.T) {
	// Threshold judgment is the job of `type: assertion`. Putting
	// min_score / max_score on the eval handler itself is a config
	// mistake; both classify-backed text handlers surface it loudly.
	ctx := classify.WithRegistry(context.Background(), classify.NewRegistry())
	type fixture struct {
		name string
		h    evals.EvalTypeHandler
	}
	for _, f := range []fixture{
		{name: "text_sentiment", h: &TextSentimentHandler{}},
		{name: "text_toxicity", h: &TextToxicityHandler{}},
	} {
		for _, banned := range []string{"min_score", "max_score"} {
			res, _ := f.h.Eval(ctx, &evals.EvalContext{}, map[string]any{
				"model":          "x/y",
				"expected_label": "POSITIVE",
				banned:           0.5,
			})
			if !strings.Contains(res.Error, banned+" is not a valid param") {
				t.Errorf("%s with %s should be rejected; got Error=%q",
					f.name, banned, res.Error)
			}
		}
	}
}

func TestTextClassify_LabelMissingFromModelOutputEmitsZero(t *testing.T) {
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
	if res.Score == nil || *res.Score != 0 {
		t.Errorf("Score = %v, want 0 (label not returned)", res.Score)
	}
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

func TestTextClassify_SkippedOnModelNotSupported(t *testing.T) {
	// Symmetric with audio_emotion: when HF surfaces ErrModelNotSupported,
	// the handler must route it to Skipped so keyless / free-tier demos
	// stay clean (see #1234).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"Model x/y is not supported for task text-classification on provider hf-inference"}`))
	}))
	defer srv.Close()
	ctx := ctxWithTextRegistry(t, srv.URL)

	h := &TextToxicityHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{
		Messages: []types.Message{textMessage("assistant", "hello")},
	}, map[string]any{
		"model":          "x/y",
		"expected_label": "toxic",
	})
	if !res.Skipped {
		t.Fatalf("expected Skipped on model-not-supported; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "not supported") {
		t.Errorf("SkipReason %q should explain the model isn't supported", res.SkipReason)
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
	// regardless of input — we're just asserting no Error path is hit
	// and the score flows through.
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
	})
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Skipped {
		t.Fatalf("expected emit, got skipped: %s", res.SkipReason)
	}
	if res.Score == nil || *res.Score != 0.99 {
		t.Errorf("Score = %v, want 0.99", res.Score)
	}
}

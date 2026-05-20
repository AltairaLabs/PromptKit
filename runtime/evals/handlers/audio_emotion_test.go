package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// audio_emotion is a PURE EVAL PRIMITIVE — it emits the score for the
// configured expected_label and never applies a threshold. Threshold
// judgment is the job of `type: assertion` wrappers; see
// runtime/evals/wrappers.go and runtime/evals/handlers/CLAUDE.md.
//
// These tests assert the pure-eval contract:
//   - Score = raw model score for expected_label (or 0 when absent)
//   - Details carries the structured info (expected_label, actual_score, all scores)
//   - min_score / max_score on the handler params is rejected
//
// Wrapper-driven pass/fail is exercised separately via the
// AssertionEvalHandler tests (runtime/evals/wrappers_test.go).

// hfTestServer returns an httptest.Server that hands back the provided
// HF audio-classification response JSON (or a 503 model-loading payload
// when loading is true). Keeping the helper close to the handler tests
// avoids reaching into the hf package's test fixtures.
func hfTestServer(t *testing.T, scores []classify.LabelScore, loading bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if loading {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"estimated_time": 0.05})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// HF audio-classification returns the flat [{label, score}] shape.
		_ = json.NewEncoder(w).Encode(scores)
	}))
}

func ctxWithRegistry(t *testing.T, srvURL string) context.Context {
	t.Helper()
	client, err := classifyhf.NewClient(classifyhf.Config{
		APIKey:  "test-token",
		BaseURL: srvURL,
	})
	if err != nil {
		t.Fatalf("hf client: %v", err)
	}
	reg := classify.NewRegistry()
	reg.RegisterAudio("hf", client)
	if err := reg.SetDefaultAudio("hf"); err != nil {
		t.Fatalf("SetDefaultAudio: %v", err)
	}
	return classify.WithRegistry(context.Background(), reg)
}

// audioMessage returns a single user message carrying base64-encoded
// audio. Content is opaque (the httptest server doesn't decode it) so a
// short literal is enough.
func audioMessage(role, body string) types.Message {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	return types.Message{
		Role: role,
		Parts: []types.ContentPart{{
			Type: types.ContentTypeAudio,
			Media: &types.MediaContent{
				Data:     &encoded,
				MIMEType: "audio/wav",
			},
		}},
	}
}

func TestAudioEmotion_EmitsScoreForExpectedLabel(t *testing.T) {
	srv := hfTestServer(t, []classify.LabelScore{
		{Label: "angry", Score: 0.82},
		{Label: "neutral", Score: 0.10},
	}, false)
	defer srv.Close()

	ctx := ctxWithRegistry(t, srv.URL)
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{audioMessage("user", "rawaudio")},
	}
	h := &AudioEmotionHandler{}
	res, err := h.Eval(ctx, evalCtx, map[string]any{
		"model":          "superb/wav2vec2-base-superb-er",
		"expected_label": "angry",
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
	if res.Score == nil || *res.Score != 0.82 {
		t.Errorf("Score = %v, want 0.82 (raw model score for the expected label)", res.Score)
	}
	if res.MetricValue == nil || *res.MetricValue != 0.82 {
		t.Errorf("MetricValue = %v, want 0.82", res.MetricValue)
	}
	if res.Details["expected_label"] != "angry" {
		t.Errorf("Details.expected_label = %v, want angry", res.Details["expected_label"])
	}
	if res.Details["actual_score"].(float64) != 0.82 {
		t.Errorf("Details.actual_score = %v, want 0.82", res.Details["actual_score"])
	}
}

func TestAudioEmotion_EmitsZeroWhenLabelNotReturned(t *testing.T) {
	srv := hfTestServer(t, []classify.LabelScore{
		{Label: "happy", Score: 0.9},
	}, false)
	defer srv.Close()

	ctx := ctxWithRegistry(t, srv.URL)
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{audioMessage("user", "rawaudio")},
	}
	h := &AudioEmotionHandler{}
	res, _ := h.Eval(ctx, evalCtx, map[string]any{
		"model":          "superb/wav2vec2-base-superb-er",
		"expected_label": "angry",
	})
	// Label absent from the model output → emit Score = 0 (raw signal:
	// the label's effective confidence is zero). Wrapper-supplied
	// thresholds (any positive min_score) will compute pass/fail
	// correctly from the zero.
	if res.Score == nil || *res.Score != 0 {
		t.Errorf("Score = %v, want 0 (label not returned)", res.Score)
	}
	if !strings.Contains(res.Explanation, "not returned by model") {
		t.Errorf("explanation %q should call out missing label", res.Explanation)
	}
	if !strings.Contains(res.Explanation, "happy") {
		t.Errorf("explanation %q should list the labels the model did return", res.Explanation)
	}
}

func TestAudioEmotion_SkippedOnModelLoading(t *testing.T) {
	srv := hfTestServer(t, nil, true)
	defer srv.Close()

	ctx := ctxWithRegistry(t, srv.URL)
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{audioMessage("user", "rawaudio")},
	}
	h := &AudioEmotionHandler{}
	res, err := h.Eval(ctx, evalCtx, map[string]any{
		"model":          "superb/wav2vec2-base-superb-er",
		"expected_label": "angry",
	})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !res.Skipped {
		t.Fatalf("expected skipped, got %+v", res)
	}
	if !strings.Contains(res.SkipReason, "loading") {
		t.Errorf("SkipReason %q should mention loading", res.SkipReason)
	}
}

func TestAudioEmotion_RejectsThresholdParams(t *testing.T) {
	// Threshold judgment is the job of `type: assertion`. Putting
	// min_score / max_score on the eval handler itself is a config
	// mistake; the handler should surface it loudly, not silently
	// accept a no-op param.
	ctx := classify.WithRegistry(context.Background(), classify.NewRegistry())
	h := &AudioEmotionHandler{}
	for _, banned := range []string{"min_score", "max_score"} {
		res, _ := h.Eval(ctx, &evals.EvalContext{}, map[string]any{
			"model":          "superb/wav2vec2-base-superb-er",
			"expected_label": "angry",
			banned:           0.5,
		})
		if !strings.Contains(res.Error, banned+" is not a valid param") {
			t.Errorf("%s should be rejected; got Error=%q", banned, res.Error)
		}
		if !strings.Contains(res.Error, "type: assertion") {
			t.Errorf("error should point to the assertion wrapper: %q", res.Error)
		}
	}
}

func TestAudioEmotion_MissingModelParam(t *testing.T) {
	ctx := classify.WithRegistry(context.Background(), classify.NewRegistry())
	h := &AudioEmotionHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{}, map[string]any{
		"expected_label": "angry",
	})
	if !strings.Contains(res.Error, "model is required") {
		t.Errorf("error %q should mention model required", res.Error)
	}
}

func TestAudioEmotion_MissingExpectedLabelParam(t *testing.T) {
	ctx := classify.WithRegistry(context.Background(), classify.NewRegistry())
	h := &AudioEmotionHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{}, map[string]any{
		"model": "some/model",
	})
	if !strings.Contains(res.Error, "expected_label is required") {
		t.Errorf("error %q should mention expected_label required", res.Error)
	}
}

func TestAudioEmotion_NoRegistryInContext(t *testing.T) {
	// Keyless-CI path: no inference provider declared, registry is nil,
	// assertion skips cleanly with a pointer at the missing wiring.
	h := &AudioEmotionHandler{}
	res, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"model":          "x/y",
		"expected_label": "angry",
	})
	if err != nil {
		t.Fatalf("Eval should not return Go error; got %v", err)
	}
	if !res.Skipped {
		t.Fatalf("expected Skipped when no registry; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "no classify registry configured") {
		t.Errorf("SkipReason %q should point users at the missing wiring", res.SkipReason)
	}
}

func TestAudioEmotion_NoAudioInMessages(t *testing.T) {
	srv := hfTestServer(t, nil, false)
	defer srv.Close()

	ctx := ctxWithRegistry(t, srv.URL)
	textOnly := types.Message{
		Role:  "user",
		Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: ptrString("hi")}},
	}
	h := &AudioEmotionHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{textOnly}}, map[string]any{
		"model":          "x/y",
		"expected_label": "angry",
	})
	if !res.Skipped {
		t.Fatalf("expected Skipped when no audio in messages; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "no audio part") {
		t.Errorf("SkipReason %q should explain why no audio was scored", res.SkipReason)
	}
}

func TestAudioEmotion_MessageIndexPicksSpecificPart(t *testing.T) {
	srv := hfTestServer(t, []classify.LabelScore{{Label: "angry", Score: 0.9}}, false)
	defer srv.Close()
	ctx := ctxWithRegistry(t, srv.URL)
	msgs := []types.Message{
		audioMessage("user", "first"),
		audioMessage("user", "second"),
	}
	h := &AudioEmotionHandler{}
	for _, idx := range []int{0, 1, -1} {
		res, _ := h.Eval(ctx, &evals.EvalContext{Messages: msgs}, map[string]any{
			"model":          "x/y",
			"expected_label": "angry",
			"message_index":  idx,
		})
		if res.Error != "" {
			t.Errorf("idx=%d unexpected error: %s", idx, res.Error)
		}
	}
}

func TestAudioEmotion_MessageIndexOutOfRange(t *testing.T) {
	srv := hfTestServer(t, []classify.LabelScore{{Label: "angry", Score: 0.9}}, false)
	defer srv.Close()
	ctx := ctxWithRegistry(t, srv.URL)
	h := &AudioEmotionHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{audioMessage("user", "one")}},
		map[string]any{
			"model":          "x/y",
			"expected_label": "angry",
			"message_index":  5,
		})
	if !strings.Contains(res.Error, "out of range") {
		t.Errorf("error %q should explain index is out of range", res.Error)
	}
}

func ptrString(s string) *string { return &s }

// TestAudioEmotion_StorageReferencePath proves the handler can read audio
// from a storage_reference that's a local-filesystem path — the shape the
// duplex pipeline produces when the local-storage media backend persists
// recordings.
func TestAudioEmotion_StorageReferencePath(t *testing.T) {
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(wavPath, []byte("fake-wav-bytes"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	srv := hfTestServer(t, []classify.LabelScore{{Label: "angry", Score: 0.91}}, false)
	defer srv.Close()
	ctx := ctxWithRegistry(t, srv.URL)

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{{
			Type: types.ContentTypeAudio,
			Media: &types.MediaContent{
				StorageReference: &wavPath,
				MIMEType:         "audio/wav",
			},
		}},
	}

	h := &AudioEmotionHandler{}
	res, err := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{msg}}, map[string]any{
		"model":          "superb/wav2vec2-base-superb-er",
		"expected_label": "angry",
	})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected score result, got skipped: %s", res.SkipReason)
	}
	if res.Error != "" {
		t.Fatalf("expected score result, got error: %s", res.Error)
	}
	if res.Score == nil || *res.Score != 0.91 {
		t.Errorf("Score = %v, want 0.91", res.Score)
	}
}

func TestAudioEmotion_StorageReferenceMissingFile(t *testing.T) {
	srv := hfTestServer(t, nil, false)
	defer srv.Close()
	ctx := ctxWithRegistry(t, srv.URL)

	missingPath := "/nonexistent/path/to.wav"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{{
			Type:  types.ContentTypeAudio,
			Media: &types.MediaContent{StorageReference: &missingPath, MIMEType: "audio/wav"},
		}},
	}

	h := &AudioEmotionHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{msg}}, map[string]any{
		"model":          "x/y",
		"expected_label": "angry",
	})
	if res.Error == "" {
		t.Errorf("expected error for unreadable storage_reference; got %+v", res)
	}
}

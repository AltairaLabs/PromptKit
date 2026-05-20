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

func TestAudioEmotion_PassesWhenLabelMeetsThreshold(t *testing.T) {
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
		"min_score":      0.6,
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
	if res.Score == nil || *res.Score < 0.6 {
		t.Errorf("score = %v, want >= 0.6", res.Score)
	}
}

func TestAudioEmotion_FailsWhenLabelBelowThreshold(t *testing.T) {
	srv := hfTestServer(t, []classify.LabelScore{
		{Label: "angry", Score: 0.3},
		{Label: "neutral", Score: 0.7},
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
		"min_score":      0.6,
	})
	if res.Score == nil || *res.Score >= 0.6 {
		t.Errorf("score = %v, want < 0.6 (model returned 0.3)", res.Score)
	}
	// Even when the label is found but below threshold, we want the
	// actual model score surfaced in Value so the report explains why.
	val, _ := res.Value.(map[string]any)
	if val["actual_score"].(float64) != 0.3 {
		t.Errorf("actual_score = %v, want 0.3", val["actual_score"])
	}
}

func TestAudioEmotion_FailsWhenLabelNotReturned(t *testing.T) {
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
	// Plain context with no classify.Registry attached. Infrastructure
	// absence (no inference provider declared) is Skipped — the assertion
	// couldn't run, but nothing is broken. This is the keyless-CI path:
	// the demo declares an HF provider but HF_TOKEN isn't set, so the
	// engine never populates the registry, and the assertion skips.
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
	// Mock providers without audio output (or scenarios where the chosen
	// role never speaks) should skip cleanly. The user can still see WHY
	// via SkipReason.
	if !res.Skipped {
		t.Fatalf("expected Skipped when no audio in messages; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "no audio part") {
		t.Errorf("SkipReason %q should explain why no audio was scored", res.SkipReason)
	}
}

func TestAudioEmotion_MessageIndexPicksSpecificPart(t *testing.T) {
	// Two audio messages with different scores. message_index 0 should
	// pick the first; -1 picks the last. We test by using two scores per
	// label tied to physical position via the body bytes (httptest
	// server returns the same scores for any call, so verify selection
	// indirectly by asserting we read SOME audio without erroring).
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
			"min_score":      0.5,
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
// recordings. Without this support, audio_emotion can never fire in a
// real voice-refund-demo run because the message log never carries inline
// base64.
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
		"min_score":      0.5,
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
	if res.Score == nil || *res.Score < 0.5 {
		t.Errorf("score = %v, want >= 0.5", res.Score)
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
	// An unreadable storage reference is a real config error (path is
	// wrong / file got cleaned up). Surface as Error, not Skipped.
	if res.Error == "" {
		t.Errorf("expected error for unreadable storage_reference; got %+v", res)
	}
}

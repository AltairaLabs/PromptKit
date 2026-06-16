package handlers

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// image_moderation is a PURE EVAL PRIMITIVE — it emits the score for the
// configured expected_label (e.g. "nsfw") and never applies a threshold.
// Threshold judgment is the job of `type: assertion` / `type: guardrail`
// wrappers. These tests assert that contract plus the Skipped/Error split,
// mirroring audio_emotion.

func ctxWithImageRegistry(t *testing.T, srvURL string) context.Context {
	t.Helper()
	client, err := classifyhf.NewClient(classifyhf.Config{
		APIKey:  "test-token",
		BaseURL: srvURL,
	})
	if err != nil {
		t.Fatalf("hf client: %v", err)
	}
	reg := classify.NewRegistry()
	reg.RegisterImage("hf", client)
	if err := reg.SetDefaultImage("hf"); err != nil {
		t.Fatalf("SetDefaultImage: %v", err)
	}
	return classify.WithRegistry(context.Background(), reg)
}

func imageMessage(role, body string) types.Message {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	return types.Message{
		Role: role,
		Parts: []types.ContentPart{{
			Type: types.ContentTypeImage,
			Media: &types.MediaContent{
				Data:     &encoded,
				MIMEType: "image/png",
			},
		}},
	}
}

func TestImageModeration_EmitsScoreForExpectedLabel(t *testing.T) {
	srv := hfTestServer(t, []classify.LabelScore{
		{Label: "nsfw", Score: 0.93},
		{Label: "normal", Score: 0.07},
	}, false)
	defer srv.Close()

	ctx := ctxWithImageRegistry(t, srv.URL)
	evalCtx := &evals.EvalContext{
		// Default role is the agent's output (assistant).
		Messages: []types.Message{imageMessage("assistant", "rawimg")},
	}
	h := &ImageModerationHandler{}
	res, err := h.Eval(ctx, evalCtx, map[string]any{
		"model":          "Falconsai/nsfw_image_detection",
		"expected_label": "nsfw",
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
	if res.Score == nil || *res.Score != 0.93 {
		t.Errorf("Score = %v, want 0.93 (raw model score for the expected label)", res.Score)
	}
	if res.MetricValue == nil || *res.MetricValue != 0.93 {
		t.Errorf("MetricValue = %v, want 0.93", res.MetricValue)
	}
	if res.Details["expected_label"] != "nsfw" {
		t.Errorf("Details.expected_label = %v, want nsfw", res.Details["expected_label"])
	}
}

// TestImageModeration_ModeratesToolResultImage is the across-the-board fix:
// an image produced by a tool (e.g. image__generate) lands in a tool-role
// message's ToolResult.Parts, not an assistant inline Part. The default role
// (assistant = the agent's output) must still find it, since the tool ran
// during the assistant's turn.
func TestImageModeration_ModeratesToolResultImage(t *testing.T) {
	srv := hfTestServer(t, []classify.LabelScore{
		{Label: "nsfw", Score: 0.71},
		{Label: "normal", Score: 0.29},
	}, false)
	defer srv.Close()

	ctx := ctxWithImageRegistry(t, srv.URL)
	encoded := base64.StdEncoding.EncodeToString([]byte("toolimg"))
	msgs := []types.Message{
		{Role: "user", Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: ptrString("make an image")}}},
		{Role: "assistant"},
		{Role: "tool", ToolResult: &types.MessageToolResult{
			Name:  "image__generate",
			Parts: []types.ContentPart{types.NewImagePartFromData(encoded, "image/png", nil)},
		}},
	}

	h := &ImageModerationHandler{}
	res, err := h.Eval(ctx, &evals.EvalContext{Messages: msgs}, map[string]any{
		"model":          "Falconsai/nsfw_image_detection",
		"expected_label": "nsfw",
	})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected score for tool-produced image, got skipped: %s", res.SkipReason)
	}
	if res.Score == nil || *res.Score != 0.71 {
		t.Errorf("Score = %v, want 0.71 (tool-result image scored)", res.Score)
	}
}

func TestImageModeration_NoRegistryInContext(t *testing.T) {
	h := &ImageModerationHandler{}
	res, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"model":          "Falconsai/nsfw_image_detection",
		"expected_label": "nsfw",
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

func TestImageModeration_NoImageInMessages(t *testing.T) {
	srv := hfTestServer(t, nil, false)
	defer srv.Close()
	ctx := ctxWithImageRegistry(t, srv.URL)

	textOnly := types.Message{
		Role:  "assistant",
		Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: ptrString("no image here")}},
	}
	h := &ImageModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{textOnly}}, map[string]any{
		"model":          "Falconsai/nsfw_image_detection",
		"expected_label": "nsfw",
	})
	if !res.Skipped {
		t.Fatalf("expected Skipped when no image in messages; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "no image part") {
		t.Errorf("SkipReason %q should explain why no image was scored", res.SkipReason)
	}
}

func TestImageModeration_SkippedOnModelLoading(t *testing.T) {
	srv := hfTestServer(t, nil, true)
	defer srv.Close()
	ctx := ctxWithImageRegistry(t, srv.URL)

	h := &ImageModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{imageMessage("assistant", "x")}},
		map[string]any{"model": "Falconsai/nsfw_image_detection", "expected_label": "nsfw"})
	if !res.Skipped {
		t.Fatalf("expected Skipped on model loading; got %+v", res)
	}
	if !strings.Contains(res.SkipReason, "loading") {
		t.Errorf("SkipReason %q should mention loading", res.SkipReason)
	}
}

func TestImageModeration_EmitsZeroWhenLabelNotReturned(t *testing.T) {
	srv := hfTestServer(t, []classify.LabelScore{{Label: "normal", Score: 0.99}}, false)
	defer srv.Close()
	ctx := ctxWithImageRegistry(t, srv.URL)

	h := &ImageModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{imageMessage("assistant", "x")}},
		map[string]any{"model": "Falconsai/nsfw_image_detection", "expected_label": "nsfw"})
	if res.Score == nil || *res.Score != 0 {
		t.Errorf("Score = %v, want 0 (label not returned)", res.Score)
	}
	if !strings.Contains(res.Explanation, "not returned by model") {
		t.Errorf("explanation %q should call out missing label", res.Explanation)
	}
}

func TestImageModeration_SkippedOnModelNotSupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"Model Falconsai/nsfw_image_detection is not supported for task image-classification on provider hf-inference"}`))
	}))
	defer srv.Close()
	ctx := ctxWithImageRegistry(t, srv.URL)

	h := &ImageModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{imageMessage("assistant", "x")}},
		map[string]any{"model": "Falconsai/nsfw_image_detection", "expected_label": "nsfw"})
	if !res.Skipped {
		t.Fatalf("expected Skipped on model-not-supported; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "not supported") {
		t.Errorf("SkipReason %q should explain the model isn't supported", res.SkipReason)
	}
}

func TestImageModeration_MessageIndexOutOfRange(t *testing.T) {
	srv := hfTestServer(t, []classify.LabelScore{{Label: "nsfw", Score: 0.4}}, false)
	defer srv.Close()
	ctx := ctxWithImageRegistry(t, srv.URL)

	h := &ImageModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{imageMessage("assistant", "one")}},
		map[string]any{
			"model":          "Falconsai/nsfw_image_detection",
			"expected_label": "nsfw",
			"message_index":  5,
		})
	if !strings.Contains(res.Error, "out of range") {
		t.Errorf("error %q should explain index is out of range", res.Error)
	}
}

func TestImageModeration_RejectsThresholdParams(t *testing.T) {
	ctx := classify.WithRegistry(context.Background(), classify.NewRegistry())
	h := &ImageModerationHandler{}
	for _, banned := range []string{"min_score", "max_score"} {
		res, _ := h.Eval(ctx, &evals.EvalContext{}, map[string]any{
			"model":          "Falconsai/nsfw_image_detection",
			"expected_label": "nsfw",
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

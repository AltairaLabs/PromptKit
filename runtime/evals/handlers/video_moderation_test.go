package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/classify/framesample"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// video_moderation is a PURE EVAL PRIMITIVE — it emits the score for the
// configured expected_label and never applies a threshold. These tests assert
// that contract plus the Skipped/Error split, mirroring image_moderation.

// fakeVideoClassifier is an injectable classify.VideoClassifier. It records the
// last VideoOptions it received so tests can assert per-call plumbing.
type fakeVideoClassifier struct {
	scores  []classify.LabelScore
	err     error
	gotOpts classify.VideoOptions
}

func (f *fakeVideoClassifier) ClassifyVideo(
	_ context.Context, _ []byte, opts classify.VideoOptions,
) ([]classify.LabelScore, error) {
	f.gotOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	return f.scores, nil
}

func ctxWithVideoRegistry(t *testing.T, vc classify.VideoClassifier) context.Context {
	t.Helper()
	reg := classify.NewRegistry()
	reg.RegisterVideo("decompose", vc)
	if err := reg.SetDefaultVideo("decompose"); err != nil {
		t.Fatalf("SetDefaultVideo: %v", err)
	}
	return classify.WithRegistry(context.Background(), reg)
}

func videoMessage(role, body string) types.Message {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	return types.Message{
		Role: role,
		Parts: []types.ContentPart{{
			Type: types.ContentTypeVideo,
			Media: &types.MediaContent{
				Data:     &encoded,
				MIMEType: "image/gif",
			},
		}},
	}
}

func TestVideoModeration_EmitsScoreForExpectedLabel(t *testing.T) {
	vc := &fakeVideoClassifier{scores: []classify.LabelScore{
		{Label: "nsfw", Score: 0.88},
		{Label: "safe", Score: 0.12},
	}}
	ctx := ctxWithVideoRegistry(t, vc)

	h := &VideoModerationHandler{}
	res, err := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{videoMessage("assistant", "gifbytes")}},
		map[string]any{"model": "Falconsai/nsfw_image_detection", "expected_label": "nsfw"})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected emit, got skipped: %s", res.SkipReason)
	}
	if res.Score == nil || *res.Score != 0.88 {
		t.Errorf("Score = %v, want 0.88", res.Score)
	}
	if res.MetricValue == nil || *res.MetricValue != 0.88 {
		t.Errorf("MetricValue = %v, want 0.88", res.MetricValue)
	}
}

func TestVideoModeration_PlumbsPerCallOptions(t *testing.T) {
	vc := &fakeVideoClassifier{scores: []classify.LabelScore{{Label: "nsfw", Score: 0.5}}}
	ctx := ctxWithVideoRegistry(t, vc)

	h := &VideoModerationHandler{}
	_, err := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{videoMessage("assistant", "g")}},
		map[string]any{
			"model":             "img-model",
			"expected_label":    "nsfw",
			"frame_sample_rate": 2.0,
			"extract_audio":     true,
			"aggregation":       "mean",
		})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if vc.gotOpts.Model != "img-model" {
		t.Errorf("Model = %q, want img-model", vc.gotOpts.Model)
	}
	if vc.gotOpts.FrameSampleRate != 2.0 {
		t.Errorf("FrameSampleRate = %v, want 2.0", vc.gotOpts.FrameSampleRate)
	}
	if !vc.gotOpts.ExtractAudio {
		t.Error("ExtractAudio = false, want true")
	}
	if vc.gotOpts.Aggregation != classify.AggregationMean {
		t.Errorf("Aggregation = %q, want mean", vc.gotOpts.Aggregation)
	}
	if vc.gotOpts.MIMEType != "image/gif" {
		t.Errorf("MIMEType = %q, want image/gif", vc.gotOpts.MIMEType)
	}
}

func TestVideoModeration_NoRegistryInContext(t *testing.T) {
	h := &VideoModerationHandler{}
	res, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"model": "m", "expected_label": "nsfw",
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

func TestVideoModeration_NoVideoInMessages(t *testing.T) {
	vc := &fakeVideoClassifier{}
	ctx := ctxWithVideoRegistry(t, vc)

	textOnly := types.Message{
		Role:  "assistant",
		Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: ptrString("no video here")}},
	}
	h := &VideoModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{textOnly}},
		map[string]any{"model": "m", "expected_label": "nsfw"})
	if !res.Skipped {
		t.Fatalf("expected Skipped when no video; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "no video part") {
		t.Errorf("SkipReason %q should explain why no video was scored", res.SkipReason)
	}
}

func TestVideoModeration_SkippedOnUnsupportedContainer(t *testing.T) {
	vc := &fakeVideoClassifier{err: framesample.ErrUnsupportedContainer}
	ctx := ctxWithVideoRegistry(t, vc)

	h := &VideoModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{videoMessage("assistant", "mp4bytes")}},
		map[string]any{"model": "m", "expected_label": "nsfw"})
	if !res.Skipped {
		t.Fatalf("expected Skipped on unsupported container; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "not decodable") {
		t.Errorf("SkipReason %q should explain the container isn't decodable", res.SkipReason)
	}
}

func TestVideoModeration_SkippedOnModelLoading(t *testing.T) {
	vc := &fakeVideoClassifier{err: classifyhf.ErrModelLoading}
	ctx := ctxWithVideoRegistry(t, vc)

	h := &VideoModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{videoMessage("assistant", "g")}},
		map[string]any{"model": "m", "expected_label": "nsfw"})
	if !res.Skipped {
		t.Fatalf("expected Skipped on model loading; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "loading") {
		t.Errorf("SkipReason %q should mention loading", res.SkipReason)
	}
}

func TestVideoModeration_SkippedOnModelNotSupported(t *testing.T) {
	vc := &fakeVideoClassifier{err: classifyhf.ErrModelNotSupported}
	ctx := ctxWithVideoRegistry(t, vc)

	h := &VideoModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{videoMessage("assistant", "g")}},
		map[string]any{"model": "m", "expected_label": "nsfw"})
	if !res.Skipped {
		t.Fatalf("expected Skipped on model-not-supported; got Error=%q", res.Error)
	}
	if !strings.Contains(res.SkipReason, "not supported") {
		t.Errorf("SkipReason %q should explain the model isn't supported", res.SkipReason)
	}
}

func TestVideoModeration_ErrorOnClassifierFailure(t *testing.T) {
	vc := &fakeVideoClassifier{err: errors.New("boom")}
	ctx := ctxWithVideoRegistry(t, vc)

	h := &VideoModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{videoMessage("assistant", "g")}},
		map[string]any{"model": "m", "expected_label": "nsfw"})
	if res.Skipped {
		t.Fatalf("a generic classifier failure should be Error, not Skipped: %s", res.SkipReason)
	}
	if !strings.Contains(res.Error, "classify failed") {
		t.Errorf("Error %q should surface the classifier failure", res.Error)
	}
}

func TestVideoModeration_InvalidAggregation(t *testing.T) {
	vc := &fakeVideoClassifier{}
	ctx := ctxWithVideoRegistry(t, vc)

	h := &VideoModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{videoMessage("assistant", "g")}},
		map[string]any{"model": "m", "expected_label": "nsfw", "aggregation": "median"})
	if !strings.Contains(res.Error, "invalid aggregation") {
		t.Errorf("error %q should reject the unknown aggregation", res.Error)
	}
}

func TestVideoModeration_MessageIndexOutOfRange(t *testing.T) {
	vc := &fakeVideoClassifier{scores: []classify.LabelScore{{Label: "nsfw", Score: 0.4}}}
	ctx := ctxWithVideoRegistry(t, vc)

	h := &VideoModerationHandler{}
	res, _ := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{videoMessage("assistant", "one")}},
		map[string]any{"model": "m", "expected_label": "nsfw", "message_index": 5})
	if !strings.Contains(res.Error, "out of range") {
		t.Errorf("error %q should explain index is out of range", res.Error)
	}
}

func TestVideoModeration_RejectsThresholdParams(t *testing.T) {
	ctx := classify.WithRegistry(context.Background(), classify.NewRegistry())
	h := &VideoModerationHandler{}
	for _, banned := range []string{"min_score", "max_score"} {
		res, _ := h.Eval(ctx, &evals.EvalContext{}, map[string]any{
			"model": "m", "expected_label": "nsfw", banned: 0.5,
		})
		if !strings.Contains(res.Error, banned+" is not a valid param") {
			t.Errorf("%s should be rejected; got Error=%q", banned, res.Error)
		}
		if !strings.Contains(res.Error, "type: assertion") {
			t.Errorf("error should point to the assertion wrapper: %q", res.Error)
		}
	}
}

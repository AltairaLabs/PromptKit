//go:build integration

package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/classify/framesample"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestVideoModeration_RealHFModel drives the whole decomposing pipeline against a
// live HuggingFace image-classification model:
//
//	GIF bytes → framesample (decode + PNG frames) → HF ImageClassifier per frame
//	→ per-label aggregation → video_moderation grade
//
// It proves the parts the unit tests fake: that the sampler emits frames a real
// model actually accepts, and that the model's labels flow back through
// aggregation to a graded score. It uses a benign, synthetic GIF — the point is
// the wiring, not NSFW content, so a valid "normal"/"safe" score is a pass.
//
// Run with: go test -tags=integration ./runtime/evals/handlers/ -run RealHFModel
// Requires HF_TOKEN. Skips (never fails) when the token is absent or the model
// isn't currently servable on the configured inference path.
func TestVideoModeration_RealHFModel(t *testing.T) {
	token := os.Getenv("HF_TOKEN")
	if token == "" {
		t.Skip("HF_TOKEN not set; skipping live HuggingFace video-moderation test")
	}

	const nsfwModel = "Falconsai/nsfw_image_detection"

	client, err := classifyhf.NewClient(classifyhf.Config{APIKey: token})
	if err != nil {
		t.Fatalf("hf client: %v", err)
	}

	// Compose the real decomposing video classifier over the live HF image model.
	vc, err := classify.NewDecomposingVideoClassifier(
		framesample.NewGIFMJPEGSampler(), client, nil,
		classify.DecomposeConfig{ImageModel: nsfwModel, Aggregation: classify.AggregationMax},
	)
	if err != nil {
		t.Fatalf("NewDecomposingVideoClassifier: %v", err)
	}
	reg := classify.NewRegistry()
	reg.RegisterVideo("decompose", vc)
	if err := reg.SetDefaultVideo("decompose"); err != nil {
		t.Fatalf("SetDefaultVideo: %v", err)
	}
	ctx := classify.WithRegistry(context.Background(), reg)

	// A real 3-frame animated GIF, base64-encoded into a video message part.
	encoded := base64.StdEncoding.EncodeToString(realAnimatedGIF(t))
	msg := types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{{
			Type:  types.ContentTypeVideo,
			Media: &types.MediaContent{Data: &encoded, MIMEType: "image/gif"},
		}},
	}

	// Cross-check against the classifier directly first so we can distinguish
	// "model unavailable" (skip) from a genuine handler bug (fail).
	if _, cerr := vc.ClassifyVideo(ctx, realAnimatedGIF(t), classify.VideoOptions{Model: nsfwModel}); cerr != nil {
		if errors.Is(cerr, classifyhf.ErrModelLoading) || errors.Is(cerr, classifyhf.ErrModelNotSupported) {
			t.Skipf("HF model %q not servable on the configured path: %v", nsfwModel, cerr)
		}
		t.Fatalf("ClassifyVideo against real model failed: %v", cerr)
	}

	h := &VideoModerationHandler{}
	res, err := h.Eval(ctx, &evals.EvalContext{Messages: []types.Message{msg}}, map[string]any{
		"model":          nsfwModel,
		"expected_label": "nsfw",
	})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.Skipped {
		t.Skipf("handler skipped (model unavailable): %s", res.SkipReason)
	}
	if res.Error != "" {
		t.Fatalf("handler errored against real model: %s", res.Error)
	}
	// The benign clip should score low for nsfw; the assertion here is that we got
	// a real, in-range score back for the requested label — the pipeline worked.
	if res.Score == nil {
		t.Fatal("expected a real nsfw score from the live model, got nil")
	}
	if *res.Score < 0 || *res.Score > 1 {
		t.Fatalf("nsfw score %v out of [0,1]", *res.Score)
	}
	t.Logf("live HF nsfw score for benign clip: %.4f (scores: %v)", *res.Score, res.Details["scores"])
}

// realAnimatedGIF builds a small, valid 3-frame animated GIF.
func realAnimatedGIF(t *testing.T) []byte {
	t.Helper()
	const w, h = 16, 16
	pal := color.Palette{color.RGBA{R: 20, G: 120, B: 200, A: 255}, color.White, color.Black}
	g := &gif.GIF{Config: image.Config{Width: w, Height: h}}
	for i := range 3 {
		img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for p := range img.Pix {
			img.Pix[p] = uint8(i % len(pal))
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 10)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

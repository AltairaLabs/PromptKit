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
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/classify/framesample"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// hfLiveTimeout is generous: HuggingFace serverless cold-starts a model on
// first hit and can take well over the client's 60s default before returning
// headers. A longer bound turns those cold starts into a real result instead
// of a client timeout.
const hfLiveTimeout = 180 * time.Second

// isTransientHFError reports whether err is a transient network / serverless
// hiccup (cold-start timeout, dropped connection) rather than a bug in our
// pipeline. The free HF Inference tier is inherently flaky, so the live test
// skips on these instead of going red on infrastructure it doesn't control.
func isTransientHFError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"context deadline exceeded", "timeout", "connection refused",
		"connection reset", "eof", "no such host", "temporarily",
		"status 502", "status 503", "status 504", "gateway", "bad gateway",
		"service unavailable",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

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

	// Default to the NSFW moderation model; allow an override for when HF's
	// free serverless tier isn't currently serving it (a general image
	// classifier still proves the decompose pipeline end to end).
	nsfwModel := "Falconsai/nsfw_image_detection"
	if m := os.Getenv("HF_VIDEO_TEST_MODEL"); m != "" {
		nsfwModel = m
	}

	client, err := classifyhf.NewClient(classifyhf.Config{
		APIKey:     token,
		HTTPClient: &http.Client{Timeout: hfLiveTimeout},
	})
	if err != nil {
		t.Fatalf("hf client: %v", err)
	}

	// Compose the real decomposing video classifier over the live HF image
	// model. One frame is enough to prove the pipeline end to end (GIF decode →
	// real HF classification → aggregation) while keeping the flaky free-tier
	// network surface to a single call.
	vc, err := classify.NewDecomposingVideoClassifier(
		framesample.NewGIFMJPEGSampler(), client, nil,
		classify.DecomposeConfig{ImageModel: nsfwModel, Aggregation: classify.AggregationMax, MaxFrames: 1},
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
	directScores, cerr := vc.ClassifyVideo(ctx, realAnimatedGIF(t), classify.VideoOptions{Model: nsfwModel})
	if cerr != nil {
		if errors.Is(cerr, classifyhf.ErrModelLoading) || errors.Is(cerr, classifyhf.ErrModelNotSupported) {
			t.Skipf("HF model %q not servable on the configured path: %v", nsfwModel, cerr)
		}
		if isTransientHFError(cerr) {
			t.Skipf("HF serverless transient error (not a pipeline bug): %v", cerr)
		}
		t.Fatalf("ClassifyVideo against real model failed: %v", cerr)
	}
	// The core proof: real frames went through a real model and came back with
	// real, ranked labels that aggregation collapsed into a result.
	if len(directScores) == 0 {
		t.Fatal("live model returned no labels for the sampled frames")
	}
	t.Logf("live %q returned %d aggregated labels; top: %+v", nsfwModel, len(directScores), directScores[0])

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
		if isTransientHFError(errors.New(res.Error)) {
			t.Skipf("HF serverless transient error via handler: %s", res.Error)
		}
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

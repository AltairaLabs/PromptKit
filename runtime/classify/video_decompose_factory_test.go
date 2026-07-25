package classify_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// fakeImage returns a fixed score for any frame, so a decompose provider
// built over it yields a predictable aggregate.
type fakeImage struct{}

func (fakeImage) ClassifyImage(_ context.Context, _ []byte, _ classify.ImageOptions) ([]classify.LabelScore, error) {
	return []classify.LabelScore{{Label: "nsfw", Score: 0.77}, {Label: "safe", Score: 0.23}}, nil
}

// tinyGIF builds a 3-frame animated GIF for the decompose path to sample.
func tinyGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White}
	g := &gif.GIF{Config: image.Config{Width: 4, Height: 4}}
	for i := range 3 {
		img := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
		for p := range img.Pix {
			img.Pix[p] = uint8(i % 2)
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 5)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func TestBuildRegistry_DecomposeProvider(t *testing.T) {
	classify.RegisterFactory("fakeimg_dcp", func(_ classify.ProviderSpec) (classify.Backend, error) {
		return fakeImage{}, nil
	})
	specs := []classify.ProviderSpec{
		{ID: "img", Type: "fakeimg_dcp"},
		{ID: "vid", Type: classify.DecomposeProviderType, AdditionalConfig: map[string]any{
			"image_classifier_id": "img",
			"aggregation":         classify.AggregationMax,
		}},
	}
	reg, err := classify.BuildRegistry(specs, classify.RegistryDefaults{})
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}

	// The composite is registered under its id and becomes the default video
	// classifier (first-wins).
	vc, err := reg.VideoClassifier("")
	if err != nil {
		t.Fatalf("default video classifier: %v", err)
	}

	scores, err := vc.ClassifyVideo(context.Background(), tinyGIF(t), classify.VideoOptions{})
	if err != nil {
		t.Fatalf("ClassifyVideo: %v", err)
	}
	if len(scores) == 0 || scores[0].Label != "nsfw" || scores[0].Score != 0.77 {
		t.Fatalf("aggregate scores = %v, want top nsfw 0.77", scores)
	}
}

func TestBuildRegistry_DecomposeWithAudio(t *testing.T) {
	// fakeAll (from factory_test.go) satisfies both image and audio, so one
	// backend can back both refs — exercises the audio-resolution branch.
	classify.RegisterFactory("fakeav_dcp", func(_ classify.ProviderSpec) (classify.Backend, error) {
		return fakeAll{}, nil
	})
	specs := []classify.ProviderSpec{
		{ID: "av", Type: "fakeav_dcp"},
		{ID: "vid", Type: classify.DecomposeProviderType, AdditionalConfig: map[string]any{
			"image_classifier_id": "av",
			"audio_classifier_id": "av",
			"audio_model":         "audio-model",
			"frame_sample_rate":   1,
			"max_frames":          8,
		}},
	}
	reg, err := classify.BuildRegistry(specs, classify.RegistryDefaults{})
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if _, err := reg.VideoClassifier("vid"); err != nil {
		t.Fatalf("video classifier should resolve: %v", err)
	}
}

func TestBuildRegistry_DecomposeUnknownAudioRef(t *testing.T) {
	classify.RegisterFactory("fakeimg_dcp2", func(_ classify.ProviderSpec) (classify.Backend, error) {
		return fakeImage{}, nil
	})
	specs := []classify.ProviderSpec{
		{ID: "img", Type: "fakeimg_dcp2"},
		{ID: "vid", Type: classify.DecomposeProviderType, AdditionalConfig: map[string]any{
			"image_classifier_id": "img",
			"audio_classifier_id": "missing-audio",
		}},
	}
	_, err := classify.BuildRegistry(specs, classify.RegistryDefaults{})
	if err == nil {
		t.Fatal("expected error when audio_classifier_id references an unregistered backend")
	}
}

func TestBuildRegistry_DecomposeMissingImageID(t *testing.T) {
	specs := []classify.ProviderSpec{
		{ID: "vid", Type: classify.DecomposeProviderType, AdditionalConfig: map[string]any{}},
	}
	_, err := classify.BuildRegistry(specs, classify.RegistryDefaults{})
	if err == nil {
		t.Fatal("expected error when image_classifier_id is missing")
	}
}

func TestBuildRegistry_DecomposeUnknownImageRef(t *testing.T) {
	specs := []classify.ProviderSpec{
		{ID: "vid", Type: classify.DecomposeProviderType, AdditionalConfig: map[string]any{
			"image_classifier_id": "does-not-exist",
		}},
	}
	_, err := classify.BuildRegistry(specs, classify.RegistryDefaults{})
	if err == nil {
		t.Fatal("expected error when image_classifier_id references an unregistered backend")
	}
}

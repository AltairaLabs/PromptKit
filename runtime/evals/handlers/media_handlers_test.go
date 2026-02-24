package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func intPtr(v int) *int       { return &v }
func floatPtr(v float64) *float64 { return &v } //nolint:unparam // test helper

func imageMsg(mime string, width, height *int) types.Message {
	return types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					MIMEType: mime,
					Width:    width,
					Height:   height,
				},
			},
		},
	}
}

func audioMsg(mime string, duration *int) types.Message {
	return types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					MIMEType: mime,
					Duration: duration,
				},
			},
		},
	}
}

func videoMsg(mime string, width, height *int, duration *int) types.Message {
	return types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeVideo,
				Media: &types.MediaContent{
					MIMEType: mime,
					Width:    width,
					Height:   height,
					Duration: duration,
				},
			},
		},
	}
}

// --- ImageFormat ---

func TestImageFormatHandler_Type(t *testing.T) {
	h := &ImageFormatHandler{}
	if h.Type() != "image_format" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestImageFormatHandler_Pass(t *testing.T) {
	h := &ImageFormatHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{imageMsg("image/jpeg", intPtr(100), intPtr(100))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"formats": []any{"jpeg", "png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestImageFormatHandler_InvalidFormat(t *testing.T) {
	h := &ImageFormatHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{imageMsg("image/gif", intPtr(100), intPtr(100))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"formats": []any{"jpeg", "png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for disallowed format")
	}
}

func TestImageFormatHandler_NoImages(t *testing.T) {
	h := &ImageFormatHandler{}
	evalCtx := &evals.EvalContext{Messages: []types.Message{assistantMsg("hello")}}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"formats": []any{"jpeg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for no images")
	}
}

// --- ImageDimensions ---

func TestImageDimensionsHandler_Type(t *testing.T) {
	h := &ImageDimensionsHandler{}
	if h.Type() != "image_dimensions" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestImageDimensionsHandler_Pass(t *testing.T) {
	h := &ImageDimensionsHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{imageMsg("image/jpeg", intPtr(800), intPtr(600))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"min_width": 100, "max_width": 1000,
		"min_height": 100, "max_height": 800,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestImageDimensionsHandler_ExactMatch(t *testing.T) {
	h := &ImageDimensionsHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{imageMsg("image/jpeg", intPtr(800), intPtr(600))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"width": 800, "height": 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestImageDimensionsHandler_TooWide(t *testing.T) {
	h := &ImageDimensionsHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{imageMsg("image/jpeg", intPtr(2000), intPtr(600))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_width": 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for exceeding max_width")
	}
}

// --- AudioFormat ---

func TestAudioFormatHandler_Type(t *testing.T) {
	h := &AudioFormatHandler{}
	if h.Type() != "audio_format" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestAudioFormatHandler_Pass(t *testing.T) {
	h := &AudioFormatHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{audioMsg("audio/mpeg", intPtr(30))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"formats": []any{"mp3", "wav"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

// --- AudioDuration ---

func TestAudioDurationHandler_Type(t *testing.T) {
	h := &AudioDurationHandler{}
	if h.Type() != "audio_duration" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestAudioDurationHandler_Pass(t *testing.T) {
	h := &AudioDurationHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{audioMsg("audio/mpeg", intPtr(30))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"min_seconds": 10.0,
		"max_seconds": 60.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestAudioDurationHandler_TooShort(t *testing.T) {
	h := &AudioDurationHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{audioMsg("audio/mpeg", intPtr(5))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"min_seconds": 10.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for too short")
	}
}

// --- VideoDuration ---

func TestVideoDurationHandler_Type(t *testing.T) {
	h := &VideoDurationHandler{}
	if h.Type() != "video_duration" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestVideoDurationHandler_Pass(t *testing.T) {
	h := &VideoDurationHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", intPtr(1920), intPtr(1080), intPtr(60))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"min_seconds": 30.0,
		"max_seconds": 120.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

// --- VideoResolution ---

func TestVideoResolutionHandler_Type(t *testing.T) {
	h := &VideoResolutionHandler{}
	if h.Type() != "video_resolution" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestVideoResolutionHandler_PresetMatch(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", intPtr(1920), intPtr(1080), intPtr(60))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"presets": []any{"1080p", "720p"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestVideoResolutionHandler_PresetNoMatch(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", intPtr(1920), intPtr(1080), intPtr(60))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"presets": []any{"720p"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for preset mismatch")
	}
}

func TestVideoResolutionHandler_DimensionRange(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", intPtr(1920), intPtr(1080), nil)},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"min_width": 1280, "min_height": 720,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestVideoResolutionHandler_NoVideos(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{assistantMsg("hello")},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"presets": []any{"1080p"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for no videos")
	}
}

func TestVideoResolutionHandler_MissingMetadata(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", nil, nil, intPtr(60))},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"presets": []any{"1080p"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing width/height metadata")
	}
}

func TestVideoResolutionHandler_AllPresets(t *testing.T) {
	tests := []struct {
		preset string
		height int
	}{
		{"480p", 480},
		{"sd", 480},
		{"720p", 720},
		{"hd", 720},
		{"1080p", 1080},
		{"fhd", 1080},
		{"full_hd", 1080},
		{"1440p", 1440},
		{"2k", 1440},
		{"qhd", 1440},
		{"2160p", 2160},
		{"4k", 2160},
		{"uhd", 2160},
		{"4320p", 4320},
		{"8k", 4320},
	}

	h := &VideoResolutionHandler{}
	for _, tt := range tests {
		evalCtx := &evals.EvalContext{
			Messages: []types.Message{videoMsg("video/mp4", intPtr(1920), intPtr(tt.height), nil)},
		}
		result, err := h.Eval(context.Background(), evalCtx, map[string]any{
			"presets": []any{tt.preset},
		})
		if err != nil {
			t.Fatalf("preset %q: %v", tt.preset, err)
		}
		if !result.Passed {
			t.Errorf("preset %q with height %d should pass: %s", tt.preset, tt.height, result.Explanation)
		}
	}
}

func TestVideoResolutionHandler_UnknownPreset(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", intPtr(1920), intPtr(1080), nil)},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"presets": []any{"unknown_preset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for unknown preset")
	}
}

func TestVideoResolutionHandler_WidthRange(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", intPtr(800), intPtr(600), nil)},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"min_width": 1280,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for width below min_width")
	}
}

func TestVideoResolutionHandler_MaxWidth(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", intPtr(3840), intPtr(2160), nil)},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_width": 1920,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for width above max_width")
	}
}

func TestVideoResolutionHandler_HeightRange(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", intPtr(1920), intPtr(400), nil)},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"min_height": 720,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for height below min_height")
	}
}

func TestVideoResolutionHandler_MaxHeight(t *testing.T) {
	h := &VideoResolutionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{videoMsg("video/mp4", intPtr(1920), intPtr(1440), nil)},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_height": 1080,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for height above max_height")
	}
}

func TestExtractFormatFromMIMEType(t *testing.T) {
	tests := []struct {
		mime, expected string
	}{
		{"image/jpeg", "jpeg"},
		{"image/png", "png"},
		{"audio/mpeg", "mp3"},
		{"video/mp4", "mp4"},
		{"invalid", "invalid"},
	}
	for _, tt := range tests {
		got := extractFormatFromMIMEType(tt.mime)
		if got != tt.expected {
			t.Errorf("extractFormatFromMIMEType(%q) = %q, want %q", tt.mime, got, tt.expected)
		}
	}
}

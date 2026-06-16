package mediagen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// fakeProvider is a configurable base.Provider used to back both image and
// video provider fakes in these tests.
type fakeProvider struct {
	name string
	typ  base.ProviderType
}

func (f *fakeProvider) Name() string                      { return f.name }
func (f *fakeProvider) Type() base.ProviderType           { return f.typ }
func (f *fakeProvider) Pricing() *base.PricingDescriptor  { return nil }
func (f *fakeProvider) Validate() error                   { return nil }
func (f *fakeProvider) Init(context.Context) error        { return nil }
func (f *fakeProvider) HealthCheck(context.Context) error { return nil }
func (f *fakeProvider) Close() error                      { return nil }

// fakeImageProvider implements base.ImageProvider.
type fakeImageProvider struct {
	fakeProvider
	resp base.ImageResponse
	err  error
}

func newFakeImageProvider(name string, images [][]byte, mime string) *fakeImageProvider {
	return &fakeImageProvider{
		fakeProvider: fakeProvider{name: name, typ: base.ProviderTypeImage},
		resp:         base.ImageResponse{Images: images, MIMEType: mime},
	}
}

func (f *fakeImageProvider) Generate(context.Context, base.ImageRequest) (base.ImageResponse, error) {
	if f.err != nil {
		return base.ImageResponse{}, f.err
	}
	return f.resp, nil
}

// fakeVideoProvider implements base.VideoProvider.
type fakeVideoProvider struct {
	fakeProvider
	resp base.VideoResponse
}

func newFakeVideoProvider(name string, videos [][]byte, mime string) *fakeVideoProvider {
	return &fakeVideoProvider{
		fakeProvider: fakeProvider{name: name, typ: base.ProviderTypeVideo},
		resp:         base.VideoResponse{Videos: videos, MIMEType: mime},
	}
}

func (f *fakeVideoProvider) Generate(context.Context, base.VideoRequest) (base.VideoResponse, error) {
	return f.resp, nil
}

func mustRegister(t *testing.T, r *base.Registry, p base.Provider) {
	t.Helper()
	if err := r.Register(p); err != nil {
		t.Fatalf("register %q: %v", p.Name(), err)
	}
}

func imageCall(t *testing.T, e *Executor, prompt string) (json.RawMessage, []types.ContentPart, error) {
	t.Helper()
	args, _ := json.Marshal(imageArgs{Prompt: prompt})
	return e.ExecuteMultimodal(context.Background(), ImageGenerateDescriptor(), args)
}

func TestExecutor_GenerateImage_HappyPath(t *testing.T) {
	pool := base.NewRegistry()
	mustRegister(t, pool, newFakeImageProvider("imagen", [][]byte{[]byte("YmFzZTY0aW1n")}, types.MIMETypeImagePNG))
	e := NewExecutor(pool)

	summary, parts, err := imageCall(t, e, "a fox in snow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	part := parts[0]
	if part.Type != types.ContentTypeImage || part.Media == nil {
		t.Fatalf("expected image media part, got %+v", part)
	}
	if part.Media.Data == nil || *part.Media.Data != "YmFzZTY0aW1n" {
		t.Fatalf("expected base64 data passed through, got %v", part.Media.Data)
	}
	if part.Media.MIMEType != types.MIMETypeImagePNG {
		t.Fatalf("unexpected mime %q", part.Media.MIMEType)
	}

	var sum map[string]any
	if err := json.Unmarshal(summary, &sum); err != nil {
		t.Fatalf("summary not valid json: %v", err)
	}
	if sum["provider"] != "imagen" {
		t.Fatalf("unexpected summary provider: %v", sum["provider"])
	}
}

func TestExecutor_GenerateImage_NoProvider(t *testing.T) {
	e := NewExecutor(base.NewRegistry())
	_, _, err := imageCall(t, e, "anything")
	if err == nil || !strings.Contains(err.Error(), "no image provider configured") {
		t.Fatalf("expected no-image-provider error, got %v", err)
	}
}

func TestExecutor_GenerateImage_NilPool(t *testing.T) {
	e := NewExecutor(nil)
	_, _, err := imageCall(t, e, "anything")
	if err == nil || !strings.Contains(err.Error(), "no image provider configured") {
		t.Fatalf("expected no-image-provider error, got %v", err)
	}
}

func TestExecutor_GenerateImage_EmptyPrompt(t *testing.T) {
	pool := base.NewRegistry()
	mustRegister(t, pool, newFakeImageProvider("imagen", [][]byte{[]byte("x")}, types.MIMETypeImagePNG))
	e := NewExecutor(pool)
	_, _, err := imageCall(t, e, "")
	if err == nil || !strings.Contains(err.Error(), "non-empty prompt") {
		t.Fatalf("expected empty-prompt error, got %v", err)
	}
}

func TestExecutor_GenerateImage_ProviderReturnsNoImages(t *testing.T) {
	pool := base.NewRegistry()
	mustRegister(t, pool, newFakeImageProvider("imagen", nil, types.MIMETypeImagePNG))
	e := NewExecutor(pool)
	_, _, err := imageCall(t, e, "prompt")
	if err == nil || !strings.Contains(err.Error(), "returned no images") {
		t.Fatalf("expected no-images error, got %v", err)
	}
}

// TestExecutor_GenerateImage_DeterministicDefault verifies the lexically-first
// provider by Name() is chosen regardless of registration/map order.
func TestExecutor_GenerateImage_DeterministicDefault(t *testing.T) {
	pool := base.NewRegistry()
	mustRegister(t, pool, newFakeImageProvider("zeta", [][]byte{[]byte("zzz")}, types.MIMETypeImagePNG))
	mustRegister(t, pool, newFakeImageProvider("alpha", [][]byte{[]byte("aaa")}, types.MIMETypeImageJPEG))
	e := NewExecutor(pool)

	summary, _, err := imageCall(t, e, "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sum map[string]any
	_ = json.Unmarshal(summary, &sum)
	if sum["provider"] != "alpha" {
		t.Fatalf("expected lexically-first provider 'alpha', got %v", sum["provider"])
	}
}

func TestExecutor_GenerateVideo_NoProvider(t *testing.T) {
	e := NewExecutor(base.NewRegistry())
	args, _ := json.Marshal(videoArgs{Prompt: "a timelapse"})
	_, _, err := e.ExecuteMultimodal(context.Background(), VideoGenerateDescriptor(), args)
	if err == nil || !strings.Contains(err.Error(), "no video provider configured") {
		t.Fatalf("expected no-video-provider error, got %v", err)
	}
}

// TestExecutor_GenerateVideo_HappyPath exercises the video path with a fake
// provider even though no concrete video provider ships yet.
func TestExecutor_GenerateVideo_HappyPath(t *testing.T) {
	pool := base.NewRegistry()
	mustRegister(t, pool, newFakeVideoProvider("veo", [][]byte{[]byte("dmlkZW8=")}, types.MIMETypeVideoMP4))
	e := NewExecutor(pool)

	args, _ := json.Marshal(videoArgs{Prompt: "a timelapse", AspectRatio: "16:9"})
	_, parts, err := e.ExecuteMultimodal(context.Background(), VideoGenerateDescriptor(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 1 || parts[0].Type != types.ContentTypeVideo {
		t.Fatalf("expected 1 video part, got %+v", parts)
	}
}

func TestExecutor_UnsupportedTool(t *testing.T) {
	e := NewExecutor(base.NewRegistry())
	desc := ImageGenerateDescriptor()
	desc.Name = "image__unknown"
	_, _, err := e.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported tool") {
		t.Fatalf("expected unsupported-tool error, got %v", err)
	}
}

func TestExecutor_NameMatchesMode(t *testing.T) {
	e := NewExecutor(nil)
	if e.Name() != ExecutorMode {
		t.Fatalf("Name() %q must equal ExecutorMode %q", e.Name(), ExecutorMode)
	}
	if ImageGenerateDescriptor().Mode != ExecutorMode || VideoGenerateDescriptor().Mode != ExecutorMode {
		t.Fatalf("descriptors must use mode %q", ExecutorMode)
	}
}

// compile-time guard: Executor satisfies tools.MultimodalExecutor.
var _ tools.MultimodalExecutor = (*Executor)(nil)

package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/mediagen"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// mediagenFakeImageProvider is a minimal base.ImageProvider for Arena wiring tests.
type mediagenFakeImageProvider struct{ name string }

func (f *mediagenFakeImageProvider) Name() string                      { return f.name }
func (f *mediagenFakeImageProvider) Type() base.ProviderType           { return base.ProviderTypeImage }
func (f *mediagenFakeImageProvider) Pricing() *base.PricingDescriptor  { return nil }
func (f *mediagenFakeImageProvider) Validate() error                   { return nil }
func (f *mediagenFakeImageProvider) Init(context.Context) error        { return nil }
func (f *mediagenFakeImageProvider) HealthCheck(context.Context) error { return nil }
func (f *mediagenFakeImageProvider) Close() error                      { return nil }
func (f *mediagenFakeImageProvider) Generate(
	context.Context, base.ImageRequest,
) (base.ImageResponse, error) {
	return base.ImageResponse{}, nil
}

// TestRegisterMediaGenTools_Arena_ImagePresent verifies Arena registers the image
// tool when an image provider is pooled, and leaves video__generate absent.
func TestRegisterMediaGenTools_Arena_ImagePresent(t *testing.T) {
	pr := providers.NewRegistry()
	if err := pr.Base().Register(&mediagenFakeImageProvider{name: "imagen"}); err != nil {
		t.Fatalf("register fake image provider: %v", err)
	}
	reg := tools.NewRegistry()

	registerMediaGenTools(reg, pr)

	if reg.Get(mediagen.ImageGenerateToolName) == nil {
		t.Fatalf("expected %s registered when an image provider is pooled", mediagen.ImageGenerateToolName)
	}
	if reg.Get(mediagen.VideoGenerateToolName) != nil {
		t.Fatalf("did not expect %s registered (no video provider)", mediagen.VideoGenerateToolName)
	}
}

// TestRegisterMediaGenTools_Arena_NoProvider verifies neither tool is registered
// without a matching provider in the pool.
func TestRegisterMediaGenTools_Arena_NoProvider(t *testing.T) {
	reg := tools.NewRegistry()
	registerMediaGenTools(reg, providers.NewRegistry())

	if reg.Get(mediagen.ImageGenerateToolName) != nil {
		t.Fatalf("%s must be absent without an image provider", mediagen.ImageGenerateToolName)
	}
	if reg.Get(mediagen.VideoGenerateToolName) != nil {
		t.Fatalf("%s must be absent without a video provider", mediagen.VideoGenerateToolName)
	}
}

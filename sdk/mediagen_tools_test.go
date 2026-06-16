package sdk

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/mediagen"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// mediagenFakeImageProvider is a minimal base.ImageProvider for wiring tests.
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

func newMediagenTestConv() *Conversation {
	return &Conversation{config: &config{}, toolRegistry: tools.NewRegistry()}
}

// TestRegisterMediaGenTools_ImageProviderPresent verifies the image tool is
// registered (and is a system tool, so exposed without an allowlist entry) when
// an image provider is in the pool, while video__generate stays absent because
// no video provider exists.
func TestRegisterMediaGenTools_ImageProviderPresent(t *testing.T) {
	conv := newMediagenTestConv()
	ensureProviderPool(conv.config)
	if err := conv.config.providers.Base().Register(&mediagenFakeImageProvider{name: "imagen"}); err != nil {
		t.Fatalf("register fake image provider: %v", err)
	}

	conv.registerMediaGenTools()

	if conv.toolRegistry.Get(mediagen.ImageGenerateToolName) == nil {
		t.Fatalf("expected %s to be registered when an image provider is pooled", mediagen.ImageGenerateToolName)
	}
	if conv.toolRegistry.Get(mediagen.VideoGenerateToolName) != nil {
		t.Fatalf("did not expect %s registered (no video provider)", mediagen.VideoGenerateToolName)
	}
	if !tools.IsSystemTool(mediagen.ImageGenerateToolName) {
		t.Fatalf("%s must be a system tool so it is exposed without a prompt tools entry",
			mediagen.ImageGenerateToolName)
	}
}

// TestRegisterMediaGenTools_NoProvider verifies neither tool is registered when
// no matching provider is in the pool (capability-gated registration).
func TestRegisterMediaGenTools_NoProvider(t *testing.T) {
	conv := newMediagenTestConv()
	conv.registerMediaGenTools()

	if conv.toolRegistry.Get(mediagen.ImageGenerateToolName) != nil {
		t.Fatalf("%s must be absent without an image provider", mediagen.ImageGenerateToolName)
	}
	if conv.toolRegistry.Get(mediagen.VideoGenerateToolName) != nil {
		t.Fatalf("%s must be absent without a video provider", mediagen.VideoGenerateToolName)
	}
}

package claude

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStreamingPaths_CustomHeaderCollision verifies that the streaming
// code paths (both tool and non-tool) check for custom-header collisions
// before dispatching the request. The non-tool streaming path is
// exercised via the Provider, and the tool streaming path via the
// ToolProvider's request builders. Both share applyRequestHeaders /
// applyToolRequestHeaders so this test covers every branch that runs
// custom headers through the streaming pipeline.
func TestStreamingPaths_CustomHeaderCollision(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	spec := providers.ProviderSpec{
		ID:      "test-claude-stream",
		Type:    "claude",
		Model:   "claude-3-5-sonnet-20241022",
		BaseURL: "https://example.invalid",
		Headers: map[string]string{
			"X-Api-Key": "conflict",
		},
	}

	provider, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}

	tp, ok := provider.(*ToolProvider)
	if !ok {
		t.Fatalf("provider is not *ToolProvider, got %T", provider)
	}

	// Drive the tool-streaming request builders directly.
	bedrockFn := tp.buildBedrockStreamingRequestFn("https://example.invalid/stream", []byte(`{}`))
	if _, err := bedrockFn(context.Background()); err == nil {
		t.Error("expected collision error from buildBedrockStreamingRequestFn, got nil")
	}

	directFn := tp.buildDirectStreamingRequestFn("https://example.invalid/stream", []byte(`{}`))
	if _, err := directFn(context.Background()); err == nil {
		t.Error("expected collision error from buildDirectStreamingRequestFn, got nil")
	}

	// Drive the non-tool streaming path via PredictStream. The collision
	// happens at request-build time, before any network I/O.
	_, err = provider.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected collision error from PredictStream, got nil")
	}
}

package stage

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)

// TestProviderStage_ToolHistorySurvivesEmptyToolSet_Live pins issue #1735 at the
// wire level. The unit tests assert which provider entry point the stage picks;
// this asserts what that choice actually does to the request, because the damage
// happens inside the provider's serializer rather than in the stage.
//
// It exercises both routes against the live API:
//   - provider.PredictStream — the pre-fix route. Its serializer builds
//     []openAIMessage, which has no tool_calls / tool_call_id field, so the
//     linkage is stripped and OpenAI rejects the array with a 400.
//   - stage.startStreamingRequest with a nil tool set — the fixed route, which
//     keeps the tool-aware serializer and is accepted.
//
// Skips without OPENAI_API_KEY; hits the paid API when it runs.
func TestProviderStage_ToolHistorySurvivesEmptyToolSet_Live(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("no OPENAI_API_KEY, skipping live tool-routing integration test")
	}

	provider := openai.NewToolProvider(
		"openai", "gpt-4o-mini", "https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 64}, false, nil, nil,
	)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{MaxTokens: 64})
	req := providers.PredictionRequest{Messages: historyWithToolLinkage(), MaxTokens: 64}

	drain := func(ch <-chan providers.StreamChunk) error {
		for c := range ch {
			if c.Error != nil {
				return c.Error
			}
		}
		return nil
	}

	// The plain path must still be demonstrably lossy. If OpenAI ever starts
	// accepting this array the fix stops being load-bearing, and that should
	// surface here rather than let the test silently keep passing.
	err := func() error {
		ch, streamErr := provider.PredictStream(context.Background(), req)
		if streamErr != nil {
			return streamErr
		}
		return drain(ch)
	}()
	if err == nil {
		t.Error("plain path unexpectedly accepted tool history; #1735 premise no longer holds")
	} else if !strings.Contains(err.Error(), "tool_calls") {
		t.Errorf("expected a tool-linkage rejection, got: %v", err)
	}

	// Same nil tool set, routed through the stage: must be accepted.
	err = func() error {
		ch, streamErr := stage.startStreamingRequest(context.Background(), req, nil, "")
		if streamErr != nil {
			return streamErr
		}
		return drain(ch)
	}()
	if err != nil {
		t.Fatalf("tool-aware path rejected tool history with an empty tool set: %v", err)
	}
}

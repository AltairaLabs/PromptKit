package gemini

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestExplicitCaching_Vertex_Live exercises explicit context caching against a
// real Vertex AI project, proving the CachedContent resource is created on the
// regional aiplatform endpoint (Bearer auth) and hits immediately.
//
// Gated: set GEMINI_LIVE_VERTEX=1 and VERTEX_PROJECT=<project> (VERTEX_REGION
// defaults to us-central1). Auth uses Application Default Credentials
// (gcloud auth application-default login). Never runs in CI.
func TestExplicitCaching_Vertex_Live(t *testing.T) {
	if os.Getenv("GEMINI_LIVE_VERTEX") != "1" || os.Getenv("VERTEX_PROJECT") == "" {
		t.Skip("set GEMINI_LIVE_VERTEX=1 and VERTEX_PROJECT to run the live Vertex caching test")
	}
	project := os.Getenv("VERTEX_PROJECT")
	region := os.Getenv("VERTEX_REGION")
	if region == "" {
		region = "us-central1"
	}

	ctx := context.Background()
	cred, err := credentials.NewGCPCredential(ctx, project, region)
	if err != nil {
		t.Fatalf("NewGCPCredential (is ADC set up?): %v", err)
	}

	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "live-vertex-gemini", Type: "gemini", Model: "gemini-2.5-flash",
		Platform:         "vertex",
		PlatformConfig:   &providers.PlatformConfig{Type: "vertex", Region: region, Project: project},
		Credential:       cred,
		AdditionalConfig: map[string]any{"explicit_caching": true},
		Defaults:         providers.ProviderDefaults{MaxTokens: 1024},
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	tp := provider.(*ToolProvider)
	t.Cleanup(func() {
		for _, name := range tp.cache.trackedNames() {
			tp.deleteCachedContent(context.Background(), name)
		}
	})

	for round := 1; round <= 2; round++ {
		resp, err := tp.Predict(ctx, providers.PredictionRequest{
			System:   bigSystem,
			Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK."}},
		})
		if err != nil {
			t.Fatalf("round %d Predict: %v", round, err)
		}
		if resp.CostInfo == nil || resp.CostInfo.CachedTokens <= 0 {
			t.Fatalf("round %d: expected cachedTokens > 0 on Vertex, got %+v", round, resp.CostInfo)
		}
		t.Logf("Vertex Predict round %d: cachedTokens=%d inputTokens=%d", round, resp.CostInfo.CachedTokens, resp.CostInfo.InputTokens)
	}

	// The tool choice rides in the CachedContent (the request may not carry
	// tool_config alongside cachedContent), so it must still be honored.
	tools, _ := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "get_status", Description: "Get the build status", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
	})
	for _, tc := range []struct {
		choice    string
		wantCalls bool
	}{{"required", true}, {"none", false}} {
		resp, calls, err := tp.PredictWithTools(ctx, providers.PredictionRequest{
			System:   bigSystem,
			Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK."}},
		}, tools, tc.choice)
		if err != nil {
			t.Fatalf("PredictWithTools(%q): %v", tc.choice, err)
		}
		if resp.CostInfo == nil || resp.CostInfo.CachedTokens <= 0 {
			t.Fatalf("choice %q: expected the cache to be referenced on Vertex, got %+v", tc.choice, resp.CostInfo)
		}
		if (len(calls) > 0) != tc.wantCalls {
			t.Fatalf("choice %q: got %d tool calls (content %q), want calls=%v",
				tc.choice, len(calls), resp.Content, tc.wantCalls)
		}
		t.Logf("Vertex choice %q: toolCalls=%d cachedTokens=%d", tc.choice, len(calls), resp.CostInfo.CachedTokens)
	}
}

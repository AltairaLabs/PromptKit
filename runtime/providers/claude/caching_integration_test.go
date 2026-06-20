//go:build integration

package claude

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// bigCacheablePrompt returns a stable system prompt comfortably above Haiku's
// 2048-token prompt-cache minimum so the prefix is eligible for caching.
func bigCacheablePrompt() string {
	return strings.Repeat(
		"You are a meticulous refund-policy assistant. Follow the rules exactly. ", 600)
}

// TestIntegration_PromptCaching_ToolsPath proves prompt caching works end to end
// against the real Anthropic API on the TOOLS path (the path that carried the
// message-level cache_control bug and that the codegen harness uses): a large,
// stable prefix sent twice must produce a cache READ on the second call. Two tiny
// calls — a few cents — and a permanent regression test.
//
// Run: ANTHROPIC_API_KEY=... go test -tags integration ./runtime/providers/claude/ -run PromptCaching -v
func TestIntegration_PromptCaching_ToolsPath(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("no ANTHROPIC_API_KEY, skipping prompt-caching integration test")
	}

	// Caching is enabled by default (DisablePromptCaching false) for a current model.
	provider := NewToolProvider("itest-cache", "claude-haiku-4-5",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 16}, false)

	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "noop",
			Description: "A placeholder tool that does nothing.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}
	tools, err := provider.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("BuildTooling: %v", err)
	}
	req := providers.PredictionRequest{
		System:    bigCacheablePrompt(),
		Messages:  []types.Message{{Role: "user", Content: "Reply with the single word: ok"}},
		MaxTokens: 16,
	}
	ctx := context.Background()

	// Call 1 — primes the cache (cache_creation). Must not 400 on cache_control.
	resp1, _, err := provider.PredictWithTools(ctx, req, tools, "auto")
	if err != nil {
		t.Fatalf("first PredictWithTools failed (a 400 here means cache_control is misplaced): %v", err)
	}
	if resp1.CostInfo == nil {
		t.Fatal("call 1 returned no CostInfo")
	}
	t.Logf("call 1: input=%d output=%d cached=%d",
		resp1.CostInfo.InputTokens, resp1.CostInfo.OutputTokens, resp1.CostInfo.CachedTokens)

	// Call 2 — identical prefix, back to back, must READ the cache.
	resp2, _, err := provider.PredictWithTools(ctx, req, tools, "auto")
	if err != nil {
		t.Fatalf("second PredictWithTools failed: %v", err)
	}
	if resp2.CostInfo == nil {
		t.Fatal("call 2 returned no CostInfo")
	}
	t.Logf("call 2: input=%d output=%d cached=%d",
		resp2.CostInfo.InputTokens, resp2.CostInfo.OutputTokens, resp2.CostInfo.CachedTokens)

	if resp2.CostInfo.CachedTokens <= 0 {
		t.Fatalf("PROMPT CACHING NOT WORKING: expected a cache read on the second call "+
			"(CostInfo.CachedTokens > 0), got %d. Either the API isn't caching the prefix "+
			"or we aren't reading cache_read_input_tokens back into cost.",
			resp2.CostInfo.CachedTokens)
	}
	if resp2.CostInfo.InputTokens < 0 || resp2.CostInfo.TotalCost < 0 {
		t.Fatalf("negative cost (input=%d total=%.6f): Anthropic input_tokens already excludes "+
			"cache reads — the cost calc must not subtract cachedTokens from it",
			resp2.CostInfo.InputTokens, resp2.CostInfo.TotalCost)
	}
}

// TestIntegration_PromptCaching_StreamingToolsPath proves caching on the
// STREAMING-WITH-TOOLS path — the exact path the codegen agent loop uses. A big
// stable system prompt sent twice (via PredictStreamWithTools) must produce a
// cache READ on the second call. This is the path that regressed to cached=0.
func TestIntegration_PromptCaching_StreamingToolsPath(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("no ANTHROPIC_API_KEY, skipping prompt-caching integration test")
	}

	provider := NewToolProvider("itest-cache-stream-tools", "claude-haiku-4-5",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 16}, false)

	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "noop",
			Description: "A placeholder tool that does nothing.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}
	tools, err := provider.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("BuildTooling: %v", err)
	}
	req := providers.PredictionRequest{
		System:    bigCacheablePrompt(),
		Messages:  []types.Message{{Role: "user", Content: "Reply with the single word: ok"}},
		MaxTokens: 16,
	}
	ctx := context.Background()

	drain := func(label string) *types.CostInfo {
		stream, streamErr := provider.PredictStreamWithTools(ctx, req, tools, "auto")
		if streamErr != nil {
			t.Fatalf("%s: PredictStreamWithTools failed: %v", label, streamErr)
		}
		var cost *types.CostInfo
		for chunk := range stream {
			if chunk.Error != nil {
				t.Fatalf("%s: stream error: %v", label, chunk.Error)
			}
			if chunk.CostInfo != nil {
				cost = chunk.CostInfo
			}
		}
		if cost == nil {
			t.Fatalf("%s: stream produced no CostInfo", label)
		}
		t.Logf("%s: input=%d output=%d cached=%d", label, cost.InputTokens, cost.OutputTokens, cost.CachedTokens)
		return cost
	}

	drain("call 1 (prime)")
	cost2 := drain("call 2 (read)")

	if cost2.CachedTokens <= 0 {
		t.Fatalf("STREAMING-TOOLS PATH NOT CACHING: expected CachedTokens > 0 on the "+
			"second call (this is the path the agent loop uses), got %d", cost2.CachedTokens)
	}
	if cost2.InputTokens < 0 || cost2.TotalCost < 0 {
		t.Fatalf("negative cost (input=%d total=%.6f) on the streaming-tools path",
			cost2.InputTokens, cost2.TotalCost)
	}
}

// TestIntegration_PromptCaching_StreamingPath proves caching is captured on the
// STREAMING path (which the harness uses) — the streaming parser must read
// cache_read_input_tokens from message_start. Same two-call shape.
func TestIntegration_PromptCaching_StreamingPath(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("no ANTHROPIC_API_KEY, skipping prompt-caching integration test")
	}

	provider := NewProvider("itest-cache-stream", "claude-haiku-4-5",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 16}, false)

	req := providers.PredictionRequest{
		System:    bigCacheablePrompt(),
		Messages:  []types.Message{{Role: "user", Content: "Reply with the single word: ok"}},
		MaxTokens: 16,
	}
	ctx := context.Background()

	drain := func(label string) *types.CostInfo {
		stream, err := provider.PredictStream(ctx, req)
		if err != nil {
			t.Fatalf("%s: PredictStream failed: %v", label, err)
		}
		var cost *types.CostInfo
		for chunk := range stream {
			if chunk.Error != nil {
				t.Fatalf("%s: stream error: %v", label, chunk.Error)
			}
			if chunk.CostInfo != nil {
				cost = chunk.CostInfo
			}
		}
		if cost == nil {
			t.Fatalf("%s: stream produced no CostInfo", label)
		}
		t.Logf("%s: input=%d output=%d cached=%d", label, cost.InputTokens, cost.OutputTokens, cost.CachedTokens)
		return cost
	}

	drain("call 1 (prime)")
	cost2 := drain("call 2 (read)")

	if cost2.CachedTokens <= 0 {
		t.Fatalf("PROMPT CACHING NOT WORKING on the streaming path: expected CachedTokens > 0 "+
			"on the second call, got %d", cost2.CachedTokens)
	}
	if cost2.InputTokens < 0 || cost2.TotalCost < 0 {
		t.Fatalf("negative cost (input=%d total=%.6f) on the streaming path",
			cost2.InputTokens, cost2.TotalCost)
	}
}

package vllm

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// newTestVLLMProvider builds a bare Provider for cost/streaming unit tests.
func newTestVLLMProvider(t *testing.T) *Provider {
	t.Helper()
	return NewProvider("test-vllm", "test-model", "http://localhost:8000", providers.ProviderDefaults{}, false, nil)
}

// TestVLLMUsageToTokens_MapsFields verifies the wire-to-canonical mapping:
// vLLM's usage carries no cache breakdown, so PromptTokens/CompletionTokens
// map straight to full-price Input/Output.
func TestVLLMUsageToTokens_MapsFields(t *testing.T) {
	got := vllmUsageToTokens(vllmUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	assert.Equal(t, base.TokenUsage{Input: 100, Output: 50}, got)
}

// TestVLLMCost_CalculateCostWrapperMatchesCostFromUsage verifies the public
// CalculateCost(tokensIn, tokensOut, cachedTokens) signature (kept for the
// Provider interface contract) is a thin wrapper over costFromUsage.
func TestVLLMCost_CalculateCostWrapperMatchesCostFromUsage(t *testing.T) {
	p := newTestVLLMProvider(t)
	want := p.costFromUsage(vllmUsage{PromptTokens: 800, CompletionTokens: 500})
	got := p.CalculateCost(1000, 500, 200)

	assert.Equal(t, want.TotalCost, got.TotalCost)
	assert.Equal(t, want.InputCostUSD, got.InputCostUSD)
	assert.Equal(t, want.OutputCostUSD, got.OutputCostUSD)
	assert.Equal(t, want.InputTokens, got.InputTokens)
	assert.Equal(t, want.OutputTokens, got.OutputTokens)
}

// TestVLLMCost_UnconfiguredIsZeroWithTokenCounts pins finding J's stated
// contract: an unconfigured (self-hosted, no pricing table) vLLM provider
// prices as $0 but still reports token counts, silently (PriceUsage's nil
// descriptor path), matching the pre-existing "free for self-hosted" default.
func TestVLLMCost_UnconfiguredIsZeroWithTokenCounts(t *testing.T) {
	p := newTestVLLMProvider(t)
	ci := p.costFromUsage(vllmUsage{PromptTokens: 1000, CompletionTokens: 500})
	assert.Zero(t, ci.TotalCost)
	assert.Equal(t, 1000, ci.InputTokens)
	assert.Equal(t, 500, ci.OutputTokens)
}

// TestVLLMStreaming_RequestsUsage is the regression test for finding J: a
// streaming request must set stream_options.include_usage so vLLM's terminal
// SSE chunk carries usage (without it, streamed responses never report cost).
func TestVLLMStreaming_RequestsUsage(t *testing.T) {
	p := newTestVLLMProvider(t)
	req := &providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	messages, err := p.prepareMessages(req)
	require.NoError(t, err)

	vllmReq := p.buildRequest(req, messages, 0.7, 0.9, 100, true)

	require.NotNil(t, vllmReq.StreamOptions)
	assert.True(t, vllmReq.StreamOptions.IncludeUsage)
}

// TestVLLMStreaming_NonStreamingRequestOmitsStreamOptions guards against
// stream_options leaking onto non-streaming requests (vLLM only expects it
// alongside stream: true).
func TestVLLMStreaming_NonStreamingRequestOmitsStreamOptions(t *testing.T) {
	p := newTestVLLMProvider(t)
	req := &providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	messages, err := p.prepareMessages(req)
	require.NoError(t, err)

	vllmReq := p.buildRequest(req, messages, 0.7, 0.9, 100, false)

	assert.Nil(t, vllmReq.StreamOptions)
}

// TestVLLMToolStreamRequest_IncludesStreamOptions verifies the map-based
// tool-streaming request builder (buildToolRequest) also carries
// stream_options through, so PredictStreamWithTools' terminal chunk can
// receive usage the same way the non-tool streaming path does.
func TestVLLMToolStreamRequest_IncludesStreamOptions(t *testing.T) {
	p := newTestVLLMProvider(t)
	req := &providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	messages, err := p.prepareMessages(req)
	require.NoError(t, err)

	reqMap := p.buildToolRequest(req, messages, toolRequestParams{
		temperature: 0.7, topP: 0.9, maxTokens: 100, stream: true,
	})

	streamOpts, ok := reqMap["stream_options"].(*vllmStreamOptions)
	require.True(t, ok, "expected stream_options to be present and typed *vllmStreamOptions")
	assert.True(t, streamOpts.IncludeUsage)
}

// streamWithUsageAndStop returns a fake vLLM tool-streaming SSE body whose
// terminal chunk carries usage and vLLM's native "eos_token" finish reason
// (rather than the OpenAI-compat "stop"), exercising both normalization and
// cost propagation on the terminal chunk.
func streamWithUsageAndStop() io.ReadCloser {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"eos_token\"}]," +
		"\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n" +
		"data: [DONE]\n\n"
	return io.NopCloser(strings.NewReader(sse))
}

// collectVLLMToolStreamChunks drains streamToolResponse (the SSE consumer
// behind PredictStreamWithTools) into a slice for assertion.
func collectVLLMToolStreamChunks(t *testing.T, body io.ReadCloser) []providers.StreamChunk {
	t.Helper()
	p := newTestVLLMProvider(t)
	chunks := make(chan providers.StreamChunk, 10)
	go p.streamToolResponse(context.Background(), body, chunks)

	var out []providers.StreamChunk
	for c := range chunks {
		out = append(out, c)
	}
	return out
}

// TestVLLMToolStreaming_TerminalChunkCarriesCostAndNormalizedFinish is the
// regression test for finding J: the tool-streaming terminal chunk must
// carry a priced CostInfo (when usage is present) and a normalized finish
// reason ("stop", not vLLM's raw "eos_token").
func TestVLLMToolStreaming_TerminalChunkCarriesCostAndNormalizedFinish(t *testing.T) {
	chunks := collectVLLMToolStreamChunks(t, streamWithUsageAndStop())
	require.NotEmpty(t, chunks)

	last := chunks[len(chunks)-1]
	require.NotNil(t, last.FinishReason)
	assert.Equal(t, types.FinishReasonStop, *last.FinishReason)
	require.NotNil(t, last.CostInfo)
	assert.Equal(t, 10, last.CostInfo.InputTokens)
	assert.Equal(t, 5, last.CostInfo.OutputTokens)
}

// TestVLLMToolStreaming_NoUsageOmitsCostInfo guards the converse: without a
// usage-bearing terminal chunk (e.g. stream_options wasn't honored by the
// server), CostInfo must stay nil rather than fabricate a $0/0-token result.
func TestVLLMToolStreaming_NoUsageOmitsCostInfo(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	chunks := collectVLLMToolStreamChunks(t, io.NopCloser(strings.NewReader(sse)))
	require.NotEmpty(t, chunks)

	last := chunks[len(chunks)-1]
	require.NotNil(t, last.FinishReason)
	assert.Equal(t, types.FinishReasonStop, *last.FinishReason)
	assert.Nil(t, last.CostInfo)
}

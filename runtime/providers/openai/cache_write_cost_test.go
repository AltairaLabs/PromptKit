package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The usage objects below are copied verbatim from live gpt-6-luna calls made
// on 2026-11-20 with a ~17K-token repeated prefix and a prompt_cache_key: the
// first call writes the prefix to the cache, the repeat reads it. Both APIs
// count the written and the read tokens INSIDE prompt_tokens / input_tokens.
const (
	liveChatWriteUsage = `{"prompt_tokens": 17133, "completion_tokens": 4, "total_tokens": 17137,
		"prompt_tokens_details": {"cached_tokens": 0, "cache_write_tokens": 17130, "audio_tokens": 0},
		"completion_tokens_details": {"reasoning_tokens": 0, "audio_tokens": 0,
			"accepted_prediction_tokens": 0, "rejected_prediction_tokens": 0}}`
	liveResponsesWrite30mUsage = `{"input_tokens": 17133, "input_tokens_details": {"cache_write_tokens": 17130,
		"cache_write_tokens_details": {"ttl_12h_tokens": 0, "ttl_30m_tokens": 17130}, "cached_tokens": 0},
		"output_tokens": 5, "output_tokens_details": {"reasoning_tokens": 0}, "total_tokens": 17138}`
	liveResponsesWrite12hUsage = `{"input_tokens": 17135, "input_tokens_details": {"cache_write_tokens": 17132,
		"cache_write_tokens_details": {"ttl_12h_tokens": 17132, "ttl_30m_tokens": 0}, "cached_tokens": 0},
		"output_tokens": 5, "output_tokens_details": {"reasoning_tokens": 0}, "total_tokens": 17140}`
	liveResponsesReadUsage = `{"input_tokens": 17133, "input_tokens_details": {"cache_write_tokens": 0,
		"cache_write_tokens_details": {"ttl_12h_tokens": 0, "ttl_30m_tokens": 0}, "cached_tokens": 17130},
		"output_tokens": 5, "output_tokens_details": {"reasoning_tokens": 0}, "total_tokens": 17138}`
)

// gpt-6-luna list rates per token: input $0.10/M, cached $0.01/M, 30-minute
// cache write $0.125/M (1.25x input), 12-hour write $0.25/M (2.5x), output $0.50/M.
const (
	lunaIn       = 0.10 / 1e6
	lunaCached   = 0.01 / 1e6
	lunaWrite30m = 0.125 / 1e6
	lunaWrite12h = 0.25 / 1e6
	lunaOut      = 0.50 / 1e6
)

func chatServer(t *testing.T, usage string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"gpt-6-luna",
			"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],
			"usage":`+usage+`}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func responsesServer(t *testing.T, usage string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"r1","object":"response","status":"completed","model":"gpt-6-luna",
			"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],
			"usage":`+usage+`}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func predictCost(t *testing.T, srv *httptest.Server, model, apiMode string) *types.CostInfo {
	t.Helper()
	p := NewProviderWithConfig("cw", model, srv.URL, providers.ProviderDefaults{MaxTokens: 100}, false,
		map[string]any{"api_mode": apiMode})
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.CostInfo)
	return resp.CostInfo
}

// Chat Completions reports cache writes in prompt_tokens_details.cache_write_tokens.
// They are billed at the write rate, not again as ordinary input.
func TestCost_ChatCacheWriteBilledAtWriteRate(t *testing.T) {
	ci := predictCost(t, chatServer(t, liveChatWriteUsage), "gpt-6-luna", APIModeCompletions)

	assert.Equal(t, 17130.0, ci.Quantities[base.UnitCacheWriteToken])
	assert.Equal(t, 3.0, ci.Quantities[base.UnitInputToken])
	assert.InDelta(t, 3*lunaIn+17130*lunaWrite30m, ci.InputCostUSD, 1e-12)
	assert.InDelta(t, 4*lunaOut, ci.OutputCostUSD, 1e-12)
	assert.InDelta(t, 3*lunaIn+17130*lunaWrite30m+4*lunaOut, ci.TotalCost, 1e-12)
}

// The Responses API reports writes in input_tokens_details; a 30-minute write
// bills at 1.25x input.
func TestCost_ResponsesCacheWrite30m(t *testing.T) {
	ci := predictCost(t, responsesServer(t, liveResponsesWrite30mUsage), "gpt-6-luna", APIModeResponses)

	assert.Equal(t, 17130.0, ci.Quantities[base.UnitCacheWriteToken])
	assert.Equal(t, 3.0, ci.Quantities[base.UnitInputToken])
	assert.InDelta(t, 3*lunaIn+17130*lunaWrite30m+5*lunaOut, ci.TotalCost, 1e-12)
}

// A 12-hour write (prompt_cache_options.ttl = "12h") bills at 2.5x input.
func TestCost_ResponsesCacheWrite12h(t *testing.T) {
	ci := predictCost(t, responsesServer(t, liveResponsesWrite12hUsage), "gpt-6-luna", APIModeResponses)

	assert.Equal(t, 17132.0, ci.Quantities[base.UnitCacheWriteToken])
	assert.InDelta(t, 3*lunaIn+17132*lunaWrite12h, ci.InputCostUSD, 1e-12)
	assert.InDelta(t, 3*lunaIn+17132*lunaWrite12h+5*lunaOut, ci.TotalCost, 1e-12)
}

// The Responses API reports cache reads as input_tokens_details.cached_tokens.
// They bill at the cached rate rather than as full-price input.
func TestCost_ResponsesCacheReadBilledAtCachedRate(t *testing.T) {
	ci := predictCost(t, responsesServer(t, liveResponsesReadUsage), "gpt-6-luna", APIModeResponses)

	assert.Equal(t, 17130, ci.CachedTokens)
	assert.Equal(t, 3, ci.InputTokens)
	assert.InDelta(t, 17130*lunaCached, ci.CachedCostUSD, 1e-12)
	assert.InDelta(t, 3*lunaIn+17130*lunaCached+5*lunaOut, ci.TotalCost, 1e-12)
}

// The streaming Responses path reads the same usage off response.completed.
func TestCost_ResponsesStreamCacheWrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":"+
			compactJSON(t, liveResponsesWrite30mUsage)+"}}\n\n")
	}))
	t.Cleanup(srv.Close)
	p := NewProviderWithConfig("cw", "gpt-6-luna", srv.URL, providers.ProviderDefaults{MaxTokens: 100}, false,
		map[string]any{"api_mode": APIModeResponses})
	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	var ci *types.CostInfo
	for chunk := range ch {
		if chunk.CostInfo != nil {
			ci = chunk.CostInfo
		}
	}
	require.NotNil(t, ci)
	assert.InDelta(t, 3*lunaIn+17130*lunaWrite30m+5*lunaOut, ci.TotalCost, 1e-12)
}

// Responses reports reasoning in output_tokens_details; it is split out of the
// visible-output headline and still billed at the output rate.
func TestCost_ResponsesReasoningSplitOut(t *testing.T) {
	usage := `{"input_tokens": 100, "input_tokens_details": {"cached_tokens": 0, "cache_write_tokens": 0},
		"output_tokens": 50, "output_tokens_details": {"reasoning_tokens": 30}, "total_tokens": 150}`
	ci := predictCost(t, responsesServer(t, usage), "gpt-6-luna", APIModeResponses)

	assert.Equal(t, 20, ci.OutputTokens)
	assert.Equal(t, 30.0, ci.Quantities[base.UnitReasoningToken])
	assert.InDelta(t, 100*lunaIn+50*lunaOut, ci.TotalCost, 1e-12)
}

// Models before GPT-5.6 charge nothing extra to write the cache: should one
// report written tokens, they bill exactly as the ordinary input they were.
func TestCost_CacheWriteOnPrePremiumModelBillsAsInput(t *testing.T) {
	withWrite := newTestOpenAIProvider(t, "gpt-5.4-mini").costFromUsage(openAIUsage{
		PromptTokens: 10_000, CompletionTokens: 100,
		PromptTokensDetails: &openAIPromptDetails{CacheWriteTokens: 8_000},
	})
	assert.InDelta(t, 10_000*0.75/1e6+100*4.50/1e6, withWrite.TotalCost, 1e-12)
}

// A usage report whose cached and written counts exceed the prompt total never
// produces negative full-price input.
func TestOpenAIUsageToTokens_InputNeverNegative(t *testing.T) {
	got := openAIUsageToTokens(openAIUsage{
		PromptTokens:        100,
		PromptTokensDetails: &openAIPromptDetails{CachedTokens: 80, CacheWriteTokens: 40},
	})
	assert.Equal(t, base.TokenUsage{Input: 0, CacheRead: 80, CacheWrite: 40}, got)
}

// servedWriteRates is each premium-write model's per-1M 30-minute and 12-hour
// cache-write price as https://developers.openai.com/api/docs/pricing lists
// them (Standard tier, short context, 2026-11-20).
var servedWriteRates = map[string][2]float64{
	"gpt-6.2-astra": {10.00, 20.00},
	"gpt-6.2-sol":   {2.50, 5.00},
	"gpt-6-astra":   {12.50, 25.00},
	"gpt-6.1-sol":   {2.50, 5.00},
	"gpt-6.1-terra": {1.25, 2.50},
	"gpt-6-luna":    {0.125, 0.25},
	"gpt-6-sol":     {2.50, 5.00},
	"gpt-5.6-sol":   {5.00, 10.00},
	"gpt-5.6-terra": {2.50, 5.00},
	"gpt-5.6-luna":  {0.25, 0.50},
}

func TestCost_ServedModelsCacheWriteRates(t *testing.T) {
	for model, r := range servedWriteRates {
		t.Run(model, func(t *testing.T) {
			p := newTestOpenAIProvider(t, model)
			w30 := p.costFromUsage(openAIUsage{
				PromptTokens:        1_000_000,
				PromptTokensDetails: &openAIPromptDetails{CacheWriteTokens: 1_000_000},
			})
			assert.InDelta(t, r[0], w30.TotalCost, 1e-9, "30m write")
			w12 := p.costFromUsage(openAIUsage{
				PromptTokens: 1_000_000,
				PromptTokensDetails: &openAIPromptDetails{
					CacheWriteTokens: 1_000_000, cacheWrite12hTokens: 1_000_000,
				},
			})
			assert.InDelta(t, r[1], w12.TotalCost, 1e-9, "12h write")
		})
	}
}

// compactJSON puts a multi-line JSON literal on one line for an SSE data field.
func compactJSON(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, json.Compact(&b, []byte(s)))
	return b.String()
}

// A provider-config pricing descriptor that does not price cache_write_token
// bills the written tokens at its input rate, which is what such a config
// recorded before cache writes were reported apart from input.
func TestCost_ConfigPricingWithoutCacheWriteBillsWritesAsInput(t *testing.T) {
	srv := chatServer(t, liveChatWriteUsage)
	p := NewProviderWithConfig("cw", "gpt-6-luna", srv.URL, providers.ProviderDefaults{MaxTokens: 100}, false,
		map[string]any{"api_mode": APIModeCompletions})
	p.SetPricing(&base.PricingDescriptor{Items: []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: 1e-6},
		{Unit: base.UnitOutputToken, Rate: 4e-6},
	}})
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.CostInfo)
	// 3 uncached + 17130 written input tokens at $1/M, 4 output at $4/M.
	assert.InDelta(t, 17133*1e-6, resp.CostInfo.InputCostUSD, 1e-12)
	assert.InDelta(t, 17133*1e-6+4*4e-6, resp.CostInfo.TotalCost, 1e-12)
}

// The legacy flat per-1K config bills written tokens at its input rate too.
func TestCost_FlatConfigPricingBillsWritesAsInput(t *testing.T) {
	srv := chatServer(t, liveChatWriteUsage)
	defaults := providers.ProviderDefaults{MaxTokens: 100,
		Pricing: providers.Pricing{InputCostPer1K: 0.001, OutputCostPer1K: 0.004}}
	p := NewProviderWithConfig("cw", "gpt-6-luna", srv.URL, defaults, false,
		map[string]any{"api_mode": APIModeCompletions})
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.CostInfo)
	assert.InDelta(t, 17133*1e-6+4*4e-6, resp.CostInfo.TotalCost, 1e-12)
}

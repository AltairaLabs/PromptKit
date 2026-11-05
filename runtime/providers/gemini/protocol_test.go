package gemini

import (
	"context"
	"math"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func TestBuildCostInfo(t *testing.T) {
	t.Run("nil usage returns nil", func(t *testing.T) {
		s := &StreamSession{pricing: sessionPricing(&StreamSessionConfig{Model: "gemini-3.8-live"})}
		if got := s.buildCostInfo(nil); got != nil {
			t.Errorf("expected nil CostInfo for nil usage, got %+v", got)
		}
	})

	t.Run("tokens populated but zero cost when unpriced", func(t *testing.T) {
		s := &StreamSession{} // no descriptor: the model is in no table and no rate is configured
		got := s.buildCostInfo(&UsageMetadata{PromptTokenCount: 1000, ResponseTokenCount: 500})
		if got == nil {
			t.Fatal("expected non-nil CostInfo")
		}
		if got.InputTokens != 1000 || got.OutputTokens != 500 {
			t.Errorf("tokens = %d/%d, want 1000/500", got.InputTokens, got.OutputTokens)
		}
		if got.TotalCost != 0 {
			t.Errorf("expected zero cost when unpriced, got %+v", got)
		}
	})

	t.Run("flat per-1K config prices every token as text", func(t *testing.T) {
		// 2000 input tokens @ $3/1K = $6.00; 1000 output @ $10/1K = $10.00
		cfg := &StreamSessionConfig{Model: "gemini-3.8-live", InputCostPer1K: 3.0, OutputCostPer1K: 10.0}
		s := &StreamSession{pricing: sessionPricing(cfg)}
		got := s.buildCostInfo(&UsageMetadata{PromptTokenCount: 2000, ResponseTokenCount: 1000})
		assertUSD(t, "InputCostUSD", got.InputCostUSD, 6.0)
		assertUSD(t, "OutputCostUSD", got.OutputCostUSD, 10.0)
		assertUSD(t, "TotalCost", got.TotalCost, 16.0)
	})

	t.Run("flat per-1K config prices audio at the flat rates", func(t *testing.T) {
		cfg := &StreamSessionConfig{Model: "gemini-3.8-live", InputCostPer1K: 3.0, OutputCostPer1K: 10.0}
		s := &StreamSession{pricing: sessionPricing(cfg)}
		got := s.buildCostInfo(&UsageMetadata{
			PromptTokenCount: 2000, ResponseTokenCount: 1000,
			PromptTokensDetails:   []ModalityTokenCount{{Modality: "AUDIO", TokenCount: 2000}},
			ResponseTokensDetails: []ModalityTokenCount{{Modality: "AUDIO", TokenCount: 1000}},
		})
		assertUSD(t, "TotalCost", got.TotalCost, 16.0)
	})

	t.Run("Live model prices audio at its own rate", func(t *testing.T) {
		// gemini-3.8-live per 1M: text in 0.75, audio in 3.00, text out 4.50 (thinking too), audio out 12.00.
		s := &StreamSession{pricing: sessionPricing(&StreamSessionConfig{Model: "gemini-3.8-live"})}
		got := s.buildCostInfo(&UsageMetadata{
			PromptTokenCount:   1000,
			ResponseTokenCount: 500,
			ThoughtsTokenCount: 50,
			PromptTokensDetails: []ModalityTokenCount{
				{Modality: "TEXT", TokenCount: 200}, {Modality: "AUDIO", TokenCount: 800},
			},
			ResponseTokensDetails: []ModalityTokenCount{
				{Modality: "TEXT", TokenCount: 100}, {Modality: "AUDIO", TokenCount: 400},
			},
		})
		assertUSD(t, "InputCostUSD", got.InputCostUSD, (200*0.75+800*3.00)/1e6)
		assertUSD(t, "OutputCostUSD", got.OutputCostUSD, (100*4.50+400*12.00+50*4.50)/1e6)
		// The headline counts cover audio too: a voice turn is not near zero tokens.
		if got.InputTokens != 1000 || got.OutputTokens != 500 {
			t.Errorf("tokens = %d/%d, want 1000/500", got.InputTokens, got.OutputTokens)
		}
	})
}

// assertUSD compares dollar amounts to within float rounding.
func assertUSD(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("%s = %.9f, want %.9f", name, got, want)
	}
}

func TestApplyPricingConfig_UsesTheTable(t *testing.T) {
	cases := []struct {
		model     string
		wantPrice bool
		in, out   float64 // per 1M text tokens
	}{
		// The legacy heuristic billed this as 1.5 Flash ($0.075/$0.30).
		{model: "gemini-2.5-flash", wantPrice: true, in: 0.30, out: 2.50},
		{model: "gemini-3.8-live", wantPrice: true, in: 0.75, out: 4.50},
		// The legacy heuristic billed an unknown model as 1.5 Pro; now it is
		// unpriced, which PriceUsage reports loudly.
		{model: "gemini-unknown-model", wantPrice: false},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			p := NewProvider("gemini-test", tc.model, "https://example.invalid", providers.ProviderDefaults{}, false)
			cfg := &StreamSessionConfig{Model: tc.model}
			p.applyPricingConfig(cfg)
			s := &StreamSession{pricing: sessionPricing(cfg), providerID: sessionProviderID(cfg)}
			got := s.buildCostInfo(&UsageMetadata{PromptTokenCount: 1_000_000, ResponseTokenCount: 1_000_000})
			if !tc.wantPrice {
				if got.TotalCost != 0 {
					t.Errorf("TotalCost = %f, want 0 for an unpriced model", got.TotalCost)
				}
				return
			}
			assertUSD(t, "InputCostUSD", got.InputCostUSD, tc.in)
			assertUSD(t, "OutputCostUSD", got.OutputCostUSD, tc.out)
			if got.ProviderName != "gemini-test" {
				t.Errorf("ProviderName = %q, want gemini-test", got.ProviderName)
			}
		})
	}
}

func TestApplyPricingConfig_FlatDefaultsWin(t *testing.T) {
	defaults := providers.ProviderDefaults{Pricing: providers.Pricing{InputCostPer1K: 1, OutputCostPer1K: 2}}
	p := NewProvider("gemini-test", "gemini-3.8-live", "https://example.invalid", defaults, false)
	cfg := &StreamSessionConfig{Model: "gemini-3.8-live"}
	p.applyPricingConfig(cfg)
	s := &StreamSession{pricing: sessionPricing(cfg)}
	got := s.buildCostInfo(&UsageMetadata{PromptTokenCount: 1000, ResponseTokenCount: 1000})
	assertUSD(t, "TotalCost", got.TotalCost, 3.0)
}

func TestSessionProviderID_DefaultsToGemini(t *testing.T) {
	if got := sessionProviderID(&StreamSessionConfig{}); got != "gemini" {
		t.Errorf("sessionProviderID = %q, want gemini", got)
	}
}

func TestHandleToolCalls(t *testing.T) {
	t.Run("FunctionCall maps to MessageToolCall with marshaled args", func(t *testing.T) {
		s := &StreamSession{
			ctx:    context.Background(),
			emitCh: make(chan providers.StreamChunk, 4),
		}
		msg := &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "call_1", Name: "lookup", Args: map[string]interface{}{"q": "hi"}},
			{ID: "call_2", Name: "noargs"}, // nil Args -> "null"? see below
		}}
		if err := s.handleToolCalls(msg); err != nil {
			t.Fatalf("handleToolCalls: %v", err)
		}

		chunk := <-s.emitCh
		if chunk.FinishReason == nil || *chunk.FinishReason != "tool_calls" {
			t.Errorf("FinishReason = %v, want tool_calls", chunk.FinishReason)
		}
		if len(chunk.ToolCalls) != 2 {
			t.Fatalf("expected 2 tool calls, got %d", len(chunk.ToolCalls))
		}
		if chunk.ToolCalls[0].ID != "call_1" || chunk.ToolCalls[0].Name != "lookup" {
			t.Errorf("call[0] = %+v", chunk.ToolCalls[0])
		}
		if string(chunk.ToolCalls[0].Args) != `{"q":"hi"}` {
			t.Errorf("call[0].Args = %s, want {\"q\":\"hi\"}", chunk.ToolCalls[0].Args)
		}
		// nil map marshals to "null" (json.Marshal(nil map) == "null").
		if string(chunk.ToolCalls[1].Args) != "null" {
			t.Errorf("call[1].Args = %s, want null", chunk.ToolCalls[1].Args)
		}
	})

	t.Run("empty args map marshals to {}", func(t *testing.T) {
		s := &StreamSession{
			ctx:    context.Background(),
			emitCh: make(chan providers.StreamChunk, 1),
		}
		msg := &ToolCallMsg{FunctionCalls: []FunctionCall{
			{ID: "c", Name: "n", Args: map[string]interface{}{}},
		}}
		if err := s.handleToolCalls(msg); err != nil {
			t.Fatalf("handleToolCalls: %v", err)
		}
		chunk := <-s.emitCh
		if string(chunk.ToolCalls[0].Args) != "{}" {
			t.Errorf("Args = %s, want {}", chunk.ToolCalls[0].Args)
		}
	})
}

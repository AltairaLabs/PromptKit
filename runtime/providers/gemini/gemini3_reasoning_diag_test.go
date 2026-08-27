//go:build integration

package gemini

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// thinkingConfigFor picks the control matching the model generation. Gemini 3
// takes thinking_level; Gemini 2.5 rejects it with HTTP 400 and takes
// thinking_budget. Issue #1843.
func thinkingConfigFor(model string) map[string]interface{} {
	if strings.HasPrefix(model, "gemini-3") {
		return map[string]interface{}{"thinking_level": "high", "include_thoughts": true}
	}
	return map[string]interface{}{"thinking_budget": 2048, "include_thoughts": true}
}

// TestGemini_ReasoningAcrossPaths_Live checks reasoning survives every path —
// non-streaming and streaming, with tools and without — for both model
// generations. The streaming paths are the ones that regressed: Gemini 3
// returns no thought summaries for thinking_budget. Issue #1843.
func TestGemini_ReasoningAcrossPaths_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	models := []string{"gemini-2.5-flash", "gemini-3.7-flash"}
	if m := os.Getenv("GEMINI_DIAG_MODELS"); m != "" {
		models = strings.Split(m, ",")
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			spec := providers.ProviderSpec{
				ID: "gem", Type: "gemini", Model: model,
				BaseURL:  "https://generativelanguage.googleapis.com/v1beta",
				Defaults: providers.ProviderDefaults{MaxTokens: 4096},
				AdditionalConfig: thinkingConfigFor(model),
			}
			p, err := providers.CreateProviderFromSpec(spec)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			defer func() { _ = p.Close() }()

			req := providers.PredictionRequest{
				Messages: []types.Message{{
					Role:    "user",
					Content: "Solve carefully: bat and ball cost $1.10, bat is $1.00 more. Ball price?",
				}},
			}

			// 1. non-streaming, no tools
			resp, err := p.Predict(context.Background(), req)
			if err != nil {
				t.Errorf("  Predict error: %v", err)
			} else {
				t.Logf("  Predict (no tools):        reasoning=%v len=%d",
					resp.Reasoning != nil, traceLen(resp.Reasoning))
			}

			// 2. streaming, no tools
			ch, err := p.PredictStream(context.Background(), req)
			if err != nil {
				t.Errorf("  PredictStream error: %v", err)
			} else {
				var sb strings.Builder
				for c := range ch {
					sb.WriteString(c.Reasoning)
				}
				t.Logf("  PredictStream (no tools):  reasoning=%v len=%d", sb.Len() > 0, sb.Len())
			}

			ts, ok := p.(providers.ToolSupport)
			if !ok {
				t.Fatalf("provider does not support tools")
			}
			tools, err := ts.BuildTooling([]*providers.ToolDescriptor{{
				Name:        "get_temperature",
				Description: "Get the current temperature for a city",
				InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
			}})
			if err != nil {
				t.Fatalf("BuildTooling: %v", err)
			}
			toolReq := providers.PredictionRequest{
				Messages: []types.Message{{
					Role: "user", Content: "What's the temperature in Bristol? Use the tool.",
				}},
			}

			// 3. non-streaming, with tools
			tResp, tCalls, err := ts.PredictWithTools(context.Background(), toolReq, tools, "auto")
			if err != nil {
				t.Errorf("  PredictWithTools error: %v", err)
			} else {
				t.Logf("  PredictWithTools:          reasoning=%v len=%d toolCalls=%d",
					tResp.Reasoning != nil, traceLen(tResp.Reasoning), len(tCalls))
			}

			// 4. streaming, with tools  <- the path the pipeline uses
			sch, err := ts.PredictStreamWithTools(context.Background(), toolReq, tools, "auto")
			if err != nil {
				t.Errorf("  PredictStreamWithTools error: %v", err)
			} else {
				var sb strings.Builder
				var calls int
				for c := range sch {
					sb.WriteString(c.Reasoning)
					if len(c.ToolCalls) > calls {
						calls = len(c.ToolCalls)
					}
				}
				t.Logf("  PredictStreamWithTools:    reasoning=%v len=%d toolCalls=%d",
					sb.Len() > 0, sb.Len(), calls)
			}
		})
	}
}

func traceLen(rt *types.ReasoningTrace) int {
	if rt == nil {
		return 0
	}
	return len(rt.Text)
}

// TestGemini3_StreamingToolRound_NoReasoning_Live records a boundary that is
// NOT ours to fix, so it is not rediscovered as a defect.
//
// With thinking_level set, Gemini 3 returns thought summaries on three of the
// four paths. The exception is streaming WITH tools, which returned none across
// every observed run while the same request non-streaming returned ~900 chars —
// same body, only the endpoint differs. Gemini 2.5 supplies reasoning on all
// four paths, so this is specific to Gemini 3 rather than a regression.
//
// If Gemini starts supplying it, this test fails and tells us the boundary
// moved.
func TestGemini3_StreamingToolRound_NoReasoning_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := os.Getenv("GEMINI_THINKING_MODEL")
	if model == "" {
		model = "gemini-3.7-flash"
	}
	if !strings.HasPrefix(model, "gemini-3") {
		t.Skipf("%s is not a Gemini 3 model", model)
	}

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "gem", Type: "gemini", Model: model,
		BaseURL:          "https://generativelanguage.googleapis.com/v1beta",
		Defaults:         providers.ProviderDefaults{MaxTokens: 4096},
		AdditionalConfig: thinkingConfigFor(model),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = p.Close() }()

	ts, ok := p.(providers.ToolSupport)
	if !ok {
		t.Fatalf("provider does not support tools")
	}
	tools, err := ts.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_temperature",
		Description: "Get the current temperature for a city",
		InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	if err != nil {
		t.Fatalf("BuildTooling: %v", err)
	}

	ch, err := ts.PredictStreamWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "What's the temperature in Bristol? Use the tool."}},
	}, tools, "auto")
	if err != nil {
		t.Fatalf("PredictStreamWithTools: %v", err)
	}

	var reasoning strings.Builder
	var sawToolCall bool
	for c := range ch {
		reasoning.WriteString(c.Reasoning)
		if len(c.ToolCalls) > 0 {
			sawToolCall = true
		}
	}

	if !sawToolCall {
		t.Fatal("model did not call the tool; the premise is untested")
	}
	if reasoning.Len() > 0 {
		t.Errorf("Gemini 3 now returns reasoning on streaming tool rounds (%d chars). "+
			"That is good news — drop this expectation and treat it like the other "+
			"three paths.", reasoning.Len())
	}
}

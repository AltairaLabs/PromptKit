package openai

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestExtractResponsesOutput_Reasoning verifies a Responses "reasoning" output
// item's summary is captured onto a ReasoningTrace, separate from the message text.
func TestExtractResponsesOutput_Reasoning(t *testing.T) {
	p := &Provider{}
	content, _, reasoning := p.extractResponsesOutput([]responsesOutput{
		{Type: "reasoning", Summary: []responsesSummaryPart{
			{Type: "summary_text", Text: "weigh the options"},
		}},
		{Type: "message", Content: []responsesContent{
			{Type: "output_text", Text: "the answer"},
		}},
	})

	if content != "the answer" {
		t.Fatalf("content = %q, want %q", content, "the answer")
	}
	if reasoning == nil || reasoning.Text != "weigh the options" {
		t.Fatalf("reasoning = %+v, want text %q", reasoning, "weigh the options")
	}
	if strings.Contains(content, "weigh the options") {
		t.Fatal("reasoning must not be folded into content")
	}
}

// TestExtractResponsesOutput_NoReasoning returns nil reasoning when absent.
func TestExtractResponsesOutput_NoReasoning(t *testing.T) {
	p := &Provider{}
	_, _, reasoning := p.extractResponsesOutput([]responsesOutput{
		{Type: "message", Content: []responsesContent{{Type: "output_text", Text: "hi"}}},
	})
	if reasoning != nil {
		t.Fatalf("reasoning = %+v, want nil", reasoning)
	}
}

// TestHandleReasoningDelta streams a summary delta on StreamChunk.Reasoning.
func TestHandleReasoningDelta(t *testing.T) {
	p := &Provider{}
	ch := make(chan providers.StreamChunk, 1)
	p.handleReasoningDelta(`{"type":"response.reasoning_summary_text.delta","delta":"thinking..."}`, ch)
	select {
	case got := <-ch:
		if got.Reasoning != "thinking..." {
			t.Fatalf("Reasoning = %q, want %q", got.Reasoning, "thinking...")
		}
	default:
		t.Fatal("expected a reasoning chunk")
	}
}

// TestGetReasoningSummary validates the opt-in config parsing.
func TestGetReasoningSummary(t *testing.T) {
	if got := getReasoningSummary(map[string]any{"reasoning_summary": "auto"}); got != "auto" {
		t.Fatalf("auto = %q", got)
	}
	if got := getReasoningSummary(map[string]any{"reasoning_summary": "detailed"}); got != "detailed" {
		t.Fatalf("detailed = %q", got)
	}
	if got := getReasoningSummary(map[string]any{"reasoning_summary": "bogus"}); got != "" {
		t.Fatalf("bogus = %q, want empty", got)
	}
	if got := getReasoningSummary(nil); got != "" {
		t.Fatalf("nil = %q, want empty", got)
	}
}

func TestBuildResponsesRequest_IncludesSummary(t *testing.T) {
	p := NewProviderWithConfig("t", "o4-mini", "", providers.ProviderDefaults{MaxTokens: 100}, false,
		map[string]any{"api_mode": "responses", "reasoning_effort": "medium", "reasoning_summary": "auto"})
	req := p.buildResponsesRequest(
		providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}, nil, "")
	r, ok := req["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning block missing: %v", req["reasoning"])
	}
	if r["effort"] != "medium" || r["summary"] != "auto" {
		t.Fatalf("reasoning = %v, want effort=medium summary=auto", r)
	}
}

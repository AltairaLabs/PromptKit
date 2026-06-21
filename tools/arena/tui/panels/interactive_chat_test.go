package panels

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestInteractiveChatPanel_RendersMessages(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.SetMessages([]types.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	})
	out := p.View()
	if !strings.Contains(out, "hello") || !strings.Contains(out, "hi there") {
		t.Fatalf("transcript missing message text:\n%s", out)
	}
}

func TestInteractiveChatPanel_RendersToolCall(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.SetMessages([]types.Message{
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{Name: "search"}}},
	})
	out := p.View()
	if !strings.Contains(out, "search") {
		t.Fatalf("transcript missing tool call name:\n%s", out)
	}
}

func TestInteractiveChatPanel_ClearInput(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.textarea.SetValue("draft")
	if p.InputValue() != "draft" {
		t.Fatalf("want draft, got %q", p.InputValue())
	}
	p.ClearInput()
	if p.InputValue() != "" {
		t.Fatalf("want empty after clear, got %q", p.InputValue())
	}
}

func TestInteractiveChatPanel_RendersToolResult(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	tr := types.NewTextToolResult("id1", "lookup", "result text")
	p.SetMessages([]types.Message{
		{Role: "tool", ToolResult: &tr},
	})
	out := p.View()
	if !strings.Contains(out, "lookup") {
		t.Fatalf("transcript missing tool result name:\n%s", out)
	}
	if !strings.Contains(out, "result text") {
		t.Fatalf("transcript missing tool result content:\n%s", out)
	}
}

func TestInteractiveChatPanel_RendersValidations(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.SetMessages([]types.Message{
		{
			Role:    "assistant",
			Content: "answer",
			Validations: []types.ValidationResult{
				{ValidatorType: "pii_check", Passed: false},
			},
		},
	})
	out := p.View()
	if !strings.Contains(out, "pii_check") {
		t.Fatalf("transcript missing validation type:\n%s", out)
	}
}

func TestInteractiveChatPanel_BusyShowsSpinner(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.SetBusy(true)
	out := p.View()
	// Busy flag adds a placeholder "assistant: ..." line
	if !strings.Contains(out, "assistant") {
		t.Fatalf("busy state missing assistant indicator:\n%s", out)
	}
}

func TestInteractiveChatPanel_BusyUnblocksInput(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.SetBusy(true)
	p.SetBusy(false)
	// After clearing busy, textarea should be focused; InputValue works.
	p.textarea.SetValue("typed")
	if p.InputValue() != "typed" {
		t.Fatalf("expected typed after un-busy, got %q", p.InputValue())
	}
}

func TestInteractiveChatPanel_FooterShowsCost(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.SetCost(&types.CostInfo{TotalCost: 0.0042})
	out := p.View()
	if !strings.Contains(out, "0.0042") {
		t.Fatalf("footer missing cost value:\n%s", out)
	}
}

func TestInteractiveChatPanel_FooterShowsEvals(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	score := 0.87
	p.SetEvals([]evals.EvalResult{
		{Type: "accuracy", Score: &score},
	})
	out := p.View()
	if !strings.Contains(out, "accuracy") {
		t.Fatalf("footer missing eval type:\n%s", out)
	}
	if !strings.Contains(out, "0.87") {
		t.Fatalf("footer missing eval score:\n%s", out)
	}
}

func TestInteractiveChatPanel_FooterEvalsNilScore(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.SetEvals([]evals.EvalResult{
		{Type: "coverage", Score: nil},
	})
	out := p.View()
	if !strings.Contains(out, "coverage") {
		t.Fatalf("footer missing eval type with nil score:\n%s", out)
	}
}

func TestInteractiveChatPanel_AssistantWithTextAndToolCalls(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.SetMessages([]types.Message{
		{
			Role:      "assistant",
			Content:   "I'll look that up",
			ToolCalls: []types.MessageToolCall{{Name: "fetch"}},
		},
	})
	out := p.View()
	if !strings.Contains(out, "I'll look that up") {
		t.Fatalf("transcript missing assistant text:\n%s", out)
	}
	if !strings.Contains(out, "fetch") {
		t.Fatalf("transcript missing inline tool call name:\n%s", out)
	}
}

func TestInteractiveChatPanel_SetCostNil(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	// Calling with nil should not panic
	p.SetCost(nil)
	out := p.View()
	if !strings.Contains(out, "cost:") {
		t.Fatalf("footer missing cost line:\n%s", out)
	}
}

func TestInteractiveChatPanel_UpdateForwardsMessages(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	// Update with a generic window size message should not panic.
	_ = p.Update(nil)
}

func TestInteractiveChatPanel_UpdateBusy(t *testing.T) {
	p := NewInteractiveChatPanel()
	p.SetDimensions(80, 24)
	p.SetBusy(true)
	// While busy, textarea update is skipped; ensure no panic.
	_ = p.Update(nil)
}

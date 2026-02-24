package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- AgentInvoked ---

func TestAgentInvokedHandler_Type(t *testing.T) {
	h := &AgentInvokedHandler{}
	if h.Type() != "agent_invoked" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestAgentInvokedHandler_Pass(t *testing.T) {
	h := &AgentInvokedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "agent_a"},
			{ToolName: "agent_b"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"agents": []any{"agent_a", "agent_b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestAgentInvokedHandler_Missing(t *testing.T) {
	h := &AgentInvokedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "agent_a"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"agents": []any{"agent_a", "agent_c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing agent")
	}
}

func TestAgentInvokedHandler_NoAgents(t *testing.T) {
	h := &AgentInvokedHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no agents")
	}
}

// --- AgentNotInvoked ---

func TestAgentNotInvokedHandler_Type(t *testing.T) {
	h := &AgentNotInvokedHandler{}
	if h.Type() != "agent_not_invoked" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestAgentNotInvokedHandler_Pass(t *testing.T) {
	h := &AgentNotInvokedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "agent_a"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"agents": []any{"agent_b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestAgentNotInvokedHandler_Fail(t *testing.T) {
	h := &AgentNotInvokedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "agent_a"},
			{ToolName: "forbidden_agent"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"agents": []any{"forbidden_agent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for forbidden agent called")
	}
}

// --- AgentResponseContains ---

func TestAgentResponseContainsHandler_Type(t *testing.T) {
	h := &AgentResponseContainsHandler{}
	if h.Type() != "agent_response_contains" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestAgentResponseContainsHandler_ViaToolCalls(t *testing.T) {
	h := &AgentResponseContainsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "billing_agent", Result: "Your balance is $100"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"agent":    "billing_agent",
		"contains": "balance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestAgentResponseContainsHandler_ViaMessages(t *testing.T) {
	h := &AgentResponseContainsHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{
				Role: "tool",
				ToolResult: &types.MessageToolResult{
					Name:    "billing_agent",
					Content: "Your balance is $100",
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"agent":    "billing_agent",
		"contains": "balance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass via messages: %s", result.Explanation)
	}
}

func TestAgentResponseContainsHandler_NotFound(t *testing.T) {
	h := &AgentResponseContainsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "billing_agent", Result: "some other response"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"agent":    "billing_agent",
		"contains": "balance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail when content not found")
	}
}

func TestAgentResponseContainsHandler_NoAgent(t *testing.T) {
	h := &AgentResponseContainsHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no agent")
	}
}

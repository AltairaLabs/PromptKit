package chaos

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

func TestHook_Name(t *testing.T) {
	h := NewHook()
	if h.Name() != "chaos-injection" {
		t.Errorf("expected name %q, got %q", "chaos-injection", h.Name())
	}
}

func TestHook_BeforeExecution_NoChaosConfig(t *testing.T) {
	h := NewHook()
	d := h.BeforeExecution(context.Background(), hooks.ToolRequest{Name: "search"})
	if !d.Allow {
		t.Error("expected allow when no chaos config")
	}
}

func TestHook_BeforeExecution_NoMatchingTool(t *testing.T) {
	h := NewHook()
	cfg := &config.ChaosConfig{
		ToolFailures: []config.ChaosToolFailure{
			{Tool: "other_tool", Mode: "error", Probability: 1.0},
		},
	}
	ctx := WithConfig(context.Background(), cfg)
	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "search"})
	if !d.Allow {
		t.Error("expected allow when tool doesn't match")
	}
}

func TestHook_BeforeExecution_ErrorMode(t *testing.T) {
	h := NewHook()
	cfg := &config.ChaosConfig{
		ToolFailures: []config.ChaosToolFailure{
			{Tool: "search", Mode: "error", Probability: 1.0},
		},
	}
	ctx := WithConfig(context.Background(), cfg)
	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "search"})
	if d.Allow {
		t.Error("expected deny for error mode")
	}
	if d.Metadata["chaos_injected"] != true {
		t.Error("expected chaos_injected metadata")
	}
	if d.Metadata["chaos_mode"] != "error" {
		t.Errorf("expected chaos_mode=error, got %v", d.Metadata["chaos_mode"])
	}
}

func TestHook_BeforeExecution_ErrorMode_CustomMessage(t *testing.T) {
	h := NewHook()
	cfg := &config.ChaosConfig{
		ToolFailures: []config.ChaosToolFailure{
			{Tool: "search", Mode: "error", Probability: 1.0, Message: "custom error"},
		},
	}
	ctx := WithConfig(context.Background(), cfg)
	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "search"})
	if d.Allow {
		t.Error("expected deny")
	}
	if d.Reason != "custom error" {
		t.Errorf("expected custom message, got %q", d.Reason)
	}
}

func TestHook_BeforeExecution_TimeoutMode(t *testing.T) {
	h := NewHook()
	cfg := &config.ChaosConfig{
		ToolFailures: []config.ChaosToolFailure{
			{Tool: "lookup", Mode: "timeout", Probability: 1.0},
		},
	}
	ctx := WithConfig(context.Background(), cfg)
	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "lookup"})
	if d.Allow {
		t.Error("expected deny for timeout mode")
	}
	if d.Metadata["chaos_timeout"] != true {
		t.Error("expected chaos_timeout metadata")
	}
}

func TestHook_BeforeExecution_SlowMode(t *testing.T) {
	h := NewHook()
	cfg := &config.ChaosConfig{
		ToolFailures: []config.ChaosToolFailure{
			{Tool: "search", Mode: "slow", Probability: 1.0},
		},
	}
	ctx := WithConfig(context.Background(), cfg)
	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "search"})
	if !d.Allow {
		t.Error("expected allow for slow mode (adds latency but doesn't block)")
	}
	if d.Metadata["chaos_delay_ms"] != 2000 {
		t.Errorf("expected delay metadata, got %v", d.Metadata["chaos_delay_ms"])
	}
}

func TestHook_BeforeExecution_UnknownMode(t *testing.T) {
	h := NewHook()
	cfg := &config.ChaosConfig{
		ToolFailures: []config.ChaosToolFailure{
			{Tool: "search", Mode: "unknown_mode", Probability: 1.0},
		},
	}
	ctx := WithConfig(context.Background(), cfg)
	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "search"})
	if d.Allow {
		t.Error("expected deny for unknown mode")
	}
}

func TestHook_BeforeExecution_ZeroProbability(t *testing.T) {
	h := NewHook()
	cfg := &config.ChaosConfig{
		ToolFailures: []config.ChaosToolFailure{
			{Tool: "search", Mode: "error", Probability: 0},
		},
	}
	ctx := WithConfig(context.Background(), cfg)
	// Probability 0 means "unset" → always fires
	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "search"})
	if d.Allow {
		t.Error("expected deny when probability is 0 (unset = always fire)")
	}
}

func TestHook_AfterExecution_AlwaysAllows(t *testing.T) {
	h := NewHook()
	d := h.AfterExecution(context.Background(), hooks.ToolRequest{}, hooks.ToolResponse{})
	if !d.Allow {
		t.Error("expected allow from AfterExecution")
	}
}

func TestShouldFire(t *testing.T) {
	t.Run("zero means always", func(t *testing.T) {
		if !shouldFire(0) {
			t.Error("expected true for probability 0 (unset)")
		}
	})
	t.Run("negative means always", func(t *testing.T) {
		if !shouldFire(-1) {
			t.Error("expected true for negative probability")
		}
	})
	t.Run("one means always", func(t *testing.T) {
		if !shouldFire(1.0) {
			t.Error("expected true for probability 1.0")
		}
	})
	t.Run("above one means always", func(t *testing.T) {
		if !shouldFire(2.0) {
			t.Error("expected true for probability > 1.0")
		}
	})
}

func TestContextRoundTrip(t *testing.T) {
	cfg := &config.ChaosConfig{
		ToolFailures: []config.ChaosToolFailure{
			{Tool: "test", Mode: "error"},
		},
	}
	ctx := WithConfig(context.Background(), cfg)
	got := ConfigFromContext(ctx)
	if got == nil {
		t.Fatal("expected config from context")
	}
	if len(got.ToolFailures) != 1 {
		t.Fatalf("expected 1 tool failure, got %d", len(got.ToolFailures))
	}
	if got.ToolFailures[0].Tool != "test" {
		t.Errorf("expected tool=test, got %q", got.ToolFailures[0].Tool)
	}
}

func TestConfigFromContext_Nil(t *testing.T) {
	got := ConfigFromContext(context.Background())
	if got != nil {
		t.Error("expected nil from empty context")
	}
}

func TestHook_MultipleRules_FirstMatch(t *testing.T) {
	h := NewHook()
	cfg := &config.ChaosConfig{
		ToolFailures: []config.ChaosToolFailure{
			{Tool: "search", Mode: "error", Probability: 1.0, Message: "first"},
			{Tool: "search", Mode: "timeout", Probability: 1.0, Message: "second"},
		},
	}
	ctx := WithConfig(context.Background(), cfg)
	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "search"})
	if d.Allow {
		t.Error("expected deny")
	}
	// First matching rule should win
	if d.Reason != "first" {
		t.Errorf("expected first rule to match, got %q", d.Reason)
	}
}

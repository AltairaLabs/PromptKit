package consent

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func init() {
	if testing.Testing() {
		config.SchemaValidationEnabled = false
	}
}

func TestSimulationHook_Name(t *testing.T) {
	h := NewSimulationHook()
	if h.Name() != "consent-simulation" {
		t.Errorf("expected name 'consent-simulation', got %q", h.Name())
	}
}

func TestSimulationHook_Grant(t *testing.T) {
	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"get_location": "grant",
	})

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if !d.Allow {
		t.Errorf("expected Allow=true for grant override, got Allow=false, reason=%q", d.Reason)
	}
}

func TestSimulationHook_GrantMetadata(t *testing.T) {
	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"get_location": "grant",
	})

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if !d.Allow {
		t.Fatal("expected Allow=true for grant override")
	}
	if d.Metadata == nil {
		t.Fatal("expected non-nil Metadata for grant decision")
	}
	if status, ok := d.Metadata["consent_status"].(string); !ok || status != "granted" {
		t.Errorf("expected consent_status='granted', got %v", d.Metadata["consent_status"])
	}
}

func TestSimulationHook_DenyMetadata(t *testing.T) {
	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"get_location": "deny",
	})

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if d.Allow {
		t.Fatal("expected Allow=false for deny override")
	}
	if d.Metadata == nil {
		t.Fatal("expected non-nil Metadata for deny decision")
	}
	if status, ok := d.Metadata["consent_status"].(string); !ok || status != "denied" {
		t.Errorf("expected consent_status='denied', got %v", d.Metadata["consent_status"])
	}
}

func TestSimulationHook_TimeoutMetadata(t *testing.T) {
	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"get_location": "timeout",
	})

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if d.Allow {
		t.Fatal("expected Allow=false for timeout override")
	}
	if d.Metadata == nil {
		t.Fatal("expected non-nil Metadata for timeout decision")
	}
	if status, ok := d.Metadata["consent_status"].(string); !ok || status != "timeout" {
		t.Errorf("expected consent_status='timeout', got %v", d.Metadata["consent_status"])
	}
}

func TestSimulationHook_Deny_ErrorStrategy(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(&tools.ToolDescriptor{
		Name: "get_location",
		Mode: "client",
		ClientConfig: &tools.ClientConfig{
			Consent: &tools.ConsentConfig{
				Required:        true,
				DeclineStrategy: tools.DeclineStrategyError,
			},
		},
	})

	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"get_location": "deny",
	})
	ctx = WithToolRegistry(ctx, registry)

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if d.Allow {
		t.Fatal("expected Allow=false for deny override")
	}
	expected := "Tool get_location denied: user declined consent"
	if d.Reason != expected {
		t.Errorf("expected reason %q, got %q", expected, d.Reason)
	}
}

func TestSimulationHook_Deny_FallbackStrategy(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(&tools.ToolDescriptor{
		Name: "get_location",
		Mode: "client",
		ClientConfig: &tools.ClientConfig{
			Consent: &tools.ConsentConfig{
				Required:        true,
				DeclineStrategy: tools.DeclineStrategyFallback,
			},
		},
	})

	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"get_location": "deny",
	})
	ctx = WithToolRegistry(ctx, registry)

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if d.Allow {
		t.Fatal("expected Allow=false for deny override")
	}
	expected := "Tool get_location unavailable: user declined consent. Use alternative approach."
	if d.Reason != expected {
		t.Errorf("expected reason %q, got %q", expected, d.Reason)
	}
}

func TestSimulationHook_Deny_RetryStrategy(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(&tools.ToolDescriptor{
		Name: "get_location",
		Mode: "client",
		ClientConfig: &tools.ClientConfig{
			Consent: &tools.ConsentConfig{
				Required:        true,
				DeclineStrategy: tools.DeclineStrategyRetry,
			},
		},
	})

	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"get_location": "deny",
	})
	ctx = WithToolRegistry(ctx, registry)

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if d.Allow {
		t.Fatal("expected Allow=false for deny override")
	}
	expected := "Tool get_location denied: user declined consent. Try a different approach."
	if d.Reason != expected {
		t.Errorf("expected reason %q, got %q", expected, d.Reason)
	}
}

func TestSimulationHook_Deny_DefaultStrategy(t *testing.T) {
	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"get_location": "deny",
	})
	// No registry — should fall back to default (error) message

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if d.Allow {
		t.Fatal("expected Allow=false for deny override")
	}
	expected := "Tool get_location denied: user declined consent"
	if d.Reason != expected {
		t.Errorf("expected reason %q, got %q", expected, d.Reason)
	}
}

func TestSimulationHook_Timeout(t *testing.T) {
	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"get_location": "timeout",
	})

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if d.Allow {
		t.Fatal("expected Allow=false for timeout override")
	}
	expected := "Tool get_location timed out waiting for user consent"
	if d.Reason != expected {
		t.Errorf("expected reason %q, got %q", expected, d.Reason)
	}
}

func TestSimulationHook_NoOverride(t *testing.T) {
	h := NewSimulationHook()
	ctx := WithConsentOverrides(context.Background(), map[string]string{
		"other_tool": "deny",
	})

	d := h.BeforeExecution(ctx, hooks.ToolRequest{Name: "get_location"})
	if !d.Allow {
		t.Errorf("expected Allow=true for tool not in overrides, got Allow=false")
	}
}

func TestSimulationHook_NoContext(t *testing.T) {
	h := NewSimulationHook()
	d := h.BeforeExecution(context.Background(), hooks.ToolRequest{Name: "get_location"})
	if !d.Allow {
		t.Errorf("expected Allow=true with no overrides in context, got Allow=false")
	}
}

func TestSimulationHook_AfterExecution(t *testing.T) {
	h := NewSimulationHook()
	d := h.AfterExecution(context.Background(), hooks.ToolRequest{Name: "get_location"}, hooks.ToolResponse{})
	if !d.Allow {
		t.Errorf("expected AfterExecution to always return Allow=true")
	}
}

func TestContextRoundTrip_ConsentOverrides(t *testing.T) {
	overrides := map[string]string{
		"tool_a": "grant",
		"tool_b": "deny",
	}
	ctx := WithConsentOverrides(context.Background(), overrides)
	got := OverridesFromContext(ctx)

	if len(got) != len(overrides) {
		t.Fatalf("expected %d overrides, got %d", len(overrides), len(got))
	}
	for k, v := range overrides {
		if got[k] != v {
			t.Errorf("expected override[%q]=%q, got %q", k, v, got[k])
		}
	}
}

func TestContextRoundTrip_ToolRegistry(t *testing.T) {
	registry := tools.NewRegistry()
	ctx := WithToolRegistry(context.Background(), registry)
	got := ToolRegistryFromContext(ctx)
	if got != registry {
		t.Error("expected same registry instance from context")
	}
}

func TestOverridesFromContext_Empty(t *testing.T) {
	got := OverridesFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil from empty context, got %v", got)
	}
}

func TestToolRegistryFromContext_Empty(t *testing.T) {
	got := ToolRegistryFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil from empty context, got %v", got)
	}
}

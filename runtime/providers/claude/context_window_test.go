package claude

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func newContextWindowProvider(t *testing.T, model string, extra map[string]any) providers.Provider {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "ctx", Type: "claude", Model: model, BaseURL: "https://example.invalid", AdditionalConfig: extra,
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	return p
}

func contextWindowOf(t *testing.T, model string, extra map[string]any) int {
	t.Helper()
	cwp, ok := newContextWindowProvider(t, model, extra).(providers.ContextWindowProvider)
	if !ok {
		t.Fatal("provider does not implement providers.ContextWindowProvider")
	}
	return cwp.MaxContextTokens()
}

func TestMaxContextTokens_KnownModel(t *testing.T) {
	if got := contextWindowOf(t, "claude-sonnet-5-5", nil); got != 1000000 {
		t.Errorf("claude-sonnet-5-5 window = %d, want 1000000", got)
	}
}

func TestMaxContextTokens_VendorPrefixResolves(t *testing.T) {
	if got := contextWindowOf(t, "vendor/claude-sonnet-5-5", nil); got != 1000000 {
		t.Errorf("vendor-qualified window = %d, want 1000000", got)
	}
}

func TestMaxContextTokens_UnknownModelReportsNone(t *testing.T) {
	if got := contextWindowOf(t, "no-such-model", nil); got != 0 {
		t.Errorf("unknown model window = %d, want 0 (caller default)", got)
	}
}

func TestMaxContextTokens_SpecOverrideWins(t *testing.T) {
	for name, v := range map[string]any{"int": 4096, "float64": float64(4096)} {
		if got := contextWindowOf(t, "claude-sonnet-5-5", map[string]any{"context_window": v}); got != 4096 {
			t.Errorf("%s override: window = %d, want 4096", name, got)
		}
	}
	if got := contextWindowOf(t, "no-such-model", map[string]any{"context_window": 4096}); got != 4096 {
		t.Errorf("override on unknown model = %d, want 4096", got)
	}
}

func TestContextWindowTable_OnlyPricedModels(t *testing.T) {
	for id, n := range claudeContextWindows {
		if n <= 0 {
			t.Errorf("%q has non-positive window %d", id, n)
		}
		if _, ok := claudePricingTable[id]; !ok {
			t.Errorf("%q is in the context-window table but not the pricing table", id)
		}
	}
}

// The compactor must size itself from the real provider, not fall back to the
// 128K default (the bug this table fixes).
func TestMaxContextTokens_FeedsCompactorBudget(t *testing.T) {
	if got := stage.BudgetTokensFor(newContextWindowProvider(t, "claude-sonnet-5-5", nil)); got != 1_000_000 {
		t.Errorf("BudgetTokensFor = %d, want 1000000", got)
	}
	if got := stage.BudgetTokensFor(newContextWindowProvider(t, "no-such-model", nil)); got != stage.DefaultBudgetTokens {
		t.Errorf("unknown model BudgetTokensFor = %d, want default %d", got, stage.DefaultBudgetTokens)
	}
}

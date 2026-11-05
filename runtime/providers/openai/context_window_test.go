package openai

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func newContextWindowProvider(t *testing.T, model string, extra map[string]any) providers.Provider {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "ctx", Type: "openai", Model: model, BaseURL: "https://example.invalid", AdditionalConfig: extra,
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
	if got := contextWindowOf(t, "gpt-4o", nil); got != 128000 {
		t.Errorf("gpt-4o window = %d, want 128000", got)
	}
}

func TestMaxContextTokens_VendorPrefixResolves(t *testing.T) {
	if got := contextWindowOf(t, "vendor/gpt-4o", nil); got != 128000 {
		t.Errorf("vendor-qualified window = %d, want 128000", got)
	}
}

func TestMaxContextTokens_UnknownModelReportsNone(t *testing.T) {
	if got := contextWindowOf(t, "no-such-model", nil); got != 0 {
		t.Errorf("unknown model window = %d, want 0 (caller default)", got)
	}
}

func TestMaxContextTokens_SpecOverrideWins(t *testing.T) {
	for name, v := range map[string]any{"int": 4096, "float64": float64(4096)} {
		if got := contextWindowOf(t, "gpt-4o", map[string]any{"context_window": v}); got != 4096 {
			t.Errorf("%s override: window = %d, want 4096", name, got)
		}
	}
	if got := contextWindowOf(t, "no-such-model", map[string]any{"context_window": 4096}); got != 4096 {
		t.Errorf("override on unknown model = %d, want 4096", got)
	}
}

func TestContextWindowTable_OnlyPricedModels(t *testing.T) {
	for id, n := range openaiContextWindows {
		if n <= 0 {
			t.Errorf("%q has non-positive window %d", id, n)
		}
		if _, ok := openaiPricingTable[id]; !ok {
			t.Errorf("%q is in the context-window table but not the pricing table", id)
		}
	}
}

// The compactor budgets input, so GPT-5 records its stated 272K max input, not
// the 400K total window.
func TestMaxContextTokens_GPT5RecordsMaxInput(t *testing.T) {
	for _, m := range []string{"gpt-5", "gpt-5-mini", "gpt-5-nano"} {
		if got := contextWindowOf(t, m, nil); got != 272_000 {
			t.Errorf("%s = %d, want 272000", m, got)
		}
	}
}

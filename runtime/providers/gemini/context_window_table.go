package gemini

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// Input token limits, as the model pages publish them.
const (
	contextWindow64K  = 65_536
	contextWindow128K = 131_072
	contextWindow1M   = 1_048_576
)

// geminiContextWindows maps a model id to its input token limit, for the same
// keys geminiPricingTable carries. Captured 2026-11-05 from the per-model pages
// at https://aistudio.google.com/docs/models/<model-id> ("input limit"), so the
// recorded figure is the stated maximum input tokens. It is a fallback:
// additional_config.context_window overrides it. Keyed and resolved
// like the pricing table (vendor prefix stripped, no substring fallback).
//
// Models whose page is gone or that no page states a limit for are left out
// rather than guessed: the retired 1.5 rows and the 2.0-flash-001 / -exp
// variants.
//
//nolint:goconst // keys deliberately mirror the pricing table's, ids spelled once per table
var geminiContextWindows = map[string]int{
	// Gemini 2.0 (deprecated)
	"gemini-2.0-flash":      contextWindow1M,
	"gemini-2.0-flash-lite": contextWindow1M,

	// Gemini 2.5
	idGemini25Pro:           contextWindow1M,
	idGemini25Flash:         contextWindow1M,
	"gemini-2.5-flash-lite": contextWindow1M,

	// Gemini 3.x
	"gemini-3.8-flash":       contextWindow1M,
	"gemini-3.7-flash":       contextWindow1M,
	"gemini-3.6-flash":       contextWindow1M,
	"gemini-3.5-flash":       contextWindow1M,
	"gemini-3-flash-preview": contextWindow1M,
	"gemini-3.5-flash-lite":  contextWindow1M,
	"gemini-3.1-flash-lite":  contextWindow1M,
	"gemini-3.1-pro-preview": contextWindow1M,
	"gemini-3.1-pro":         contextWindow1M,

	// Live API (bidirectional streaming) models.
	"gemini-3.8-live":                               contextWindow128K,
	"gemini-3.8-live-extended-thinking":             contextWindow128K,
	"gemini-3.1-flash-live-preview":                 contextWindow128K,
	"gemini-2.5-flash-native-audio-preview-12-2025": contextWindow128K,

	// Image-generation models.
	"gemini-nano-banana-2.1":      contextWindow128K,
	"gemini-3.1-flash-image":      contextWindow128K,
	"gemini-3.1-flash-lite-image": contextWindow64K,
	"gemini-3-pro-image":          contextWindow64K,
	"gemini-2.5-flash-image":      contextWindow64K,
}

// applyContextWindowConfig reads additional_config.context_window.
func applyContextWindowConfig(p *Provider, spec providers.ProviderSpec) {
	p.contextWindow = base.ContextWindowFromConfig(spec.AdditionalConfig)
}

// MaxContextTokens returns the model's context window in tokens: the
// additional_config.context_window override, else the built-in table, else 0
// (unknown), which makes the compactor use its default budget.
func (p *Provider) MaxContextTokens() int {
	return base.ResolveContextWindow(p.ID(), p.model, p.contextWindow, geminiContextWindows)
}

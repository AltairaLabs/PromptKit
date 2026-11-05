package claude

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// Context windows, in tokens, as the per-model pages publish them.
const (
	contextWindow200K = 200_000
	contextWindow1M   = 1_000_000
)

// claudeContextWindows maps a model id to its context window in tokens, for the
// same keys claudePricingTable carries. Captured 2026-11-05 from
// https://platform.claude.com/docs/en/about-claude/models/overview and the
// per-model pages under https://platform.claude.com/docs/en/models/<model>/overview.
// The pages state a context window and a max output but no separate input
// limit, so the total context window is recorded. Mythos 5 and 5.1 are
// stated there to share Fable 5 and 5.1 specifications.
// It is a fallback: additional_config.context_window overrides it. Keyed and
// resolved like the pricing table (vendor prefix stripped, no substring
// fallback).
//
// Models whose pages are gone are left out rather than guessed: the retired
// Claude 3 / 3.5 / 3.7, Sonnet 4, and Opus 4 / 4.1 rows. Claude Sonnet 4.5 is
// deprecated but its page is still published.
//
//nolint:goconst // keys deliberately mirror the pricing table's, ids spelled once per table
var claudeContextWindows = map[string]int{
	// Claude 4.5 (200K)
	"claude-sonnet-4-5-20250929": contextWindow200K,
	idClaudeSonnet45:             contextWindow200K,
	"claude-haiku-4-5-20251001":  contextWindow200K,
	idClaudeHaiku45:              contextWindow200K,
	"claude-opus-4-5-20251101":   contextWindow200K,
	modelOpus45:                  contextWindow200K,

	// Claude 4.6 - 4.8 (1M)
	"claude-sonnet-4-6": contextWindow1M,
	"claude-opus-4-6":   contextWindow1M,
	"claude-opus-4-7":   contextWindow1M,
	"claude-opus-4-8":   contextWindow1M,

	// Claude 5 (1M)
	"claude-opus-5":     contextWindow1M,
	"claude-opus-5-5":   contextWindow1M,
	"claude-sonnet-5":   contextWindow1M,
	"claude-sonnet-5-5": contextWindow1M,
	"claude-haiku-5-5":  contextWindow1M,
	"claude-fable-5":    contextWindow1M,
	"claude-mythos-5":   contextWindow1M,
	"claude-fable-5-1":  contextWindow1M,
	"claude-mythos-5-1": contextWindow1M,
}

// applyContextWindowConfig reads additional_config.context_window.
func applyContextWindowConfig(p *Provider, spec providers.ProviderSpec) {
	p.contextWindow = base.ContextWindowFromConfig(spec.AdditionalConfig)
}

// MaxContextTokens returns the model's context window in tokens: the
// additional_config.context_window override, else the built-in table, else 0
// (unknown), which makes the compactor use its default budget.
func (p *Provider) MaxContextTokens() int {
	return base.ResolveContextWindow(p.ID(), p.model, p.contextWindow, claudeContextWindows)
}

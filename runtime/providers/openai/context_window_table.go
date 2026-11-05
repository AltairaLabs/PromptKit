package openai

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"

// Token limits as the per-model pages publish them. contextWindowNN is a total
// window, used where the page states no separate input limit; maxInput272K is
// the stated maximum input of the GPT-5 family (its window is 400K).
const (
	contextWindow8K     = 8_192
	contextWindow16K    = 16_385
	contextWindow128K   = 128_000
	contextWindow200K   = 200_000
	maxInput272K        = 272_000
	contextWindow1M4pt1 = 1_047_576
)

// openaiContextWindows maps a model id to its context window in tokens, for the
// same keys openaiPricingTable carries. Captured 2026-11-05 from the per-model
// pages at https://developers.openai.com/api/docs/models/<model-id> (the
// "context window" and, where stated, max input figures). It is a fallback: additional_config.context_window
// overrides it. Keyed and resolved like the pricing table (vendor prefix
// stripped, no substring fallback).
//
// The compactor budgets the input side, so the table records the maximum input
// tokens where the page states one separately (GPT-5, mini and nano: 272K of a
// 400K window) and the total context window otherwise (re-checked 2026-11-05:
// no other page here states a separate input limit; each states a window and a
// max output). A model is left out when its page could not be fetched.
//
//nolint:goconst // keys deliberately mirror the pricing table's, ids spelled once per table
var openaiContextWindows = map[string]int{
	// Legacy
	"gpt-4":         contextWindow8K,
	"gpt-4-turbo":   contextWindow128K,
	"gpt-3.5-turbo": contextWindow16K,

	// GPT-4o
	"gpt-4o":      contextWindow128K,
	"gpt-4o-mini": contextWindow128K,

	// GPT-4.1
	"gpt-4.1":      contextWindow1M4pt1,
	"gpt-4.1-mini": contextWindow1M4pt1,
	"gpt-4.1-nano": contextWindow1M4pt1,

	// o-series
	"o1":      contextWindow200K,
	"o1-mini": contextWindow128K,
	"o3":      contextWindow200K,
	"o3-mini": contextWindow200K,
	"o4-mini": contextWindow200K,

	// GPT-5
	modelGPT5:    maxInput272K,
	"gpt-5-mini": maxInput272K,
	"gpt-5-nano": maxInput272K,
}

// MaxContextTokens returns the model's context window in tokens: the
// additional_config.context_window override, else the built-in table, else 0
// (unknown), which makes the compactor use its default budget.
func (p *Provider) MaxContextTokens() int {
	return base.ResolveContextWindow(p.ID(), p.model,
		base.ContextWindowFromConfig(p.additionalConfig), openaiContextWindows)
}

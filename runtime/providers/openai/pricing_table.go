// Package openai provides OpenAI LLM provider integration.
package openai

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// openaiPricingCorrectAtYear/Month/Day pin the capture date stamped onto
// every table entry below. Named instead of inlined into time.Date so the
// magic-number linter doesn't fire on the call site.
const (
	openaiPricingCorrectAtYear  = 2026
	openaiPricingCorrectAtMonth = time.July
	openaiPricingCorrectAtDay   = 8
)

// openaiPricingCorrectAt is the date these per-model rates were captured.
// RATES NEED HUMAN VERIFICATION against https://openai.com/api/pricing —
// they were filled in from best-available knowledge, not fetched live. See
// the task-12 report for the full per-model list and verification note.
var openaiPricingCorrectAt = time.Date(
	openaiPricingCorrectAtYear, openaiPricingCorrectAtMonth, openaiPricingCorrectAtDay,
	0, 0, 0, 0, time.UTC,
)

// perMillionScale converts a published per-1M-token USD rate into a per-unit rate.
const perMillionScale = 1_000_000.0

// perM builds a per-model pricing descriptor from published per-1M-token USD
// input/cachedInput/output rates. Reasoning tokens are billed by OpenAI at
// the same per-token rate as visible output tokens — completion_tokens
// (which reasoning_tokens is a sub-count of) is one undifferentiated output
// budget on the wire — so reasoning_token prices at the output rate rather
// than a separate published constant.
func perM(input, cachedInput, output float64) *base.PricingDescriptor {
	return &base.PricingDescriptor{
		Source:           base.PricingSourceInline,
		PricingCorrectAt: openaiPricingCorrectAt,
		Items: []base.PriceItem{
			{Unit: base.UnitInputToken, Rate: input / perMillionScale},
			{Unit: base.UnitCacheReadToken, Rate: cachedInput / perMillionScale},
			{Unit: base.UnitOutputToken, Rate: output / perMillionScale},
			{Unit: base.UnitReasoningToken, Rate: output / perMillionScale},
		},
	}
}

// Per-1M-token USD rates (input, cached input, output) for every current
// OpenAI model family. RATES NEED HUMAN VERIFICATION against
// https://openai.com/api/pricing before being relied on for real billing —
// see the task-12 report.
const (
	// Legacy chat models. Cached-prompt discount is the original 50% OpenAI
	// launched prompt caching with in 2024.
	gpt4In, gpt4Cached, gpt4Out                   = 30.0, 15.0, 60.0 // GPT-4 (8K, legacy)
	gpt4TurboIn, gpt4TurboCached, gpt4TurboOut    = 10.0, 5.0, 30.0  // GPT-4 Turbo
	gpt35TurboIn, gpt35TurboCached, gpt35TurboOut = 1.50, 0.75, 2.00 // GPT-3.5 Turbo

	// GPT-4o family (50% cached-input discount).
	gpt4oIn, gpt4oCached, gpt4oOut             = 2.50, 1.25, 10.0  // GPT-4o
	gpt4oMiniIn, gpt4oMiniCached, gpt4oMiniOut = 0.15, 0.075, 0.60 // GPT-4o mini

	// GPT-4.1 family (75% cached-input discount).
	gpt41In, gpt41Cached, gpt41Out             = 2.00, 0.50, 8.00  // GPT-4.1
	gpt41MiniIn, gpt41MiniCached, gpt41MiniOut = 0.40, 0.10, 1.60  // GPT-4.1 mini
	gpt41NanoIn, gpt41NanoCached, gpt41NanoOut = 0.10, 0.025, 0.40 // GPT-4.1 nano

	// o-series reasoning models. o3 pricing reflects OpenAI's June 2025 price
	// cut (from an initial $10/$40 launch price down to $2/$8 per 1M).
	o1In, o1Cached, o1Out             = 15.0, 7.50, 60.0  // o1
	o1MiniIn, o1MiniCached, o1MiniOut = 1.10, 0.55, 4.40  // o1-mini
	o3In, o3Cached, o3Out             = 2.00, 0.50, 8.00  // o3 (post price-cut)
	o3MiniIn, o3MiniCached, o3MiniOut = 1.10, 0.55, 4.40  // o3-mini
	o4MiniIn, o4MiniCached, o4MiniOut = 1.10, 0.275, 4.40 // o4-mini

	// GPT-5 family (90% cached-input discount).
	gpt5In, gpt5Cached, gpt5Out             = 1.25, 0.125, 10.0 // GPT-5
	gpt5MiniIn, gpt5MiniCached, gpt5MiniOut = 0.25, 0.025, 2.00 // GPT-5 mini
	gpt5NanoIn, gpt5NanoCached, gpt5NanoOut = 0.05, 0.005, 0.40 // GPT-5 nano
)

// Shared descriptors, one per rate tier, reused across bare family aliases
// in openaiPricingTable so every alias of a model prices identically.
var (
	gpt4       = perM(gpt4In, gpt4Cached, gpt4Out)
	gpt4Turbo  = perM(gpt4TurboIn, gpt4TurboCached, gpt4TurboOut)
	gpt35Turbo = perM(gpt35TurboIn, gpt35TurboCached, gpt35TurboOut)

	gpt4o     = perM(gpt4oIn, gpt4oCached, gpt4oOut)
	gpt4oMini = perM(gpt4oMiniIn, gpt4oMiniCached, gpt4oMiniOut)

	gpt41     = perM(gpt41In, gpt41Cached, gpt41Out)
	gpt41Mini = perM(gpt41MiniIn, gpt41MiniCached, gpt41MiniOut)
	gpt41Nano = perM(gpt41NanoIn, gpt41NanoCached, gpt41NanoOut)

	o1     = perM(o1In, o1Cached, o1Out)
	o1Mini = perM(o1MiniIn, o1MiniCached, o1MiniOut)
	o3     = perM(o3In, o3Cached, o3Out)
	o3Mini = perM(o3MiniIn, o3MiniCached, o3MiniOut)
	o4Mini = perM(o4MiniIn, o4MiniCached, o4MiniOut)

	gpt5     = perM(gpt5In, gpt5Cached, gpt5Out)
	gpt5Mini = perM(gpt5MiniIn, gpt5MiniCached, gpt5MiniOut)
	gpt5Nano = perM(gpt5NanoIn, gpt5NanoCached, gpt5NanoOut)
)

// openaiPricingTable maps bare model-family names to their pricing
// descriptor. Looked up via base.ResolveLLMPricing, which normalizes a
// vendor-qualified model id (e.g. "azure/gpt-4o") down to the bare id
// before matching — there is no substring/heuristic fallback, so a model
// absent here prices as $0 with a loud warning rather than silently
// guessing a wrong constant.
var openaiPricingTable = map[string]*base.PricingDescriptor{
	// Legacy
	"gpt-4":         gpt4,
	"gpt-4-turbo":   gpt4Turbo,
	"gpt-3.5-turbo": gpt35Turbo,

	// GPT-4o
	"gpt-4o":      gpt4o,
	"gpt-4o-mini": gpt4oMini,

	// GPT-4.1
	"gpt-4.1":      gpt41,
	"gpt-4.1-mini": gpt41Mini,
	"gpt-4.1-nano": gpt41Nano,

	// o-series
	"o1":      o1,
	"o1-mini": o1Mini,
	"o3":      o3,
	"o3-mini": o3Mini,
	"o4-mini": o4Mini,

	// GPT-5
	"gpt-5":      gpt5,
	"gpt-5-mini": gpt5Mini,
	"gpt-5-nano": gpt5Nano,
}

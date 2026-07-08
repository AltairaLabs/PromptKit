package gemini

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// geminiPricingCorrectAtYear/Month/Day pin the capture date stamped onto
// every table entry below. Named instead of inlined into time.Date so the
// magic-number linter doesn't fire on the call site.
const (
	geminiPricingCorrectAtYear  = 2026
	geminiPricingCorrectAtMonth = time.July
	geminiPricingCorrectAtDay   = 7
)

// geminiPricingCorrectAt is the date these per-model rates were captured.
// RATES NEED HUMAN VERIFICATION against https://ai.google.dev/pricing — they
// were filled in from best-available knowledge, not fetched live. See the
// task-10 report for the full per-model list and verification note. Notably,
// Google publishes tiered input/output rates for some models (a higher rate
// above a context-length threshold, e.g. 128K/200K tokens); this table prices
// only the lower (short-context) tier — long-context requests will be
// understated until a dimensioned price item is added.
var geminiPricingCorrectAt = time.Date(
	geminiPricingCorrectAtYear, geminiPricingCorrectAtMonth, geminiPricingCorrectAtDay,
	0, 0, 0, 0, time.UTC,
)

// perMillionScale converts a published per-1M-token USD rate into a per-unit rate.
const perMillionScale = 1_000_000.0

// geminiCacheReadDiscount: Gemini prices an (implicit or explicit) cache hit
// at 50% of the input rate. See https://ai.google.dev/gemini-api/docs/caching.
const geminiCacheReadDiscount = 0.5

// geminiPerM builds a per-model pricing descriptor from published
// per-1M-token USD input/output rates. thinking gates whether the model bills
// "thinking"/reasoning tokens separately (at the output rate) — Gemini 2.5+
// "thinking" models do; earlier non-thinking models never emit
// ThoughtsTokenCount, so the item is simply never priced for them, but adding
// it unconditionally on non-thinking models would mean a future thinking
// backport prices silently at $0 until this table is updated, so it stays
// explicit.
func geminiPerM(input, output float64, thinking bool) *base.PricingDescriptor {
	items := []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: input / perMillionScale},
		{Unit: base.UnitOutputToken, Rate: output / perMillionScale},
		{Unit: base.UnitCacheReadToken, Rate: input * geminiCacheReadDiscount / perMillionScale},
	}
	if thinking {
		items = append(items, base.PriceItem{Unit: base.UnitReasoningToken, Rate: output / perMillionScale})
	}
	return &base.PricingDescriptor{
		Source:           base.PricingSourceInline,
		PricingCorrectAt: geminiPricingCorrectAt,
		Items:            items,
	}
}

// Per-1M-token USD rates (input, output) for every current Gemini model
// family. RATES NEED HUMAN VERIFICATION against https://ai.google.dev/pricing
// before being relied on for real billing — see the task-10 report. The 3.x
// rates in particular are low-confidence estimates (no verified public
// pricing available at capture time) and are flagged individually below.
const (
	gemini15ProIn, gemini15ProOut     = 1.25, 5.00  // Gemini 1.5 Pro (short-context tier)
	gemini15FlashIn, gemini15FlashOut = 0.075, 0.30 // Gemini 1.5 Flash (short-context tier)

	gemini20FlashIn, gemini20FlashOut         = 0.10, 0.40  // Gemini 2.0 Flash
	gemini20FlashLiteIn, gemini20FlashLiteOut = 0.075, 0.30 // Gemini 2.0 Flash-Lite

	gemini25ProIn, gemini25ProOut             = 1.25, 10.0 // Gemini 2.5 Pro (short-context tier; thinking)
	gemini25FlashIn, gemini25FlashOut         = 0.30, 2.50 // Gemini 2.5 Flash (thinking)
	gemini25FlashLiteIn, gemini25FlashLiteOut = 0.10, 0.40 // Gemini 2.5 Flash-Lite (thinking)

	// Gemini 3.x: LOW-CONFIDENCE ESTIMATE. No verified public pricing was
	// available at capture time; these are extrapolated from the 2.5 tier
	// pricing trend and MUST be verified before being relied on for billing.
	gemini3ProIn, gemini3ProOut             = 2.00, 12.00 // Gemini 3 Pro (thinking) — UNVERIFIED
	gemini3FlashIn, gemini3FlashOut         = 0.40, 3.00  // Gemini 3 Flash (thinking) — UNVERIFIED
	gemini3FlashLiteIn, gemini3FlashLiteOut = 0.15, 0.60  // Gemini 3 Flash-Lite (thinking) — UNVERIFIED
)

// Shared descriptors, one per rate tier, reused across dated snapshot IDs and
// bare family aliases in geminiPricingTable so every alias of a model prices
// identically.
var (
	gemini15Pro       = geminiPerM(gemini15ProIn, gemini15ProOut, false)
	gemini15Flash     = geminiPerM(gemini15FlashIn, gemini15FlashOut, false)
	gemini20Flash     = geminiPerM(gemini20FlashIn, gemini20FlashOut, false)
	gemini20FlashLite = geminiPerM(gemini20FlashLiteIn, gemini20FlashLiteOut, false)
	gemini25Pro       = geminiPerM(gemini25ProIn, gemini25ProOut, true)
	gemini25Flash     = geminiPerM(gemini25FlashIn, gemini25FlashOut, true)
	gemini25FlashLite = geminiPerM(gemini25FlashLiteIn, gemini25FlashLiteOut, true)
	gemini3Pro        = geminiPerM(gemini3ProIn, gemini3ProOut, true)
	gemini3Flash      = geminiPerM(gemini3FlashIn, gemini3FlashOut, true)
	gemini3FlashLite  = geminiPerM(gemini3FlashLiteIn, gemini3FlashLiteOut, true)
)

// Model IDs shared with the legacy geminiPricing() heuristic fallback in
// gemini.go (still used directly by streaming_support.go's
// applyPricingConfig for the Live/duplex session, a separate cost path from
// costFromUsage), named here so the id string is spelled once instead of
// duplicated across the two tables (goconst).
const (
	idGemini15Pro   = "gemini-1.5-pro"
	idGemini15Flash = "gemini-1.5-flash"
	idGemini25Pro   = "gemini-2.5-pro"
	idGemini25Flash = "gemini-2.5-flash"
)

// geminiPricingTable maps both dated/versioned model IDs and bare family
// aliases to their pricing descriptor. Looked up via base.ResolveLLMPricing,
// which normalizes a vendor-qualified model id down to the bare id before
// matching — there is no substring/heuristic fallback, so a model absent here
// prices as $0 with a loud warning rather than silently guessing a wrong
// constant (see PriceUsage).
var geminiPricingTable = map[string]*base.PricingDescriptor{
	// Gemini 1.5
	idGemini15Pro:          gemini15Pro,
	"gemini-1.5-pro-002":   gemini15Pro,
	idGemini15Flash:        gemini15Flash,
	"gemini-1.5-flash-002": gemini15Flash,

	// Gemini 2.0
	"gemini-2.0-flash":      gemini20Flash,
	"gemini-2.0-flash-001":  gemini20Flash,
	"gemini-2.0-flash-exp":  gemini20Flash,
	"gemini-2.0-flash-lite": gemini20FlashLite,

	// Gemini 2.5
	idGemini25Pro:           gemini25Pro,
	idGemini25Flash:         gemini25Flash,
	"gemini-2.5-flash-lite": gemini25FlashLite,

	// Gemini 3.x — UNVERIFIED, see const block comment above.
	"gemini-3-pro":        gemini3Pro,
	"gemini-3-flash":      gemini3Flash,
	"gemini-3-flash-lite": gemini3FlashLite,
}

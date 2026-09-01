package gemini

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
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

	// Gemini 3.x — VERIFIED 2026-08-28 against ai.google.dev/gemini-api/docs/pricing
	// (cross-checked against the .md.txt form of the same page).
	//
	// These replace low-confidence estimates that had been extrapolated from the
	// 2.5 tier. Two of those estimates were materially wrong, and one of them in
	// the direction that under-bills.
	//
	// There is NO single "3.x flash" rate: the versions differ by up to 2x, so
	// they get one descriptor each rather than sharing a family tier the way 1.5
	// and 2.0 do.
	//
	// Where a model tiers by context length or modality, the SHORT-CONTEXT TEXT
	// rate is used, matching the convention already applied to 1.5 Pro and 2.5
	// Pro above. Audio-in and >200k-context rates are higher and are not modeled
	// here.
	gemini31ProIn, gemini31ProOut = 2.00, 12.00 // Gemini 3.1 Pro, prompts <= 200k (>200k: 4.00/18.00)

	// 3.7 and 3.6 Flash share an INTRODUCTORY rate that expires. From
	// 2027-01-01 both become 1.50 / 7.50 — double. This table has no time
	// dimension, so that is a diary entry, not something the code will notice.
	gemini37FlashIn, gemini37FlashOut = 0.75, 3.75 // Gemini 3.7 Flash, introductory through 2026-12-31
	gemini36FlashIn, gemini36FlashOut = 0.75, 3.75 // Gemini 3.6 Flash, introductory through 2026-12-31

	gemini35FlashIn, gemini35FlashOut = 1.50, 9.00 // Gemini 3.5 Flash
	gemini3FlashIn, gemini3FlashOut   = 0.50, 3.00 // Gemini 3 Flash preview, text/image/video (audio in: 1.00)

	gemini35FlashLiteIn, gemini35FlashLiteOut = 0.30, 2.50 // Gemini 3.5 Flash-Lite
	gemini31FlashLiteIn, gemini31FlashLiteOut = 0.25, 1.50 // Gemini 3.1 Flash-Lite, text/image/video (audio in: 0.50)
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
	gemini31Pro       = geminiPerM(gemini31ProIn, gemini31ProOut, true)
	gemini37Flash     = geminiPerM(gemini37FlashIn, gemini37FlashOut, true)
	gemini36Flash     = geminiPerM(gemini36FlashIn, gemini36FlashOut, true)
	gemini35Flash     = geminiPerM(gemini35FlashIn, gemini35FlashOut, true)
	gemini3Flash      = geminiPerM(gemini3FlashIn, gemini3FlashOut, true)
	gemini35FlashLite = geminiPerM(gemini35FlashLiteIn, gemini35FlashLiteOut, true)
	gemini31FlashLite = geminiPerM(gemini31FlashLiteIn, gemini31FlashLiteOut, true)
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

	// Gemini 3.x — keyed by the IDs the API actually serves.
	//
	// The previous rows were "gemini-3-pro", "gemini-3-flash" and
	// "gemini-3-flash-lite", which match NO released model: every real 3.x id
	// carries a minor version ("gemini-3.7-flash"). ResolveLLMPricing has no
	// substring fallback by design, so each of those models resolved to nothing
	// and billed at ZERO with a warning. Verified live before this change:
	// gemini-3.7-flash, -3.6-flash, -3.5-flash and -3.1-pro-preview all
	// returned priced=false.
	"gemini-3.7-flash":       gemini37Flash,
	"gemini-3.6-flash":       gemini36Flash,
	"gemini-3.5-flash":       gemini35Flash,
	"gemini-3-flash-preview": gemini3Flash,
	"gemini-3.5-flash-lite":  gemini35FlashLite,
	"gemini-3.1-flash-lite":  gemini31FlashLite,
	"gemini-3.1-pro-preview": gemini31Pro,
	"gemini-3.1-pro":         gemini31Pro,
}

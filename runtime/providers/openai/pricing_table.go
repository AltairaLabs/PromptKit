// Package openai provides OpenAI LLM provider integration.
package openai

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// openaiPricingCorrectAtYear/Month/Day pin the capture date stamped onto
// every table entry below. Named instead of inlined into time.Date so the
// magic-number linter doesn't fire on the call site.
const (
	openaiPricingCorrectAtYear  = 2026
	openaiPricingCorrectAtMonth = time.July
	openaiPricingCorrectAtDay   = 8

	// The GPT-5.1 – 6.x rows and GPT-5/o3 pro were read from the pricing page
	// on this later date.
	openaiPricingCheckedYear  = 2026
	openaiPricingCheckedMonth = time.November
	openaiPricingCheckedDay   = 20
)

// openaiPricingCorrectAt is the date these per-model rates were captured.
// RATES NEED HUMAN VERIFICATION against https://openai.com/api/pricing —
// they were filled in from best-available knowledge, not fetched live. See
// the task-12 report for the full per-model list and verification note.
var openaiPricingCorrectAt = time.Date(
	openaiPricingCorrectAtYear, openaiPricingCorrectAtMonth, openaiPricingCorrectAtDay,
	0, 0, 0, 0, time.UTC,
)

// openaiPricingCheckedAt stamps the rows read from
// https://developers.openai.com/api/docs/pricing on 2026-11-20.
var openaiPricingCheckedAt = time.Date(
	openaiPricingCheckedYear, openaiPricingCheckedMonth, openaiPricingCheckedDay,
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
//
// These models charge nothing extra to write the prompt cache, so a written
// token (should one be reported) bills at the input rate, as it did before
// writes were reported separately.
func perM(input, cachedInput, output float64) *base.PricingDescriptor {
	return perMAt(openaiPricingCorrectAt, input, cachedInput, output)
}

// perMAt is perM with the date the rates were checked against the price list.
func perMAt(checked time.Time, input, cachedInput, output float64) *base.PricingDescriptor {
	return &base.PricingDescriptor{
		Source:           base.PricingSourceInline,
		PricingCorrectAt: checked,
		Items: append(tokenItems(input, cachedInput, output),
			base.PriceItem{Unit: base.UnitCacheWriteToken, Rate: input / perMillionScale}),
	}
}

// Cache-write premiums for GPT-5.6 and later, as multiples of the input rate:
// 1.25x for the default 30-minute retention, 2.5x for the 12-hour retention
// the Responses API offers (https://developers.openai.com/api/docs/guides/prompt-caching).
const (
	cacheWrite30mPremium = 1.25
	cacheWrite12hPremium = 2.5
)

// perMCacheWrite is perMAt for a model that bills cache writes at a premium.
// The 12-hour item carries cacheWriteTTLDimension, so it wins only for a
// request costFromUsage marks as a 12-hour write.
func perMCacheWrite(input, cachedInput, output float64) *base.PricingDescriptor {
	return &base.PricingDescriptor{
		Source:           base.PricingSourceInline,
		PricingCorrectAt: openaiPricingCheckedAt,
		Items: append(tokenItems(input, cachedInput, output),
			base.PriceItem{Unit: base.UnitCacheWriteToken, Rate: input * cacheWrite30mPremium / perMillionScale},
			base.PriceItem{
				Unit: base.UnitCacheWriteToken, Rate: input * cacheWrite12hPremium / perMillionScale,
				Dimensions: map[string]string{cacheWriteTTLDimension: cacheWriteTTL12h},
			}),
	}
}

// tokenItems are the input, cache-read, output and reasoning items every
// OpenAI text model carries.
func tokenItems(input, cachedInput, output float64) []base.PriceItem {
	return []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: input / perMillionScale},
		{Unit: base.UnitCacheReadToken, Rate: cachedInput / perMillionScale},
		{Unit: base.UnitOutputToken, Rate: output / perMillionScale},
		{Unit: base.UnitReasoningToken, Rate: output / perMillionScale},
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
	gpt5ProIn, gpt5ProOut                   = 15.0, 120.0       // GPT-5 pro (no cached discount)
	o3ProIn, o3ProOut                       = 20.0, 80.0        // o3-pro (no cached discount)

	// GPT-5.1 – 5.6. Rows below are the Standard tier, short-context (<=272K
	// input) list price read from https://developers.openai.com/api/docs/pricing
	// on 2026-11-20. Models with a 1.05M window bill 2x input and 1.5x output for
	// a whole request above 272K input tokens; that tier is not modeled, so such
	// a request is understated (set pricing in the provider config to override).
	gpt51In, gpt51Cached, gpt51Out                = 1.25, 0.125, 10.0
	gpt52In, gpt52Cached, gpt52Out                = 1.75, 0.175, 14.0
	gpt52ProIn, gpt52ProOut                       = 21.0, 168.0 // no cached discount
	gpt54In, gpt54Cached, gpt54Out                = 2.50, 0.25, 15.0
	gpt54MiniIn, gpt54MiniCached, gpt54MiniOut    = 0.75, 0.075, 4.50
	gpt54NanoIn, gpt54NanoCached, gpt54NanoOut    = 0.20, 0.02, 1.25
	gpt5xProIn, gpt5xProOut                       = 30.0, 180.0 // GPT-5.4 pro and 5.5 pro; no cached discount
	gpt55In, gpt55Cached, gpt55Out                = 5.00, 0.50, 30.0
	gpt56SolIn, gpt56SolCached, gpt56SolOut       = 4.00, 0.40, 20.0 // promotional price, published until 2026-11-30
	gpt56TerraIn, gpt56TerraCached, gpt56TerraOut = 2.00, 0.20, 12.0
	gpt56LunaIn, gpt56LunaCached, gpt56LunaOut    = 0.20, 0.02, 1.20

	// GPT-6.x (same source and short-context tier as above).
	gpt6AstraIn, gpt6AstraCached, gpt6AstraOut    = 10.0, 1.00, 50.0
	gpt6SolIn, gpt6SolCached, gpt6SolOut          = 2.00, 0.20, 10.0
	gpt6LunaIn, gpt6LunaCached, gpt6LunaOut       = 0.10, 0.01, 0.50
	gpt61SolIn, gpt61SolCached, gpt61SolOut       = 2.00, 0.10, 10.0 // also GPT-6.2 sol
	gpt61TerraIn, gpt61TerraCached, gpt61TerraOut = 1.00, 0.05, 5.00
	gpt62AstraIn, gpt62AstraCached, gpt62AstraOut = 8.00, 0.40, 40.0
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
	// A model with no cached-input discount bills any cached tokens at the
	// input rate, so its cache_read rate is its input rate.
	gpt5Pro = perMAt(openaiPricingCheckedAt, gpt5ProIn, gpt5ProIn, gpt5ProOut)
	o3Pro   = perMAt(openaiPricingCheckedAt, o3ProIn, o3ProIn, o3ProOut)

	gpt51     = perMAt(openaiPricingCheckedAt, gpt51In, gpt51Cached, gpt51Out)
	gpt52     = perMAt(openaiPricingCheckedAt, gpt52In, gpt52Cached, gpt52Out)
	gpt52Pro  = perMAt(openaiPricingCheckedAt, gpt52ProIn, gpt52ProIn, gpt52ProOut)
	gpt54     = perMAt(openaiPricingCheckedAt, gpt54In, gpt54Cached, gpt54Out)
	gpt54Mini = perMAt(openaiPricingCheckedAt, gpt54MiniIn, gpt54MiniCached, gpt54MiniOut)
	gpt54Nano = perMAt(openaiPricingCheckedAt, gpt54NanoIn, gpt54NanoCached, gpt54NanoOut)
	gpt5xPro  = perMAt(openaiPricingCheckedAt, gpt5xProIn, gpt5xProIn, gpt5xProOut)
	gpt55     = perMAt(openaiPricingCheckedAt, gpt55In, gpt55Cached, gpt55Out)
	// GPT-5.6 and later bill cache writes at a premium.
	gpt56Sol  = perMCacheWrite(gpt56SolIn, gpt56SolCached, gpt56SolOut)
	gpt56Ter  = perMCacheWrite(gpt56TerraIn, gpt56TerraCached, gpt56TerraOut)
	gpt56Luna = perMCacheWrite(gpt56LunaIn, gpt56LunaCached, gpt56LunaOut)

	gpt6Astra  = perMCacheWrite(gpt6AstraIn, gpt6AstraCached, gpt6AstraOut)
	gpt6Sol    = perMCacheWrite(gpt6SolIn, gpt6SolCached, gpt6SolOut)
	gpt6Luna   = perMCacheWrite(gpt6LunaIn, gpt6LunaCached, gpt6LunaOut)
	gpt61Sol   = perMCacheWrite(gpt61SolIn, gpt61SolCached, gpt61SolOut)
	gpt61Terra = perMCacheWrite(gpt61TerraIn, gpt61TerraCached, gpt61TerraOut)
	gpt62Astra = perMCacheWrite(gpt62AstraIn, gpt62AstraCached, gpt62AstraOut)
)

// openaiPricingTable maps model aliases to their pricing descriptor. Looked up
// via base.ResolveLLMPricing, which strips a vendor prefix ("azure/gpt-4o")
// and falls back from a dated snapshot ("gpt-5.5-2026-04-23") to its alias.
// There is no other family or substring fallback, so a model absent here is
// reported unpriced, with a warning naming it, rather than billed at a guessed
// sibling's rate.
//
//nolint:goconst // ids are shared with the context-window table by value; each table spells its own keys
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
	modelGPT5:    gpt5,
	"gpt-5-mini": gpt5Mini,
	"gpt-5-nano": gpt5Nano,
	"gpt-5-pro":  gpt5Pro,
	"o3-pro":     o3Pro,

	// GPT-5.1 – 5.6
	"gpt-5.1":       gpt51,
	"gpt-5.2":       gpt52,
	"gpt-5.2-pro":   gpt52Pro,
	"gpt-5.4":       gpt54,
	"gpt-5.4-mini":  gpt54Mini,
	"gpt-5.4-nano":  gpt54Nano,
	"gpt-5.4-pro":   gpt5xPro,
	"gpt-5.5":       gpt55,
	"gpt-5.5-pro":   gpt5xPro,
	"gpt-5.6-sol":   gpt56Sol,
	"gpt-5.6-terra": gpt56Ter,
	"gpt-5.6-luna":  gpt56Luna,

	// GPT-6.x
	"gpt-6-astra":   gpt6Astra,
	"gpt-6-sol":     gpt6Sol,
	"gpt-6-luna":    gpt6Luna,
	"gpt-6.1-sol":   gpt61Sol,
	"gpt-6.1-terra": gpt61Terra,
	"gpt-6.2-sol":   gpt61Sol,
	"gpt-6.2-astra": gpt62Astra,
}

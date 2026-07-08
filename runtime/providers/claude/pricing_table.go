package claude

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// claudePricingCorrectAtYear/Month/Day pin the capture date stamped onto
// every table entry below. Named instead of inlined into time.Date so the
// magic-number linter doesn't fire on the call site.
const (
	claudePricingCorrectAtYear  = 2026
	claudePricingCorrectAtMonth = time.July
	claudePricingCorrectAtDay   = 7
)

// claudePricingCorrectAt is the date these per-model rates were captured.
// RATES NEED HUMAN VERIFICATION against https://www.anthropic.com/pricing —
// they were filled in from best-available knowledge, not fetched live. See
// the task-9 report for the full per-model list and verification note.
var claudePricingCorrectAt = time.Date(
	claudePricingCorrectAtYear, claudePricingCorrectAtMonth, claudePricingCorrectAtDay,
	0, 0, 0, 0, time.UTC,
)

// perMillionScale converts a published per-1M-token USD rate into a per-unit rate.
const perMillionScale = 1_000_000.0

// Anthropic prompt-cache economics are stable multipliers of the input rate,
// independent of model tier: a cache READ (hit) is discounted, a cache WRITE
// (miss; creates the 5-minute ephemeral cache entry) carries a premium. See
// https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching.
const (
	cacheReadDiscount = 0.1  // cache_read_token = 10% of input rate
	cacheWritePremium = 1.25 // cache_write_token = 125% of input rate
)

// perM builds a per-model pricing descriptor from published per-1M-token USD
// input/output rates, deriving the cache_read and cache_write items from the
// standard Anthropic multipliers above.
func perM(input, output float64) *base.PricingDescriptor {
	return &base.PricingDescriptor{
		Source:           base.PricingSourceInline,
		PricingCorrectAt: claudePricingCorrectAt,
		Items: []base.PriceItem{
			{Unit: base.UnitInputToken, Rate: input / perMillionScale},
			{Unit: base.UnitOutputToken, Rate: output / perMillionScale},
			{Unit: base.UnitCacheReadToken, Rate: input * cacheReadDiscount / perMillionScale},
			{Unit: base.UnitCacheWriteToken, Rate: input * cacheWritePremium / perMillionScale},
		},
	}
}

// Per-1M-token USD rates (input, output) for every current Claude model
// family. RATES NEED HUMAN VERIFICATION against https://www.anthropic.com/pricing
// before being relied on for real billing — see the task-9 report.
const (
	claude3HaikuIn, claude3HaikuOut   = 0.25, 1.25 // Claude 3 Haiku
	claude3SonnetIn, claude3SonnetOut = 3.0, 15.0  // Claude 3 Sonnet
	claude3OpusIn, claude3OpusOut     = 15.0, 75.0 // Claude 3 Opus

	claude35SonnetIn, claude35SonnetOut = 3.0, 15.0 // Claude 3.5 Sonnet
	claude35HaikuIn, claude35HaikuOut   = 1.0, 5.0  // Claude 3.5 Haiku

	claude37SonnetIn, claude37SonnetOut = 3.0, 15.0 // Claude 3.7 Sonnet

	claudeSonnet4In, claudeSonnet4Out = 3.0, 15.0  // Claude Sonnet 4
	claudeOpus4In, claudeOpus4Out     = 15.0, 75.0 // Claude Opus 4
	claudeOpus41In, claudeOpus41Out   = 15.0, 75.0 // Claude Opus 4.1

	claudeSonnet45In, claudeSonnet45Out = 3.0, 15.0 // Claude Sonnet 4.5
	claudeHaiku45In, claudeHaiku45Out   = 1.0, 5.0  // Claude Haiku 4.5
	// Claude Opus 4.5 reportedly launched at a lower price than prior Opus
	// releases; the exact figure below is a low-confidence guess pending
	// verification (flagged in the task-9 report).
	claudeOpus45In, claudeOpus45Out = 5.0, 25.0 // Claude Opus 4.5
)

// Shared descriptors, one per rate tier, reused across dated snapshot IDs and
// bare family aliases in claudePricingTable so every alias of a model prices
// identically.
var (
	claude3Haiku   = perM(claude3HaikuIn, claude3HaikuOut)
	claude3Sonnet  = perM(claude3SonnetIn, claude3SonnetOut)
	claude3Opus    = perM(claude3OpusIn, claude3OpusOut)
	claude35Sonnet = perM(claude35SonnetIn, claude35SonnetOut)
	claude35Haiku  = perM(claude35HaikuIn, claude35HaikuOut)
	claude37Sonnet = perM(claude37SonnetIn, claude37SonnetOut)
	claudeSonnet4  = perM(claudeSonnet4In, claudeSonnet4Out)
	claudeOpus4    = perM(claudeOpus4In, claudeOpus4Out)
	claudeOpus41   = perM(claudeOpus41In, claudeOpus41Out)
	claudeSonnet45 = perM(claudeSonnet45In, claudeSonnet45Out)
	claudeHaiku45  = perM(claudeHaiku45In, claudeHaiku45Out)
	claudeOpus45   = perM(claudeOpus45In, claudeOpus45Out)
)

// Model IDs shared with the legacy claudePricing() heuristic fallback in
// claude.go (still exercised directly by claude_pricing_test.go), named here
// so the id string is spelled once instead of duplicated across the two
// tables (goconst).
const (
	idClaude3Haiku20240307   = "claude-3-haiku-20240307"
	idClaude3Sonnet20240229  = "claude-3-sonnet-20240229"
	idClaude3Opus20240229    = "claude-3-opus-20240229"
	idClaude35Sonnet20240620 = "claude-3-5-sonnet-20240620"
	idClaude35Sonnet20241022 = "claude-3-5-sonnet-20241022"
	idClaude35Haiku20241022  = "claude-3-5-haiku-20241022"
	idClaudeSonnet45         = "claude-sonnet-4-5"
	idClaudeHaiku45          = "claude-haiku-4-5"
)

// claudePricingTable maps both dated snapshot IDs and bare family aliases to
// their pricing descriptor. Looked up via base.ResolveLLMPricing, which
// normalizes a vendor-qualified model id (e.g. "anthropic/claude-sonnet-4-5")
// down to the bare id before matching — there is no substring/heuristic
// fallback, so a model absent here prices as $0 with a loud warning rather
// than silently guessing a wrong constant.
var claudePricingTable = map[string]*base.PricingDescriptor{
	// Claude 3
	idClaude3Haiku20240307:  claude3Haiku,
	idClaude3Sonnet20240229: claude3Sonnet,
	idClaude3Opus20240229:   claude3Opus,
	"claude-3-haiku":        claude3Haiku,
	"claude-3-sonnet":       claude3Sonnet,
	"claude-3-opus":         claude3Opus,

	// Claude 3.5
	idClaude35Sonnet20240620: claude35Sonnet,
	idClaude35Sonnet20241022: claude35Sonnet,
	idClaude35Haiku20241022:  claude35Haiku,
	"claude-3-5-sonnet":      claude35Sonnet,
	"claude-3-5-haiku":       claude35Haiku,

	// Claude 3.7
	"claude-3-7-sonnet-20250219": claude37Sonnet,
	"claude-3-7-sonnet":          claude37Sonnet,

	// Claude 4
	"claude-sonnet-4-20250514": claudeSonnet4,
	"claude-opus-4-20250514":   claudeOpus4,
	"claude-opus-4-1-20250805": claudeOpus41,
	"claude-sonnet-4":          claudeSonnet4,
	"claude-opus-4":            claudeOpus4,
	"claude-opus-4-1":          claudeOpus41,

	// Claude 4.5
	"claude-sonnet-4-5-20250929": claudeSonnet45,
	"claude-haiku-4-5-20251001":  claudeHaiku45,
	idClaudeSonnet45:             claudeSonnet45,
	idClaudeHaiku45:              claudeHaiku45,
	"claude-opus-4-5":            claudeOpus45,
}

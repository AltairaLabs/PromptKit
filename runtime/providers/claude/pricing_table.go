package claude

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// claudePricingCorrectAtYear/Month/Day pin the capture date stamped onto
// every table entry below. Named instead of inlined into time.Date so the
// magic-number linter doesn't fire on the call site.
const (
	claudePricingCorrectAtYear  = 2026
	claudePricingCorrectAtMonth = time.October
	claudePricingCorrectAtDay   = 30
)

// claudePricingCorrectAt is the date these per-model rates were checked
// against https://platform.claude.com/docs/en/about-claude/pricing (Anthropic
// first-party API, global routing). The retired Claude 3 / 3.5 Sonnet / 3.7
// rows are no longer on that page and keep their last published rates.
var claudePricingCorrectAt = time.Date(
	claudePricingCorrectAtYear, claudePricingCorrectAtMonth, claudePricingCorrectAtDay,
	0, 0, 0, 0, time.UTC,
)

// perMillionScale converts a published per-1M-token USD rate into a per-unit rate.
const perMillionScale = 1_000_000.0

// A cache WRITE (creating a 5-minute ephemeral entry) costs 1.25x the input
// rate on every model. Cache READS do not share one multiplier: 0.1x on most
// models, 0.05x on Opus 5.5 / Sonnet 5.5 and 0.025x on Fable 5.1, so each
// model's read rate is listed explicitly below rather than derived.
const cacheWritePremium = 1.25

// perM builds a per-model pricing descriptor from published per-1M-token USD
// input, output and cache-read rates.
func perM(input, output, cacheRead float64) *base.PricingDescriptor {
	return &base.PricingDescriptor{
		Source:           base.PricingSourceInline,
		PricingCorrectAt: claudePricingCorrectAt,
		Items:            priceItems(input, output, cacheRead, nil),
	}
}

func priceItems(input, output, cacheRead float64, dims map[string]string) []base.PriceItem {
	return []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: input / perMillionScale, Dimensions: dims},
		{Unit: base.UnitOutputToken, Rate: output / perMillionScale, Dimensions: dims},
		{Unit: base.UnitCacheReadToken, Rate: cacheRead / perMillionScale, Dimensions: dims},
		{Unit: base.UnitCacheWriteToken, Rate: input * cacheWritePremium / perMillionScale, Dimensions: dims},
	}
}

// Haiku 5.5 is priced by prompt length: a request whose prompt (input + cache
// reads + cache writes) is over longPromptThreshold tokens pays the higher
// rates for every unit. costFromUsage sets promptSizeDimension on such
// requests; the over-threshold items carry the dimension and win as the more
// specific match, and every other model ignores it.
const (
	longPromptThreshold = 100_000
	promptSizeDimension = "prompt_size"
	promptSizeOver100K  = "over_100k"
)

// perMTiered builds a descriptor whose rates rise when the prompt is over
// longPromptThreshold tokens.
func perMTiered(input, output, cacheRead, longInput, longOutput, longCacheRead float64) *base.PricingDescriptor {
	long := map[string]string{promptSizeDimension: promptSizeOver100K}
	return &base.PricingDescriptor{
		Source:           base.PricingSourceInline,
		PricingCorrectAt: claudePricingCorrectAt,
		Items: append(priceItems(input, output, cacheRead, nil),
			priceItems(longInput, longOutput, longCacheRead, long)...),
	}
}

// Per-1M-token USD rates (input, output, cache read) for every Claude model
// family, from the pricing page cited on claudePricingCorrectAt.
const (
	claude3HaikuIn, claude3HaikuOut, claude3HaikuRead    = 0.25, 1.25, 0.03 // Claude 3 Haiku (retired)
	claude3SonnetIn, claude3SonnetOut, claude3SonnetRead = 3.0, 15.0, 0.3   // Claude 3 Sonnet (retired)
	claude3OpusIn, claude3OpusOut, claude3OpusRead       = 15.0, 75.0, 1.5  // Claude 3 Opus (retired)

	claude35SonnetIn, claude35SonnetOut, claude35SonnetRead = 3.0, 15.0, 0.3 // Claude 3.5 Sonnet (retired)
	claude35HaikuIn, claude35HaikuOut, claude35HaikuRead    = 0.8, 4.0, 0.08 // Claude 3.5 Haiku

	claude37SonnetIn, claude37SonnetOut, claude37SonnetRead = 3.0, 15.0, 0.3 // Claude 3.7 Sonnet (retired)

	claudeSonnet4In, claudeSonnet4Out, claudeSonnet4Read = 3.0, 15.0, 0.3  // Claude Sonnet 4
	claudeOpus4In, claudeOpus4Out, claudeOpus4Read       = 15.0, 75.0, 1.5 // Claude Opus 4 and 4.1

	claudeSonnet45In, claudeSonnet45Out, claudeSonnet45Read = 3.0, 15.0, 0.3 // Claude Sonnet 4.5 and 4.6
	claudeHaiku45In, claudeHaiku45Out, claudeHaiku45Read    = 1.0, 5.0, 0.1  // Claude Haiku 4.5
	claudeOpus45In, claudeOpus45Out, claudeOpus45Read       = 5.0, 25.0, 0.5 // Claude Opus 4.5 through 4.8, and Opus 5

	claudeSonnet5In, claudeSonnet5Out, claudeSonnet5Read    = 2.0, 10.0, 0.2  // Claude Sonnet 5
	claudeSonnet55In, claudeSonnet55Out, claudeSonnet55Read = 2.0, 10.0, 0.1  // Claude Sonnet 5.5
	claudeOpus55In, claudeOpus55Out, claudeOpus55Read       = 4.0, 20.0, 0.2  // Claude Opus 5.5
	claudeFable5In, claudeFable5Out, claudeFable5Read       = 10.0, 50.0, 1.0 // Claude Fable 5 / Mythos 5
	claudeFable51Read                                       = 0.25            // Claude Fable 5.1 / Mythos 5.1

	claudeHaiku55In, claudeHaiku55Out, claudeHaiku55Read             = 0.1, 0.5, 0.01 // Claude Haiku 5.5, prompt <= 100K
	claudeHaiku55LongIn, claudeHaiku55LongOut, claudeHaiku55LongRead = 0.5, 2.5, 0.05 // Claude Haiku 5.5, prompt > 100K
)

// Shared descriptors, one per rate tier, reused across dated snapshot IDs and
// bare family aliases in claudePricingTable so every alias of a model prices
// identically.
var (
	claude3Haiku   = perM(claude3HaikuIn, claude3HaikuOut, claude3HaikuRead)
	claude3Sonnet  = perM(claude3SonnetIn, claude3SonnetOut, claude3SonnetRead)
	claude3Opus    = perM(claude3OpusIn, claude3OpusOut, claude3OpusRead)
	claude35Sonnet = perM(claude35SonnetIn, claude35SonnetOut, claude35SonnetRead)
	claude35Haiku  = perM(claude35HaikuIn, claude35HaikuOut, claude35HaikuRead)
	claude37Sonnet = perM(claude37SonnetIn, claude37SonnetOut, claude37SonnetRead)
	claudeSonnet4  = perM(claudeSonnet4In, claudeSonnet4Out, claudeSonnet4Read)
	claudeOpus4    = perM(claudeOpus4In, claudeOpus4Out, claudeOpus4Read)
	claudeSonnet45 = perM(claudeSonnet45In, claudeSonnet45Out, claudeSonnet45Read)
	claudeHaiku45  = perM(claudeHaiku45In, claudeHaiku45Out, claudeHaiku45Read)
	claudeOpus45   = perM(claudeOpus45In, claudeOpus45Out, claudeOpus45Read)
	claudeSonnet5  = perM(claudeSonnet5In, claudeSonnet5Out, claudeSonnet5Read)
	claudeSonnet55 = perM(claudeSonnet55In, claudeSonnet55Out, claudeSonnet55Read)
	claudeOpus55   = perM(claudeOpus55In, claudeOpus55Out, claudeOpus55Read)
	claudeFable5   = perM(claudeFable5In, claudeFable5Out, claudeFable5Read)
	claudeFable51  = perM(claudeFable5In, claudeFable5Out, claudeFable51Read)
	claudeHaiku55  = perMTiered(claudeHaiku55In, claudeHaiku55Out, claudeHaiku55Read,
		claudeHaiku55LongIn, claudeHaiku55LongOut, claudeHaiku55LongRead)
)

// Model IDs used as claudePricingTable keys, named so each id string is
// spelled once (goconst).
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
	"claude-opus-4-1-20250805": claudeOpus4,
	"claude-sonnet-4":          claudeSonnet4,
	"claude-opus-4":            claudeOpus4,
	modelOpus41:                claudeOpus4,

	// Claude 4.5
	"claude-sonnet-4-5-20250929": claudeSonnet45,
	"claude-haiku-4-5-20251001":  claudeHaiku45,
	idClaudeSonnet45:             claudeSonnet45,
	idClaudeHaiku45:              claudeHaiku45,
	"claude-opus-4-5-20251101":   claudeOpus45,
	modelOpus45:                  claudeOpus45,

	// Claude 4.6 – 4.8 (no dated snapshot IDs)
	"claude-sonnet-4-6": claudeSonnet45,
	"claude-opus-4-6":   claudeOpus45,
	"claude-opus-4-7":   claudeOpus45,
	"claude-opus-4-8":   claudeOpus45,

	// Claude 5 (no dated snapshot IDs)
	"claude-opus-5":     claudeOpus45,
	"claude-opus-5-5":   claudeOpus55,
	"claude-sonnet-5":   claudeSonnet5,
	"claude-sonnet-5-5": claudeSonnet55,
	"claude-haiku-5-5":  claudeHaiku55,
	"claude-fable-5":    claudeFable5,
	"claude-mythos-5":   claudeFable5,
	"claude-fable-5-1":  claudeFable51,
	"claude-mythos-5-1": claudeFable51,
}

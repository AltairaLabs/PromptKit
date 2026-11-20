package base

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// FlatPricing is the legacy per-1K input/output rate a config may carry. It is
// mapped to canonical input_token/output_token price items when no richer
// descriptor is supplied. base defines its own type (rather than reusing a
// providers-package type) to avoid importing the providers package, which
// would create an import cycle.
type FlatPricing struct {
	Input  float64 // per 1K input tokens
	Output float64 // per 1K output tokens
}

// per1KScale converts a per-1K rate to a per-unit rate.
const per1KScale = 1000.0

// ResolveLLMPricing selects the pricing descriptor for a model, in order:
// explicit config descriptor > flat config pricing > embedded default table.
// Both config forms bill cache-written tokens at the input rate unless the
// descriptor prices cache_write_token itself (see withCacheWriteAtInputRate).
//
// A provider that ships a table is a paid one, so a model missing from it
// resolves to an empty descriptor: PriceUsage then warns about every nonzero
// unit instead of reporting a silent $0. With no table (a self-hosted server
// such as vLLM) and no config, it returns nil, which prices at $0 quietly.
func ResolveLLMPricing(
	configDesc *PricingDescriptor, flat FlatPricing,
	table map[string]*PricingDescriptor, model string,
) *PricingDescriptor {
	if configDesc != nil && len(configDesc.Items) > 0 {
		return withCacheWriteAtInputRate(configDesc)
	}
	if flat.Input > 0 && flat.Output > 0 {
		// Reasoning tokens bill at the output rate for every provider that
		// splits them out (OpenAI o-series, Gemini thinking), so a flat config
		// — which does carry a known output rate — must price reasoning too, or
		// a thinking model under flat config understates cost by the reasoning
		// amount. A flat config is one input rate and one output rate, so audio,
		// image, video and cache-written input bill at them too, as they did
		// before providers split those out. (Cache READS are NOT priced here: a
		// flat config carries no provider-specific read discount, so they go
		// loud-unpriced.)
		in, out := flat.Input/per1KScale, flat.Output/per1KScale
		return &PricingDescriptor{Source: PricingSourceInline, Items: []PriceItem{
			{Unit: UnitInputToken, Rate: in},
			{Unit: UnitAudioInputToken, Rate: in},
			{Unit: UnitImageInputToken, Rate: in},
			{Unit: UnitVideoInputToken, Rate: in},
			{Unit: UnitCacheWriteToken, Rate: in},
			{Unit: UnitOutputToken, Rate: out},
			{Unit: UnitAudioOutputToken, Rate: out},
			{Unit: UnitReasoningToken, Rate: out},
		}}
	}
	if d, ok := lookupModel(table, model); ok {
		return d
	}
	if table != nil {
		warnUnknownModelOnce(&unknownPricingWarnOnce, model,
			"cost: no built-in pricing for model; its tokens are reported unpriced — "+
				"set pricing in the provider config")
		return &PricingDescriptor{Source: PricingSourceInline}
	}
	return nil
}

// withCacheWriteAtInputRate returns desc, or — when desc prices no
// cache_write_token — a copy that bills cache writes at desc's input rate, one
// item per input item with the same dimensions. A config written before
// providers reported cache writes apart from input (OpenAI folded them into
// prompt tokens) keeps the cost it recorded then; it is also OpenAI's actual
// charge for models with no write premium. A config that prices the unit keeps
// its own rate.
func withCacheWriteAtInputRate(desc *PricingDescriptor) *PricingDescriptor {
	var writes []PriceItem
	for _, it := range desc.Items {
		switch it.Unit {
		case UnitCacheWriteToken:
			return desc
		case UnitInputToken:
			writes = append(writes, PriceItem{Unit: UnitCacheWriteToken, Rate: it.Rate, Dimensions: it.Dimensions})
		}
	}
	if len(writes) == 0 {
		return desc
	}
	out := *desc
	out.Items = append(slices.Clone(desc.Items), writes...)
	return &out
}

// unknownPricingWarnOnce and unknownWindowWarnOnce remember which models have
// been reported missing from a built-in table, so a busy process logs each
// model once rather than once per call.
var unknownPricingWarnOnce, unknownWindowWarnOnce sync.Map

// warnUnknownModelOnce logs msg at warn level, naming model, the first time a
// given model misses a built-in table. PriceUsage's per-unit warning says the
// cost is understated; this one says which model needs configuring.
func warnUnknownModelOnce(seen *sync.Map, model, msg string) {
	if _, loaded := seen.LoadOrStore(model, struct{}{}); loaded {
		return
	}
	logger.Warn(msg, "model", model)
}

// lookupModel finds a model's row in a built-in table. The vendor prefix is
// stripped first; then the exact id is tried, then — if the id ends in a dated
// snapshot suffix — the alias without it. A snapshot is a pinned version of its
// alias, so it shares the alias's row unless the table carries one of its own.
//
// No other suffix is removed: "-pro", "-mini" and the like name a different
// model with its own price and window, and borrowing a sibling's row would
// report a confidently wrong number instead of an unknown one.
func lookupModel[V any](table map[string]V, model string) (V, bool) {
	id := normalizeModel(model)
	if v, ok := table[id]; ok {
		return v, true
	}
	if alias, ok := stripDateSuffix(id); ok {
		if v, ok := table[alias]; ok {
			return v, true
		}
	}
	var zero V
	return zero, false
}

// Dated snapshot suffix layouts: OpenAI's "-2026-04-23" and Anthropic's
// "-20250929".
const (
	dashedDateLayout  = "2006-01-02"
	compactDateLayout = "20060102"
)

// stripDateSuffix returns id without a trailing "-<date>" in either snapshot
// layout, and whether it had one. The suffix must parse as a real calendar
// date, so a version number or build tag that merely looks numeric stays put.
func stripDateSuffix(id string) (string, bool) {
	for _, layout := range []string{dashedDateLayout, compactDateLayout} {
		n := len(layout) + 1 // the date plus its leading "-"
		if len(id) <= n || id[len(id)-n] != '-' {
			continue
		}
		if _, err := time.Parse(layout, id[len(id)-n+1:]); err == nil {
			return id[:len(id)-n], true
		}
	}
	return "", false
}

// normalizeModel strips a leading vendor prefix (everything up to and
// including the last "/") so vendor-qualified model ids (e.g.
// "anthropic/claude-sonnet-4-5") match bare table keys. Providers whose wire
// model ids differ in other ways should pass an already-normalized model.
func normalizeModel(model string) string {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return model
}

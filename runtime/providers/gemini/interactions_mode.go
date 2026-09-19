package gemini

import (
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// APIMode selects which Gemini API a request is sent to.
//
// The two APIs differ in more than shape. On generateContent a response schema
// constrains EVERY turn, so it cannot coexist with function calling: Gemini 2.5
// rejects the combination outright, and Gemini 3 accepts it and then never
// stops calling tools. The Interactions API constrains only the turn that
// produces a final answer, so a tool loop there returns conforming JSON.
//
// See issue #1851.
type APIMode string

const (
	// APIModeGenerateContent is the long-standing models/{model}:generateContent
	// API. Default for every request that does not need the other one.
	APIModeGenerateContent APIMode = "generate_content"

	// APIModeInteractions is the v1beta/interactions API.
	APIModeInteractions APIMode = "interactions"

	// apiModeUnset means no explicit choice was configured, leaving the
	// per-request capability fallback to decide.
	apiModeUnset APIMode = ""
)

// configuredAPIMode reads additional_config.api_mode, mirroring the OpenAI
// provider's config-first ordering: a provider config is the source of truth,
// and any automatic selection is only a default for undeclared configs.
//
// Accepts "generate_content" (and the "generatecontent"/"legacy" spellings) or
// "interactions". An unrecognized value is ignored with a warning rather than
// forwarded, since guessing here silently changes which API a caller reaches.
func configuredAPIMode(additionalConfig map[string]any) APIMode {
	if additionalConfig == nil {
		return apiModeUnset
	}
	raw, ok := additionalConfig["api_mode"].(string)
	if !ok {
		return apiModeUnset
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "generate_content", "generatecontent", "legacy":
		return APIModeGenerateContent
	case "interactions":
		return APIModeInteractions
	case "":
		return apiModeUnset
	default:
		logger.Warn("gemini: ignoring unrecognized api_mode",
			"value", raw, "accepted", "generate_content|interactions")
		return apiModeUnset
	}
}

// resolveAPIMode decides where a single request goes.
//
// Priority:
//  1. Explicit config — always wins, so an operator can pin either API.
//  2. Capability fallback — a response schema alongside tools ONLY works on
//     interactions. Routing there beats the alternative, which is silently
//     dropping the caller's schema and answering in prose.
//  3. generateContent.
//
// The fallback is per-request rather than per-provider because whether a round
// carries tools is a property of the round, not of the configuration.
func (p *Provider) resolveAPIMode(rf *providers.ResponseFormat, hasTools bool) APIMode {
	if p.apiMode != apiModeUnset {
		return p.apiMode
	}
	if hasTools && wantsSchema(rf) && honorsInteractionsSchema(p.model) {
		return APIModeInteractions
	}
	return APIModeGenerateContent
}

// honorsInteractionsSchema reports whether a model actually applies
// response_format on the Interactions API.
//
// Gemini 3 does. Gemini 2.5 accepts the field and IGNORES it — verified live
// with no tools involved, so it is not a tool interaction: asked to report a
// city and temperature under a schema it answered "City: Bristol, Celsius: 21".
// Routing 2.5 there would therefore gain nothing and change its behavior for
// no benefit.
//
// This is a DENYLIST of generations known not to honor it, not an allowlist of
// those that do. An unrecognized model is assumed capable, so a new release
// gains the behavior automatically and only a regression needs an entry —
// where an allowlist would silently withhold the feature from every future
// model until someone remembered to add it.
func honorsInteractionsSchema(model string) bool {
	for _, family := range []string{"gemini-1.", "gemini-2."} {
		if strings.HasPrefix(model, family) {
			return false
		}
	}
	return true
}

// applyAPIModeConfig reads additional_config.api_mode onto the provider.
//
// Wired from BOTH factory constructors. A config-reached feature that is only
// applied on one of them is unreachable for half of callers while every
// constructor test still passes — the failure mode that has bitten this repo
// repeatedly.
func applyAPIModeConfig(p *Provider, spec providers.ProviderSpec) {
	p.apiMode = configuredAPIMode(spec.AdditionalConfig)
}

package gemini

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// APIMode selects which Gemini API a request is sent to.
//
// Only configuration chooses it. Neither API can apply a response schema to a
// round that may call tools — on either one the model keeps calling the tool
// and never answers — so a schema alongside tools is handled the same way on
// both (see predictSchemaAfterTools) rather than by switching API (#2771).
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

// resolveAPIMode decides where a request goes: the configured API, else
// generateContent. It applies to every turn, with tools or without
// (ToolProvider.Predict / PredictStream and the tool entry points).
//
// There is no automatic choice. One used to send a schema alongside tools to
// the Interactions API, on the premise that it applies the schema only to the
// answering turn; measured live on gemini-3.7-flash it does not, and the model
// called the tool on every round (#2771).
func (p *Provider) resolveAPIMode() APIMode {
	if p.apiMode != apiModeUnset {
		return p.apiMode
	}
	return APIModeGenerateContent
}

// applyAPIModeConfig reads additional_config.api_mode onto the provider. It
// runs after applyExplicitCachingConfig, and refuses explicit caching with
// the Interactions API, which has no explicit context caching (it answers a
// cached_content field with HTTP 400 "Unknown parameter"): every turn goes to
// that API, so the cache would never be used and each turn would pay the full
// input price the config meant to cache.
//
// Wired from BOTH factory constructors. A config-reached feature that is only
// applied on one of them is unreachable for half of callers while every
// constructor test still passes — the failure mode that has bitten this repo
// repeatedly.
func applyAPIModeConfig(p *Provider, spec providers.ProviderSpec) error {
	p.apiMode = configuredAPIMode(spec.AdditionalConfig)
	if p.apiMode == APIModeInteractions && p.cache != nil {
		return fmt.Errorf("gemini provider %q: additional_config.explicit_caching is not supported "+
			"with api_mode interactions: the Interactions API has no explicit context caching; "+
			"remove explicit_caching or use api_mode generate_content", spec.ID)
	}
	if p.apiMode == APIModeInteractions && p.isVertex() && !vertexModelsBaseURL.MatchString(p.baseURL) {
		return fmt.Errorf("gemini provider %q: api_mode interactions on Vertex needs a base URL ending in "+
			"/projects/{project}/locations/{location}/publishers/google/models, "+
			"from which the Interactions endpoint is derived; got %q", spec.ID, p.baseURL)
	}
	return nil
}

package base

import "github.com/AltairaLabs/PromptKit/runtime/v2/logger"

// ContextWindowConfigKey is the provider spec additional_config key that sets a
// model's context window in tokens, overriding the built-in table. It is how a
// self-hosted model (vLLM, Ollama) or a model newer than the table declares its
// window.
const ContextWindowConfigKey = "context_window"

// ContextWindowFromConfig reads the context_window override from a provider
// spec's additional_config. It returns 0 when the key is absent or not a
// positive number (YAML decodes integers as int, JSON as float64).
func ContextWindowFromConfig(additional map[string]any) int {
	var n int
	switch v := additional[ContextWindowConfigKey].(type) {
	case int:
		n = v
	case int64:
		n = int(v)
	case float64:
		n = int(v)
	}
	if n < 0 {
		return 0
	}
	return n
}

// ResolveContextWindow selects a model's context window in tokens, in order:
// a positive spec override > the provider's built-in table, keyed like the
// pricing table (a vendor prefix such as "anthropic/" is stripped; there is no
// substring fallback). It returns 0 when neither knows the model, which tells
// the caller to use its own default; that case is logged at debug level.
func ResolveContextWindow(providerID, model string, override int, table map[string]int) int {
	if override > 0 {
		return override
	}
	if n, ok := table[normalizeModel(model)]; ok && n > 0 {
		return n
	}
	logger.Debug("context window unknown for model; caller falls back to its default",
		"provider", providerID, "model", model)
	return 0
}

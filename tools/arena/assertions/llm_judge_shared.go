package assertions

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// coerceJudgeTargets normalizes metadata targets into a typed map.
func coerceJudgeTargets(raw interface{}) map[string]providers.ProviderSpec {
	switch t := raw.(type) {
	case map[string]providers.ProviderSpec:
		return t
	case map[string]interface{}:
		out := make(map[string]providers.ProviderSpec, len(t))
		for k, v := range t {
			if spec, ok := v.(providers.ProviderSpec); ok {
				out[k] = spec
			}
		}
		return out
	default:
		return nil
	}
}

// selectJudgeFromTargets picks a judge by name or returns the first available.
func selectJudgeFromTargets(targets map[string]providers.ProviderSpec, name string) (providers.ProviderSpec, error) {
	if len(targets) == 0 {
		return providers.ProviderSpec{}, fmt.Errorf("no judge targets available")
	}

	if name != "" {
		if spec, ok := targets[name]; ok {
			return spec, nil
		}
		return providers.ProviderSpec{}, fmt.Errorf("judge %s not found", name)
	}

	for _, spec := range targets {
		return spec, nil
	}
	return providers.ProviderSpec{}, fmt.Errorf("no judge targets available")
}

// cloneParamsWithMetadata clones params and injects metadata extras for validators that expect _metadata.
func cloneParamsWithMetadata(params map[string]interface{}, convCtx *ConversationContext) map[string]interface{} {
	out := make(map[string]interface{}, len(params)+1)
	for k, v := range params {
		out[k] = v
	}
	if convCtx != nil && convCtx.Metadata.Extras != nil {
		out["_metadata"] = convCtx.Metadata.Extras
	}
	return out
}

// getPromptRegistryFromMeta fetches a prompt registry pointer if present.
func getPromptRegistryFromMeta(meta map[string]interface{}) *prompt.Registry {
	if reg, ok := meta["prompt_registry"].(*prompt.Registry); ok {
		return reg
	}
	return nil
}

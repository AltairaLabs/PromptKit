package assertions

import (
	"fmt"

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

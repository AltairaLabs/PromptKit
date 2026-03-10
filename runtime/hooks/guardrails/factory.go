// Package guardrails provides built-in ProviderHook implementations that
// bridge the unified eval system to the pipeline's hook infrastructure.
package guardrails

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// NewGuardrailHookFromRegistry creates a guardrail ProviderHook using the eval registry.
// Any registered eval handler (including aliases) can be used as a guardrail.
func NewGuardrailHookFromRegistry(
	typeName string, params map[string]any, registry *evals.EvalTypeRegistry,
) (hooks.ProviderHook, error) {
	handler, err := registry.Get(typeName)
	if err != nil {
		return nil, fmt.Errorf("unknown guardrail type: %q", typeName)
	}

	direction := directionOutput
	if d, ok := params["direction"].(string); ok {
		direction = d
	}

	return &GuardrailHookAdapter{
		handler:   handler,
		evalType:  typeName,
		params:    params,
		direction: direction,
	}, nil
}

// NewGuardrailHook creates a guardrail ProviderHook using the default eval registry.
func NewGuardrailHook(typeName string, params map[string]any) (hooks.ProviderHook, error) {
	return NewGuardrailHookFromRegistry(typeName, params, evals.NewEvalTypeRegistry())
}

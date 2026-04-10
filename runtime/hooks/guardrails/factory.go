// Package guardrails provides built-in ProviderHook implementations that
// bridge the unified eval system to the pipeline's hook infrastructure.
package guardrails

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// GuardrailOption configures a GuardrailHookAdapter.
type GuardrailOption func(*GuardrailHookAdapter)

// WithMessage sets the user-facing message shown when content is blocked.
func WithMessage(msg string) GuardrailOption {
	return func(a *GuardrailHookAdapter) { a.message = msg }
}

// WithMonitorOnly disables enforcement — the guardrail evaluates and records
// results but does not modify content. Useful for monitoring guardrails
// without affecting output.
func WithMonitorOnly() GuardrailOption {
	return func(a *GuardrailHookAdapter) { a.monitorOnly = true }
}

// NewGuardrailHookFromRegistry creates a guardrail ProviderHook using the eval registry.
// Any registered eval handler (including aliases) can be used as a guardrail.
//
// If the handler implements evals.ParamValidator, the params are normalised
// (ApplyDefaults + NormalizeParams) and passed to ValidateParams before the
// hook is constructed. This surfaces invalid pack validators at SDK load
// time instead of silently failing every turn — handlers for which params
// are unusable return an error here, and the SDK's warn-and-skip loop in
// convertPackValidatorsToHooks logs and drops them.
func NewGuardrailHookFromRegistry(
	typeName string, params map[string]any, registry *evals.EvalTypeRegistry,
	opts ...GuardrailOption,
) (hooks.ProviderHook, error) {
	handler, err := registry.Get(typeName)
	if err != nil {
		return nil, fmt.Errorf("unknown guardrail type: %q", typeName)
	}

	// Normalise params the same way adapter.AfterCall does before passing
	// to ValidateParams, so handlers only need to check canonical key names.
	normalized := evals.ApplyDefaults(typeName, params)
	normalized = evals.NormalizeParams(typeName, normalized)

	if pv, ok := handler.(evals.ParamValidator); ok {
		if verr := pv.ValidateParams(normalized); verr != nil {
			return nil, fmt.Errorf("guardrail %q: %w", typeName, verr)
		}
	}

	direction := directionOutput
	if d, ok := params["direction"].(string); ok {
		direction = d
	}

	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  typeName,
		params:    params,
		direction: direction,
	}
	for _, opt := range opts {
		opt(adapter)
	}
	return adapter, nil
}

// NewGuardrailHook creates a guardrail ProviderHook using the default eval registry.
func NewGuardrailHook(typeName string, params map[string]any, opts ...GuardrailOption) (hooks.ProviderHook, error) {
	return NewGuardrailHookFromRegistry(typeName, params, evals.NewEvalTypeRegistry(), opts...)
}

// Package guardrails provides built-in ProviderHook implementations that
// bridge the unified eval system to the pipeline's hook infrastructure.
package guardrails

import (
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// ErrUnknownGuardrailType is returned when a validator names an eval type that
// is not registered. It is distinguishable from other construction failures
// because it is treated as fatal: an unknown type has no legitimate use, so
// dropping it would leave a conversation silently unprotected. See
// CompileValidators.
var ErrUnknownGuardrailType = errors.New("unknown guardrail type")

// GuardrailOption configures a GuardrailHookAdapter.
type GuardrailOption func(*GuardrailHookAdapter)

// WithMessage sets the user-facing message shown when content is blocked.
func WithMessage(msg string) GuardrailOption {
	return func(a *GuardrailHookAdapter) { a.message = msg }
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
		return nil, fmt.Errorf("%w: %q", ErrUnknownGuardrailType, typeName)
	}

	// Normalise params the same way adapter.AfterCall does before passing
	// to ValidateParams, so handlers only need to check canonical key names.
	// Threshold keys are stripped for the same reason the adapter strips them:
	// they belong to this wrapper, and a handler asked to validate one rejects
	// it, which would fail construction outright (#1707).
	normalized := evals.ApplyDefaults(typeName, params)
	normalized = evals.NormalizeParams(typeName, normalized)
	normalized = evals.StripScoreThresholds(normalized)

	if pv, ok := handler.(evals.ParamValidator); ok {
		if verr := pv.ValidateParams(normalized); verr != nil {
			return nil, fmt.Errorf("guardrail %q: %w", typeName, verr)
		}
	}

	direction := DirectionOutput
	if raw, present := params["direction"]; present {
		d, ok := raw.(string)
		if !ok {
			logger.Warn(
				"Guardrail direction must be a string; falling back to output",
				"type", typeName, "direction", raw)
		} else {
			switch d {
			case DirectionInput, DirectionOutput, DirectionBoth:
				direction = d
			default:
				logger.Warn(
					"Guardrail has unrecognized direction; falling back to output",
					"type", typeName, "direction", d,
					"want", fmt.Sprintf("%q, %q or %q", DirectionInput, DirectionOutput, DirectionBoth))
			}
		}
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

// registryOrDefault resolves an optional caller-supplied registry. nil means
// "the caller expressed no preference", which is the default registry of
// built-in handlers — the behavior every caller had before registries could be
// threaded through. A caller that *does* supply one gets exactly that registry,
// so a custom eval type can back a guardrail (#1717).
func registryOrDefault(registry *evals.EvalTypeRegistry) *evals.EvalTypeRegistry {
	if registry != nil {
		return registry
	}
	return evals.NewEvalTypeRegistry()
}

// CompileValidators turns pack-declared validators into ProviderHooks suitable
// for prepending to a hook registry. Both SDK.Open and Arena's per-turn
// pipeline use this so guardrails run identically in production and in tests.
//
// Per-validator behavior:
//   - Validators with Enabled == &false are skipped silently (explicit
//     opt-out). nil Enabled means enabled (spec default).
//   - All accepted validators enforce: on a hit they rewrite the assistant
//     message in place (truncate or replace). If you want observe-only
//     behavior, declare an eval and assert on it; guardrails always act.
//   - "message" set on the validator becomes the user-facing blocked text,
//     falling back to Params["message"].
//
// Failure policy, split by how ambiguous the mistake is:
//   - An **unknown eval type** is FATAL and returns ErrUnknownGuardrailType.
//     A type that is not registered has no legitimate use — it is a typo — and
//     silently dropping it leaves the conversation with no protection while
//     load appears to succeed. That is fail-open on a safety control.
//   - A validator whose **params** are unusable is logged and skipped, so one
//     bad entry does not break the others. A pack authored against a newer
//     runtime can legitimately carry params this build does not understand;
//     refusing to load would make packs forward-incompatible.
//
// On a fatal error no hooks are returned, so a caller cannot accidentally
// proceed with a partial guardrail set.
func CompileValidators(validators []prompt.ValidatorConfig) ([]hooks.ProviderHook, error) {
	return compileValidators(validators, true, nil)
}

// CompileValidatorsWithRegistry is CompileValidators resolving each validator's
// eval type against the supplied registry instead of the built-in default. Pass
// the registry a caller configured (sdk.WithEvalRegistry) so a custom handler
// can back a pack validator; a nil registry means the default one.
//
// Without this the default registry does not know a custom type, construction
// fails, and — on the lenient path — the guardrail is logged and dropped, which
// leaves the conversation unprotected while load appears to succeed (#1717).
func CompileValidatorsWithRegistry(
	validators []prompt.ValidatorConfig, registry *evals.EvalTypeRegistry,
) ([]hooks.ProviderHook, error) {
	return compileValidators(validators, true, registry)
}

// ValidatorsToHooks is the lenient form: every unusable validator — including
// an unknown eval type — is logged and skipped, and the usable ones are still
// returned.
//
// Deprecated: use CompileValidators. This form cannot report an unknown eval
// type, so a typo'd validator is silently dropped and the caller proceeds
// unprotected. Retained unchanged so existing callers keep their behavior.
func ValidatorsToHooks(validators []prompt.ValidatorConfig) []hooks.ProviderHook {
	// The lenient path never returns an error.
	out, _ := compileValidators(validators, false, nil)
	return out
}

// ValidatorsToHooksWithRegistry is the lenient form of
// CompileValidatorsWithRegistry: every unusable validator — including an
// unknown eval type — is logged and skipped. A nil registry means the default
// one.
//
// Deprecated: use CompileValidatorsWithRegistry. This form cannot report an
// unknown eval type, so a typo'd validator is silently dropped and the caller
// proceeds unprotected.
func ValidatorsToHooksWithRegistry(
	validators []prompt.ValidatorConfig, registry *evals.EvalTypeRegistry,
) []hooks.ProviderHook {
	out, _ := compileValidators(validators, false, registry)
	return out
}

// compileValidators builds hooks from validator specs. When fatalUnknownType is
// true an unregistered eval type aborts the whole set (no partial guardrails);
// when false it is logged and skipped like any other unusable entry. registry
// resolves the eval types; nil selects the default registry.
func compileValidators(
	validators []prompt.ValidatorConfig, fatalUnknownType bool, registry *evals.EvalTypeRegistry,
) ([]hooks.ProviderHook, error) {
	if len(validators) == 0 {
		return nil, nil
	}
	// Resolved once: every validator in a set must see the same handlers, and
	// the default constructor is not free.
	reg := registryOrDefault(registry)
	out := make([]hooks.ProviderHook, 0, len(validators))
	for _, v := range validators {
		if v.Enabled != nil && !*v.Enabled {
			logger.Debug("Skipping disabled pack validator", "type", v.Type)
			continue
		}

		var opts []GuardrailOption
		if v.Message != "" {
			opts = append(opts, WithMessage(v.Message))
		} else if msg, ok := v.Params["message"].(string); ok && msg != "" {
			opts = append(opts, WithMessage(msg))
		}

		hook, err := NewGuardrailHookFromRegistry(v.Type, v.Params, reg, opts...)
		if err != nil {
			if fatalUnknownType && errors.Is(err, ErrUnknownGuardrailType) {
				return nil, fmt.Errorf("pack validator: %w", err)
			}
			logger.Warn("Skipping unusable pack validator", "type", v.Type, "error", err)
			continue
		}
		out = append(out, hook)
	}
	return out, nil
}

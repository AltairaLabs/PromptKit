// Package guardrails provides built-in ProviderHook implementations that
// bridge the unified eval system to the pipeline's hook infrastructure.
package guardrails

import (
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// ErrUnknownGuardrailType is returned when a validator names an eval type that
// is not registered. It is distinguishable from other construction failures
// because it is treated as fatal: an unknown type has no legitimate use, so
// dropping it would leave a conversation silently unprotected. See
// CompileValidators.
var ErrUnknownGuardrailType = errors.New("unknown guardrail type")

// ErrInvalidGuardrailParams is returned when the eval type IS registered and
// its own evals.ParamValidator rejects the params. Like ErrUnknownGuardrailType
// it is fatal on the strict path: the handler is the authority on its own
// config, so its rejection is a statement that this declaration cannot work —
// not a param a newer runtime might understand. See CompileValidators.
var ErrInvalidGuardrailParams = errors.New("invalid guardrail params")

// invalidParams tags a handler's ValidateParams rejection with
// ErrInvalidGuardrailParams without altering the message. The message shape
// matters: sdk.ValidatePack strips a fixed `guardrail "<type>": ` prefix off it
// to recover the handler's own text, so the sentinel is attached through a
// second Unwrap branch rather than by inserting another clause.
type invalidParams struct{ err error }

func (e *invalidParams) Error() string   { return e.err.Error() }
func (e *invalidParams) Unwrap() []error { return []error{e.err, ErrInvalidGuardrailParams} }

// GuardrailOption configures a GuardrailHookAdapter.
type GuardrailOption func(*GuardrailHookAdapter)

// WithMessage sets the user-facing message shown when content is blocked.
func WithMessage(msg string) GuardrailOption {
	return func(a *GuardrailHookAdapter) { a.message = msg }
}

// WithEmitter gives the guardrail an event emitter so it reports its validation
// lifecycle — started, and passed when it does not trigger. Firings are emitted
// by the pipeline stage, which knows the enforcement outcome and also covers
// func-backed guardrails; see GuardrailHookAdapter.evaluate.
//
// Optional: a guardrail built without an emitter is silent, as before.
func WithEmitter(emitter *events.Emitter) GuardrailOption {
	return func(a *GuardrailHookAdapter) { a.emitter = emitter }
}

// NewGuardrailHookFromRegistry creates a guardrail ProviderHook using the eval registry.
// Any registered eval handler (including aliases) can be used as a guardrail.
//
// If the handler implements evals.ParamValidator, the params are normalised
// (ApplyDefaults + NormalizeParams) and passed to ValidateParams before the
// hook is constructed. This surfaces invalid pack validators at SDK load time
// instead of silently failing every turn: a rejection is returned wrapped in
// ErrInvalidGuardrailParams, which the strict compile path (and therefore
// sdk.Open) treats as fatal.
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
			return nil, fmt.Errorf("guardrail %q: %w", typeName, &invalidParams{verr})
		}
	}

	direction := DirectionOutput
	// normalized, not params: ApplyDefaults ran above, and an eval type that
	// declares a direction default in evals.ParamDefaults must get it. This used
	// to read the caller's raw map, so a default declared in ParamDefaults was
	// invisible here and the factory fell through to DirectionOutput — a check
	// meant to gate input would silently only inspect the assistant's reply,
	// never blocking the call it exists to prevent. topic_policy is the first
	// eval type to declare a direction default, which is what exposed it; no
	// pre-existing type sets one, so nothing else changed behavior.
	if raw, present := normalized["direction"]; present {
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
// Failure policy — both shapes of "this declaration cannot work" are FATAL,
// for the same reason:
//   - An **unknown eval type** returns ErrUnknownGuardrailType. A type that is
//     not registered has no legitimate use — it is a typo — and silently
//     dropping it leaves the conversation with no protection while load appears
//     to succeed. That is fail-open on a safety control.
//   - A registered type whose own evals.ParamValidator **rejects the params**
//     returns ErrInvalidGuardrailParams. The handler is the authority on its own
//     config; its rejection says this validator cannot run, and dropping it
//     produces exactly the same silently unprotected conversation. This path
//     used to log and skip on a forward-compatibility argument — that a pack
//     authored against a newer runtime may carry params this build does not
//     understand. It does not apply: a build that does not know the type at all
//     is already fatal, and a build that does know it has the handler's own
//     verdict. Forward-compatibility loses to an unprotected conversation.
//
// Params that no handler ever inspects are unaffected — only a handler that
// implements evals.ParamValidator can reject anything here, and a type with no
// validator accepts whatever it is given, exactly as before.
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
//
// Failure policy is CompileValidators's: an unknown type and a handler-rejected
// param set are both fatal.
func CompileValidatorsWithRegistry(
	validators []prompt.ValidatorConfig, registry *evals.EvalTypeRegistry,
) ([]hooks.ProviderHook, error) {
	return compileValidators(validators, true, registry)
}

// ValidatorsToHooks is the lenient form: every unusable validator — an unknown
// eval type or a param set the handler rejects — is logged and skipped, and the
// usable ones are still returned.
//
// Deprecated: use CompileValidators. This form cannot report either failure, so
// a typo'd validator is silently dropped and the caller proceeds unprotected.
// Retained unchanged so existing callers keep their behavior.
func ValidatorsToHooks(validators []prompt.ValidatorConfig) []hooks.ProviderHook {
	// The lenient path never returns an error.
	out, _ := compileValidators(validators, false, nil)
	return out
}

// ValidatorsToHooksWithRegistry is the lenient form of
// CompileValidatorsWithRegistry: every unusable validator — an unknown eval
// type or a param set the handler rejects — is logged and skipped. A nil
// registry means the default one.
//
// Deprecated: use CompileValidatorsWithRegistry. This form cannot report either
// failure, so a typo'd validator is silently dropped and the caller proceeds
// unprotected.
func ValidatorsToHooksWithRegistry(
	validators []prompt.ValidatorConfig, registry *evals.EvalTypeRegistry,
) []hooks.ProviderHook {
	out, _ := compileValidators(validators, false, registry)
	return out
}

// compileValidators builds hooks from validator specs. When strict is true a
// validator that cannot be constructed — an unregistered eval type, or params
// its own handler rejects — aborts the whole set (no partial guardrails); when
// false it is logged and skipped. registry resolves the eval types; nil selects
// the default registry.
func compileValidators(
	validators []prompt.ValidatorConfig, strict bool, registry *evals.EvalTypeRegistry,
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
			if strict && (errors.Is(err, ErrUnknownGuardrailType) || errors.Is(err, ErrInvalidGuardrailParams)) {
				return nil, fmt.Errorf("pack validator: %w", err)
			}
			logger.Warn("Skipping unusable pack validator", "type", v.Type, "error", err)
			continue
		}
		out = append(out, hook)
	}
	return out, nil
}

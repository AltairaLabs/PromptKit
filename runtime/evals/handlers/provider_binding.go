package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
)

// Resolving a check's ancillary provider, in one place for every
// inference-backed family.
//
// A check names a LOGICAL key its own pack declared in `requires`; the host
// binds that key to something concrete and may rebind it freely. Naming nothing
// is also legitimate — it means "whatever the host made the default for this
// task", which is a host decision rather than a pack one. What is never
// legitimate is this package picking a name, or a pack naming a host-side id.

// resolveInference resolves the inference provider a check should use.
//
// A named key goes through the host's binding; no key means the host's default
// inference provider, which the host set, so following it breaks no rule. want
// names what the check needs ("text classifier") for error messages: a host
// that bound nothing, or something that is not an inference provider, is told
// which key and that the binding is the thing to change — rather than an
// absence, which reads as "not configured" and sends them looking elsewhere.
func resolveInference(ctx context.Context, key, want string) (inference.Provider, error) {
	if key == "" {
		reg := inference.FromContext(ctx)
		if reg == nil {
			return nil, fmt.Errorf(
				"no %s configured: the check names no provider and the host set no default. "+
					"Either name one with %q — a key the pack declares in requires — or configure a default",
				want, ProviderParam)
		}
		return reg.Get("")
	}

	binding := evals.BindingFromContext(ctx)
	if binding == nil {
		return nil, fmt.Errorf("%s: %s", want, evals.DescribeUnresolved(key, evals.ErrNoBinding))
	}
	provider, err := binding.Inference(key)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", want, evals.DescribeUnresolved(key, err))
	}
	return provider, nil
}

// providerResult turns a resolution failure into the right KIND of result.
//
// A check that NAMED a provider and could not get it is misconfigured, and
// misconfiguration is an Error: Skipped scores 1.0 and passes, so a safety
// control that silently did not run would report as clean — the #1996 failure,
// one level up. A check that named nothing and found no host default is the
// older "infrastructure absent" case, which stays Skipped so a pack that never
// asked for a classifier does not start failing.
func providerResult(handlerType, key string, err error, skipped, errored resultFn) *evals.EvalResult {
	if key != "" {
		return errored(handlerType, err.Error())
	}
	return skipped(handlerType, err.Error())
}

// resultFn builds an EvalResult of one kind (skippedResult, errorResult).
type resultFn func(handlerType, reason string) *evals.EvalResult

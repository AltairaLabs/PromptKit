package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// Resolving a check's ancillary provider, in one place for every classify-backed
// family.
//
// A check names a LOGICAL key its own pack declared in `requires`; the host
// binds that key to something concrete and may rebind it freely. Naming nothing
// is also legitimate — it means "whatever the host made the default for this
// task", which is a host decision rather than a pack one. What is never
// legitimate is this package picking a name, or a pack naming a host-side id.

// classifierFor resolves the classify backend a check should use and asserts
// that it can do the task asked of it.
//
// want names the task for the error message ("text classifier"), and assert
// narrows the backend to the interface the caller needs. A host that bound
// something unsuited to the key gets told which key, what the check needed and
// that the binding is the thing to change — rather than an absence, which reads
// as "not configured" and sends them looking in the wrong place.
func classifierFor[T any](
	ctx context.Context, key, want string, assert func(classify.Backend) (T, bool),
) (T, error) {
	var zero T

	if key == "" {
		return zero, fmt.Errorf("no %s named; add %q to the check's params, "+
			"naming a provider the pack declares in requires", want, ProviderParam)
	}

	binding := evals.BindingFromContext(ctx)
	if binding == nil {
		return zero, fmt.Errorf("%s: %s", want, evals.DescribeUnresolved(key, evals.ErrNoBinding))
	}

	backend, err := binding.Classifier(key)
	if err != nil {
		return zero, fmt.Errorf("%s: %s", want, evals.DescribeUnresolved(key, err))
	}

	typed, ok := assert(backend)
	if !ok {
		return zero, fmt.Errorf("%s: %s", want, evals.DescribeUnresolved(key,
			fmt.Errorf("%w: the provider bound to it is not a %s", evals.ErrWrongKind, want)))
	}
	return typed, nil
}

// defaultClassifier falls back to the host's configured default for a task,
// used when a check names no provider. The host set that default, so following
// it breaks no rule; the registry is asked directly because a default has no
// logical key to resolve.
func defaultClassifier[T any](
	ctx context.Context, want string, get func(*classify.Registry) (T, error),
) (T, error) {
	var zero T
	reg := classify.FromContext(ctx)
	if reg == nil {
		return zero, fmt.Errorf(
			"no %s configured: the check names no provider and the host set no default. "+
				"Either name one with %q — a key the pack declares in requires — or configure a default",
			want, ProviderParam)
	}
	return get(reg)
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

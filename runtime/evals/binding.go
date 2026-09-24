package evals

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A pack declares the providers it needs as LOGICAL names in its requires
// block — `grader`, `screener`, whatever the author chose — and a check points
// at one of those names. The host binds each name to a concrete provider and
// stays free to change what sits behind it. That indirection is the entire
// point of the requirements block, and it is why nothing in this runtime may
// author a name of its own or resolve one by convention.
//
// ProviderBinding is the seam between the two halves: the pack side asks for a
// logical name, the host side answers with something concrete.

// ErrNoBinding is returned when no host binding is attached at all. It means
// the runtime was driven by a caller that never wired one up, which is a
// wiring error rather than a pack error.
var ErrNoBinding = errors.New("no provider binding configured")

// ErrUnboundKey is returned when the pack asked for a logical name the host
// bound nothing to.
var ErrUnboundKey = errors.New("no provider bound to this key")

// ErrWrongKind is returned when the host DID bind something to the key, but
// what they bound cannot do what the check needs — an embedding provider bound
// to the name a judge-backed check points at, say. Callers should surface this
// at load time; a host that binds the wrong thing has made a wiring mistake and
// deserves to hear about it before the first conversation, not on the turn that
// happens to need it.
var ErrWrongKind = errors.New("provider bound to this key cannot do what the check needs")

// ProviderBinding resolves a pack's logical provider names against what the
// host bound to them.
//
// Implementations live with the host (the SDK, Arena), because only the host
// knows what it has. Each method answers for one KIND of use, so a mismatch is
// reported as a mismatch — "you bound an embedder to the name a judge check
// uses" — rather than as an absence, which is what makes the failure legible.
type ProviderBinding interface {
	// LLM returns a provider that can run completions, for the logical key —
	// what a judge-backed check needs. ErrWrongKind when the host bound
	// something that is not one.
	LLM(key string) (providers.Provider, error)

	// Inference returns the inference provider bound to the logical key —
	// what a classifier-backed check needs. ErrWrongKind when the host bound
	// something that is not one.
	Inference(key string) (inference.Provider, error)
}

type bindingContextKey struct{}

// WithProviderBinding attaches a host's binding to ctx. Mirrors
// inference.WithRegistry: the pipeline attaches it once and every stage and
// handler below reads it from the context it was given.
func WithProviderBinding(ctx context.Context, b ProviderBinding) context.Context {
	if b == nil {
		return ctx
	}
	return context.WithValue(ctx, bindingContextKey{}, b)
}

// BindingFromContext returns the binding attached to ctx, or nil when none is.
func BindingFromContext(ctx context.Context) ProviderBinding {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(bindingContextKey{}).(ProviderBinding)
	return b
}

// DescribeUnresolved renders why a logical name could not be resolved, in terms
// the person who has to fix it can act on: which name the pack used, and
// whether the fix is in the pack or in the host's wiring.
func DescribeUnresolved(key string, err error) string {
	switch {
	case errors.Is(err, ErrNoBinding):
		return fmt.Sprintf(
			"%q: the runtime has no provider binding, so no logical provider name can be resolved", key)
	case errors.Is(err, ErrUnboundKey):
		return fmt.Sprintf(
			"%q: the pack asks for a provider under this name and the host bound none; "+
				"declare it in the pack's requires block and supply a provider for it", key)
	case errors.Is(err, ErrWrongKind):
		return fmt.Sprintf("%q: %v", key, err)
	default:
		return fmt.Sprintf("%q: %v", key, err)
	}
}

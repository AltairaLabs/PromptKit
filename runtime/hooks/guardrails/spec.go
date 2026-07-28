package guardrails

import (
	"context"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// ErrEmptySpec is returned by Spec.Hook for a zero-value Spec — one that never
// went through Input, Output, InputFunc or OutputFunc. Reachable from a
// pre-sized slice (make([]guardrails.Spec, n)) whose entries a branch failed to
// assign; returning an error keeps that config mistake out of the panic path.
var ErrEmptySpec = errors.New(
	"uninitialized Spec — build it with Input, Output, InputFunc or OutputFunc")

// Spec is a declared guardrail, not yet built into a hook. Construction errors
// (unknown eval type, invalid params) surface from Hook() so callers can
// declare guardrails inline and report all failures at one point — typically
// sdk.Open.
type Spec struct {
	build func(*evals.EvalTypeRegistry) (hooks.ProviderHook, error)
}

// Hook builds the ProviderHook this Spec describes against the default eval
// registry. A zero-value Spec returns ErrEmptySpec rather than panicking.
func (s Spec) Hook() (hooks.ProviderHook, error) {
	return s.HookWithRegistry(nil)
}

// HookWithRegistry builds the ProviderHook resolving an eval-backed guardrail's
// type against registry. A nil registry means the default one, so
// HookWithRegistry(nil) and Hook() are equivalent.
//
// Callers that let a user supply their own evals.EvalTypeRegistry — the SDK's
// WithEvalRegistry — must use this form. Building against the default registry
// makes a custom eval type unknown, and the guardrail is then dropped rather
// than enforced (#1717). Func-backed Specs (InputFunc, OutputFunc) ignore the
// registry: they carry their own logic.
func (s Spec) HookWithRegistry(registry *evals.EvalTypeRegistry) (hooks.ProviderHook, error) {
	if s.build == nil {
		return nil, ErrEmptySpec
	}
	return s.build(registry)
}

// Input declares an eval-backed guardrail that gates the user's input before
// the provider call. Any registered eval handler may be named.
//
//	guardrails.Input("pii_leakage", nil)
func Input(evalType string, params map[string]any, opts ...GuardrailOption) Spec {
	return evalSpec(evalType, params, DirectionInput, opts...)
}

// Output declares an eval-backed guardrail that gates the assistant's response.
func Output(evalType string, params map[string]any, opts ...GuardrailOption) Spec {
	return evalSpec(evalType, params, DirectionOutput, opts...)
}

// evalSpec builds a Spec for an eval-backed guardrail, forcing the direction
// rather than reading it from params — the constructor already says which it is.
func evalSpec(
	evalType string, params map[string]any, direction string, opts ...GuardrailOption,
) Spec {
	merged := make(map[string]any, len(params)+1)
	for k, v := range params {
		merged[k] = v
	}
	merged["direction"] = direction
	return Spec{build: func(registry *evals.EvalTypeRegistry) (hooks.ProviderHook, error) {
		return NewGuardrailHookFromRegistry(evalType, merged, registryOrDefault(registry), opts...)
	}}
}

// InputFunc declares a guardrail from a plain function gating user input.
// The function runs once per user turn: it is skipped on tool-loop rounds,
// where the last message is a tool result rather than user input.
//
//	guardrails.InputFunc("no-wires", func(ctx context.Context, in *hooks.InputRequest) hooks.Decision {
//	    if strings.Contains(in.UserInput, "wire transfer") {
//	        in.Replacement = "I can't help with transfers."
//	        return hooks.Enforced("wire transfer requested", nil)
//	    }
//	    return hooks.Allow
//	})
func InputFunc(
	name string, fn func(context.Context, *hooks.InputRequest) hooks.Decision,
) Spec {
	return Spec{build: func(*evals.EvalTypeRegistry) (hooks.ProviderHook, error) {
		return &funcGuardrail{name: name, input: fn}, nil
	}}
}

// OutputFunc declares a guardrail from a plain function gating the assistant
// response. Mutate OutputRequest.Message in place and return Enforced to
// rewrite. Returning Enforced also stops the provider round loop and drops any
// tool calls the response requested; downstream pipeline stages still run.
func OutputFunc(
	name string, fn func(context.Context, *hooks.OutputRequest) hooks.Decision,
) Spec {
	return Spec{build: func(*evals.EvalTypeRegistry) (hooks.ProviderHook, error) {
		return &funcGuardrail{name: name, output: fn}, nil
	}}
}

// funcGuardrail adapts a plain function to hooks.ProviderHook so callers do
// not implement the full interface with a no-op counterpart method.
type funcGuardrail struct {
	name   string
	input  func(context.Context, *hooks.InputRequest) hooks.Decision
	output func(context.Context, *hooks.OutputRequest) hooks.Decision
}

var _ hooks.ProviderHook = (*funcGuardrail)(nil)

// Name returns the guardrail's declared name.
func (g *funcGuardrail) Name() string { return g.name }

// BeforeCall runs the input func, if any, when the last message is from the
// user. Tool-loop rounds — where the last message is a tool result — are
// skipped: the gate is lastUserTurn, the same helper
// GuardrailHookAdapter.BeforeCall uses, so the two cannot drift.
func (g *funcGuardrail) BeforeCall(
	ctx context.Context, req *hooks.ProviderRequest,
) hooks.Decision {
	if g.input == nil || req == nil {
		return hooks.Allow
	}
	last, ok := lastUserTurn(req.Messages)
	if !ok {
		return hooks.Allow
	}

	in := &hooks.InputRequest{
		UserInput: last.GetContent(),
		Messages:  req.Messages,
		Round:     req.Round,
	}
	d := g.input(ctx, in)
	if !d.Allow {
		req.Replacement = in.Replacement
		if req.Replacement == "" {
			req.Replacement = prompt.DefaultBlockedMessage
		}
	}
	return d
}

// AfterCall runs the output func, if any, against the completed response.
func (g *funcGuardrail) AfterCall(
	ctx context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	if g.output == nil || resp == nil {
		return hooks.Allow
	}
	return g.output(ctx, &hooks.OutputRequest{
		Content: resp.Message.GetContent(),
		Message: &resp.Message,
		Round:   resp.Round,
	})
}

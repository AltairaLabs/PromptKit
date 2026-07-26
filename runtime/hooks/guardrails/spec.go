package guardrails

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// Spec is a declared guardrail, not yet built into a hook. Construction errors
// (unknown eval type, invalid params) surface from Hook() so callers can
// declare guardrails inline and report all failures at one point — typically
// sdk.Open.
type Spec struct {
	build func() (hooks.ProviderHook, error)
}

// Hook builds the ProviderHook this Spec describes.
func (s Spec) Hook() (hooks.ProviderHook, error) { return s.build() }

// Input declares an eval-backed guardrail that gates the user's input before
// the provider call. Any registered eval handler may be named.
//
//	guardrails.Input("pii_leakage", nil)
func Input(evalType string, params map[string]any, opts ...GuardrailOption) Spec {
	return evalSpec(evalType, params, directionInput, opts...)
}

// Output declares an eval-backed guardrail that gates the assistant's response.
func Output(evalType string, params map[string]any, opts ...GuardrailOption) Spec {
	return evalSpec(evalType, params, directionOutput, opts...)
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
	return Spec{build: func() (hooks.ProviderHook, error) {
		return NewGuardrailHook(evalType, merged, opts...)
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
	return Spec{build: func() (hooks.ProviderHook, error) {
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
	return Spec{build: func() (hooks.ProviderHook, error) {
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
// skipped, matching GuardrailHookAdapter.BeforeCall's gate.
func (g *funcGuardrail) BeforeCall(
	ctx context.Context, req *hooks.ProviderRequest,
) hooks.Decision {
	if g.input == nil || req == nil || len(req.Messages) == 0 {
		return hooks.Allow
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != roleUser {
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

package guardrails_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// alwaysFiresHandler is an eval primitive that always scores 0.0, so the
// guardrail wrapper's implicit min of 1.0 always trips. It exists to make the
// adapter's direction visible from the outside: BeforeCall enforces only when
// the direction is input or both.
type alwaysFiresHandler struct{ evalType string }

func (h *alwaysFiresHandler) Type() string { return h.evalType }

func (h *alwaysFiresHandler) Eval(
	_ context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	zero := 0.0
	return &evals.EvalResult{Type: h.evalType, Score: &zero}, nil
}

// TestNewGuardrailHookFromRegistry_DirectionComesFromParamDefaults pins that a
// direction supplied through evals.ParamDefaults actually reaches the adapter.
// Before the fix the factory read direction from the raw params, so the default
// was ignored and the hook silently ran output-only — BeforeCall returned Allow.
func TestNewGuardrailHookFromRegistry_DirectionComesFromParamDefaults(t *testing.T) {
	const evalType = "direction_default_probe"

	reg := evals.NewEmptyEvalTypeRegistry()
	reg.Register(&alwaysFiresHandler{evalType: evalType})

	evals.ParamDefaults[evalType] = map[string]any{"direction": hooks.DirectionInput}
	t.Cleanup(func() { delete(evals.ParamDefaults, evalType) })

	hook, err := guardrails.NewGuardrailHookFromRegistry(
		evalType, map[string]any{}, reg,
		guardrails.WithMessage("off limits"),
	)
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	}
	d := hook.BeforeCall(context.Background(), req)

	assert.False(t, d.Allow, "input-direction guardrail must not allow a firing turn")
	assert.True(t, d.Enforced, "guardrails enforce rather than error")
	assert.Equal(t, "off limits", req.Replacement)
}

// TestNewGuardrailHookFromRegistry_ExplicitDirectionWinsOverDefault proves the
// fix does not invert precedence: an explicit param still beats the default.
func TestNewGuardrailHookFromRegistry_ExplicitDirectionWinsOverDefault(t *testing.T) {
	const evalType = "direction_override_probe"

	reg := evals.NewEmptyEvalTypeRegistry()
	reg.Register(&alwaysFiresHandler{evalType: evalType})

	evals.ParamDefaults[evalType] = map[string]any{"direction": hooks.DirectionInput}
	t.Cleanup(func() { delete(evals.ParamDefaults, evalType) })

	hook, err := guardrails.NewGuardrailHookFromRegistry(
		evalType, map[string]any{"direction": hooks.DirectionOutput}, reg,
	)
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	}
	assert.True(t, hook.BeforeCall(context.Background(), req).Allow,
		"an explicitly output-only guardrail must not gate input")
}

package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// The score -> triggered decision already existed on evals.GuardrailEvalHandler
// with a configurable min_score. GuardrailHookAdapter carried a second, less
// capable copy hardcoded at `< 1.0` in three places and never read the param,
// so a continuous-score handler could not be tuned in the hook path at all
// (#1707).
//
// Worse, the adapter forwarded its whole param map to the inner handler, and
// several handler families call rejectThresholdParams — which returns an
// errorResult scoring 0. So declaring a threshold made those guardrails block
// every turn with an error, rather than merely ignoring the param.

// paramSpyHandler records the params it was handed and mimics the handler
// families that reject wrapper-level threshold params.
type paramSpyHandler struct {
	typeName string
	score    float64
	seen     map[string]any
}

func (s *paramSpyHandler) Type() string { return s.typeName }

func (s *paramSpyHandler) Eval(
	_ context.Context, _ *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	s.seen = params
	for _, banned := range []string{"min_score", "max_score"} {
		if _, present := params[banned]; present {
			// Mirrors handlers.rejectThresholdParams: score 0, which the
			// adapter reads as "enforce".
			return &evals.EvalResult{
				Type:        s.typeName,
				Score:       floatPtr(0),
				Error:       banned + " is not a valid param on an eval handler",
				Explanation: banned + " is not a valid param on an eval handler",
			}, nil
		}
	}
	return &evals.EvalResult{Type: s.typeName, Score: &s.score}, nil
}

func spyGuardrail(t *testing.T, score float64, params map[string]any) (*GuardrailHookAdapter, *paramSpyHandler) {
	t.Helper()
	spy := &paramSpyHandler{typeName: "spy_eval", score: score}
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(spy)

	h, err := NewGuardrailHookFromRegistry("spy_eval", params, registry)
	require.NoError(t, err)
	adapter, ok := h.(*GuardrailHookAdapter)
	require.True(t, ok)
	return adapter, spy
}

// TestGuardrailThreshold_MinScoreAllowsAboveThreshold is the headline case: a
// score of 0.8 is a pass when min_score is 0.5, even though it is below the
// hardcoded 1.0 the adapter used to demand.
func TestGuardrailThreshold_MinScoreAllowsAboveThreshold(t *testing.T) {
	adapter, _ := spyGuardrail(t, 0.8, map[string]any{"min_score": 0.5})

	resp := &hooks.ProviderResponse{Message: types.Message{Content: "out"}}
	d := adapter.AfterCall(context.Background(), nil, resp)

	assert.True(t, d.Allow,
		"0.8 clears a min_score of 0.5; the adapter must honor the threshold "+
			"rather than demanding a perfect 1.0")
}

// TestGuardrailThreshold_MinScoreEnforcesBelowThreshold is the discriminating
// half: honoring min_score must not become an unconditional allow.
func TestGuardrailThreshold_MinScoreEnforcesBelowThreshold(t *testing.T) {
	adapter, _ := spyGuardrail(t, 0.2, map[string]any{"min_score": 0.5})

	resp := &hooks.ProviderResponse{Message: types.Message{Content: "out"}}
	d := adapter.AfterCall(context.Background(), nil, resp)

	require.False(t, d.Allow, "0.2 is below a min_score of 0.5")
	assert.True(t, d.Enforced, "guardrails enforce rather than deny")
}

// TestGuardrailThreshold_MaxScoreEnforcesAboveThreshold covers max_score, for
// parity with AssertionEvalHandler.applyThresholds. Some handlers score
// "how much of the bad thing is present", where a high score is the failure.
// A perfect 1.0 is used deliberately: under the old hardcoded rule that
// allowed, so this case only distinguishes a real max_score implementation
// from the previous behavior.
func TestGuardrailThreshold_MaxScoreEnforcesAboveThreshold(t *testing.T) {
	adapter, _ := spyGuardrail(t, 1.0, map[string]any{"max_score": 0.3})

	resp := &hooks.ProviderResponse{Message: types.Message{Content: "out"}}
	d := adapter.AfterCall(context.Background(), nil, resp)

	require.False(t, d.Allow, "1.0 exceeds a max_score of 0.3")
	assert.True(t, d.Enforced, "guardrails enforce rather than deny")
}

// TestGuardrailThreshold_DefaultsToPerfectScore pins the existing default: with
// no threshold configured, anything short of 1.0 enforces. This is the behavior
// every currently-declared guardrail relies on and it must not shift.
func TestGuardrailThreshold_DefaultsToPerfectScore(t *testing.T) {
	enforcing, _ := spyGuardrail(t, 0.99, nil)
	resp := &hooks.ProviderResponse{Message: types.Message{Content: "out"}}
	d := enforcing.AfterCall(context.Background(), nil, resp)
	require.False(t, d.Allow, "0.99 with no threshold configured still enforces")

	allowing, _ := spyGuardrail(t, 1.0, nil)
	d = allowing.AfterCall(context.Background(), nil, resp)
	assert.True(t, d.Allow, "a perfect score allows")
}

// TestGuardrailThreshold_NilScoreEnforces pins the fail-closed choice. A
// handler that returns no score could not judge, and a safety mechanism that
// cannot judge must block. Note evals.GuardrailEvalHandler historically did the
// opposite for a nil score; the guardrail role converges on fail-closed.
func TestGuardrailThreshold_NilScoreEnforces(t *testing.T) {
	handler := &stubHandler{
		typeName: "nil_score",
		result:   &evals.EvalResult{Type: "nil_score"},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "nil_score",
		direction: DirectionOutput,
	}

	resp := &hooks.ProviderResponse{Message: types.Message{Content: "out"}}
	d := adapter.AfterCall(context.Background(), nil, resp)

	require.False(t, d.Allow, "a handler that could not score must not be treated as a pass")
	assert.True(t, d.Enforced)
}

// TestGuardrailThreshold_ParamsNeverReachInnerHandler is the trap half of
// #1707. Thresholds are a wrapper concern by design — rejectThresholdParams
// exists to enforce that — so they must be consumed by the adapter and stripped
// before the inner handler sees them. Forwarding them made the handler return
// an error result scoring 0, i.e. block every turn.
func TestGuardrailThreshold_ParamsNeverReachInnerHandler(t *testing.T) {
	adapter, spy := spyGuardrail(t, 1.0, map[string]any{
		"min_score": 0.5,
		"max_score": 1.0,
		"words":     []any{"keepme"},
	})

	resp := &hooks.ProviderResponse{Message: types.Message{Content: "out"}}
	d := adapter.AfterCall(context.Background(), nil, resp)

	require.NotNil(t, spy.seen, "the inner handler must have been called")
	assert.NotContains(t, spy.seen, "min_score",
		"min_score is a wrapper param; forwarding it makes threshold-rejecting "+
			"handlers return an error result that blocks every turn")
	assert.NotContains(t, spy.seen, "max_score", "max_score is a wrapper param")
	assert.Contains(t, spy.seen, "words",
		"non-threshold params must still reach the handler untouched")

	assert.True(t, d.Allow, "score 1.0 clears min_score 0.5 and max_score 1.0")
}

// TestGuardrailThreshold_BeforeCallHonorsThreshold pins that the input
// direction uses the same decision. The adapter had three separate copies of
// the hardcoded comparison; a fix that only touched AfterCall would leave
// input guardrails on the old rule.
func TestGuardrailThreshold_BeforeCallHonorsThreshold(t *testing.T) {
	spy := &paramSpyHandler{typeName: "spy_eval", score: 0.8}
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(spy)

	h, err := NewGuardrailHookFromRegistry("spy_eval", map[string]any{
		"min_score": 0.5,
		"direction": "input",
	}, registry)
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	}
	d := h.BeforeCall(context.Background(), req)

	assert.True(t, d.Allow, "BeforeCall must honor min_score too, not just AfterCall")
}

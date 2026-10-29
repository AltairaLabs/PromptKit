package evals

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selfTimedHandler takes `work` to finish and declares `own` as its bound,
// the way an exec eval declares its binding's timeout_ms.
type selfTimedHandler struct {
	work time.Duration
	own  time.Duration
}

func (h *selfTimedHandler) Type() string { return "self_timed" }

func (h *selfTimedHandler) EvalTimeout(map[string]any) time.Duration { return h.own }

func (h *selfTimedHandler) Eval(ctx context.Context, _ *EvalContext, _ map[string]any) (*EvalResult, error) {
	select {
	case <-time.After(h.work):
		return &EvalResult{Type: h.Type(), Score: float64Ptr(1)}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func runSelfTimed(t *testing.T, h *selfTimedHandler, runnerTimeout time.Duration, def EvalDef) EvalResult {
	t.Helper()
	reg := NewEvalTypeRegistry()
	reg.Register(h)
	runner := NewEvalRunner(reg, WithTimeout(runnerTimeout))
	def.ID, def.Trigger = "e", TriggerOnSessionComplete
	results := runner.RunSessionEvals(context.Background(), []EvalDef{def}, &EvalContext{SessionID: "s"})
	require.Len(t, results, 1)
	return results[0]
}

// A handler's declared bound above the runner timeout is honored; without
// that, a 300s exec eval is silently cut off at the runner's 30s.
func TestRunner_HonoursLongerHandlerTimeout(t *testing.T) {
	h := &selfTimedHandler{work: 150 * time.Millisecond, own: time.Second}
	res := runSelfTimed(t, h, 20*time.Millisecond, EvalDef{Type: "self_timed"})
	assert.Empty(t, res.Error)
	assert.InDelta(t, 1.0, *res.Score, 1e-9)
}

// Divergence case: the same work with no declared bound is cut off by the
// runner timeout, so the test above is not passing by accident.
func TestRunner_RunnerTimeoutStillAppliesWithoutDeclaration(t *testing.T) {
	h := &selfTimedHandler{work: 150 * time.Millisecond}
	res := runSelfTimed(t, h, 20*time.Millisecond, EvalDef{Type: "self_timed"})
	assert.Contains(t, res.Error, "deadline exceeded")
}

func TestRunner_WrappedHandlerTimeoutIsHonored(t *testing.T) {
	h := &selfTimedHandler{work: 150 * time.Millisecond, own: time.Second}
	res := runSelfTimed(t, h, 20*time.Millisecond, EvalDef{
		Type:   WrapperTypeAssertion,
		Params: map[string]any{"eval_type": "self_timed", "min_score": 1.0},
	})
	assert.Empty(t, res.Error)
	require.NotNil(t, res.Passed)
	assert.True(t, *res.Passed)
}

func TestRunner_TimeoutForNeverLowersTheRunnerTimeout(t *testing.T) {
	r := NewEvalRunner(NewEmptyEvalTypeRegistry(), WithTimeout(time.Minute))
	assert.Equal(t, time.Minute, r.timeoutFor(&selfTimedHandler{own: time.Second}, nil))
	assert.Equal(t, time.Minute, r.timeoutFor(&selfTimedHandler{}, nil))
	assert.Equal(t, 2*time.Minute+handlerTimeoutGrace, r.timeoutFor(&selfTimedHandler{own: 2 * time.Minute}, nil))
}

func TestInnerTimeout_UnknownOrMissingInnerIsZero(t *testing.T) {
	reg := NewEvalTypeRegistry()
	assert.Zero(t, innerTimeout(reg, map[string]any{}))
	assert.Zero(t, innerTimeout(reg, map[string]any{"eval_type": "nope"}))
	assert.Zero(t, innerTimeout(nil, map[string]any{"eval_type": "self_timed"}))
	assert.Zero(t, (&GuardrailEvalHandler{registry: reg}).EvalTimeout(map[string]any{"eval_type": WrapperTypeAssertion}))
}

package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// validationTurn reads the turn index off a recorded validation event.
func validationTurn(t *testing.T, sink *eventSink, et events.EventType) int {
	t.Helper()
	evt := sink.first(et)
	require.NotNil(t, evt, "no %s event recorded", et)
	data, ok := evt.Data.(*events.ValidationEventData)
	require.True(t, ok, "%s carried %T, want *ValidationEventData", et, evt.Data)
	return data.TurnIndex
}

// A guardrail firing on turn N must say so on BOTH events of the pair.
//
// Without the turn, a subscriber can see that a guardrail ran but cannot place
// it against the turn it judged — which is the single fact needed to line a
// validation up with anything else recorded for that turn.
func TestGuardrailHookAdapter_AfterCallCarriesTurnIndex(t *testing.T) {
	handler := &stubHandler{typeName: "test_pass", result: &evals.EvalResult{Score: floatPtr(1.0)}}
	adapter, sink := observedAdapter(t, handler, "test_pass")

	d := adapter.AfterCall(context.Background(),
		&hooks.ProviderRequest{
			Messages:  []types.Message{{Role: "user", Content: "hi"}},
			TurnIndex: 3,
		},
		&hooks.ProviderResponse{Message: types.Message{Content: "hello world"}})
	require.True(t, d.Allow)

	sink.awaitCount(t, 2)
	assert.Equal(t, 3, validationTurn(t, sink, events.EventValidationStarted))
	assert.Equal(t, 3, validationTurn(t, sink, events.EventValidationPassed))
}

// The input direction judges the user's message and builds its own eval
// context, so it needs the turn threaded independently of the output path.
func TestGuardrailHookAdapter_BeforeCallCarriesTurnIndex(t *testing.T) {
	handler := &stubHandler{typeName: "test_pass", result: &evals.EvalResult{Score: floatPtr(1.0)}}
	adapter, sink := observedAdapter(t, handler, "test_pass")
	adapter.direction = DirectionInput

	d := adapter.BeforeCall(context.Background(), &hooks.ProviderRequest{
		Messages:  []types.Message{{Role: "user", Content: "hi"}},
		TurnIndex: 7,
	})
	require.True(t, d.Allow)

	sink.awaitCount(t, 2)
	assert.Equal(t, 7, validationTurn(t, sink, events.EventValidationStarted))
	assert.Equal(t, 7, validationTurn(t, sink, events.EventValidationPassed))
}

// The handler must see the turn too. BuildGuardrailEvalContext cannot derive
// TurnIndex from message history, which is why handlers reading it were
// documented as unusable as guardrails; the hook boundary now supplies it.
func TestGuardrailHookAdapter_HandlerSeesTurnIndex(t *testing.T) {
	seen := make(chan int, 1)
	handler := &turnCapturingHandler{seen: seen, result: &evals.EvalResult{Score: floatPtr(1.0)}}
	adapter, _ := observedAdapter(t, handler, "test_turn")

	adapter.AfterCall(context.Background(),
		&hooks.ProviderRequest{
			Messages:  []types.Message{{Role: "user", Content: "hi"}},
			TurnIndex: 5,
		},
		&hooks.ProviderResponse{Message: types.Message{Content: "reply"}})

	require.Equal(t, 5, <-seen)
}

// AfterCall tolerates a nil request (the stage passes one for a response with
// no originating request), and must not panic reaching for a turn index.
func TestGuardrailHookAdapter_NilRequestHasNoTurn(t *testing.T) {
	handler := &stubHandler{typeName: "test_pass", result: &evals.EvalResult{Score: floatPtr(1.0)}}
	adapter, sink := observedAdapter(t, handler, "test_pass")

	d := adapter.AfterCall(context.Background(),
		nil, &hooks.ProviderResponse{Message: types.Message{Content: "hello"}})
	require.True(t, d.Allow)

	sink.awaitCount(t, 2)
	assert.Equal(t, 0, validationTurn(t, sink, events.EventValidationPassed))
}

// turnCapturingHandler reports the TurnIndex it was handed.
type turnCapturingHandler struct {
	seen   chan int
	result *evals.EvalResult
}

func (h *turnCapturingHandler) Type() string { return "test_turn" }

func (h *turnCapturingHandler) Eval(
	_ context.Context, evalCtx *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	h.seen <- evalCtx.TurnIndex
	return h.result, nil
}

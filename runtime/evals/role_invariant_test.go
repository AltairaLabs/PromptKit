package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// The rule these tests hold: an eval MEASURES and never states a pass/fail.
// Only a role that coerces — an assertion or a guardrail — sets Passed.
//
// It was a convention before, spread across an inference on handler-type
// strings in the runner and an overwrite of Value in the assertion wrapper.
// Both misfired. The inference reported any custom wrapper as a plain eval,
// and the overwrite destroyed the inner eval's output so an assertion on an
// llm_judge threw away the judge's reasoning (#1874, #1875).

func runOneHandler(t *testing.T, h EvalTypeHandler) *EvalResult {
	t.Helper()
	runner := NewEvalRunner(nil)
	def := &EvalDef{ID: "probe", Type: h.Type()}
	got := runner.executeHandler(context.Background(), h, def, &EvalContext{})
	require.NotNil(t, got)
	return got
}

// TestExecuteHandler_StripsPassedFromAPlainEval is the structural half. A
// handler CAN set Passed — the field is exported and it compiles — so the
// guarantee has to live somewhere every result passes through.
func TestExecuteHandler_StripsPassedFromAPlainEval(t *testing.T) {
	score := 0.9
	got := runOneHandler(t, &mockHandler{
		typeName: "llm_judge",
		result:   &EvalResult{Score: &score, Passed: boolPtr(false)},
	})

	assert.Nil(t, got.Passed,
		"a plain eval reached a consumer stating a pass/fail. executeHandler is the "+
			"single funnel every handler result crosses; if the strip is removed there, "+
			"nothing else stops a handler from shipping one")
	assert.Equal(t, events.EvalKindEval, got.Kind,
		"an unwrapped handler result is a measurement and should say so")
}

// TestExecuteHandler_NormalizesUnknownKindToEval covers the case the old
// string-matching inference got wrong in the other direction: something that
// is not one of the two known roles must not keep a pass/fail.
func TestExecuteHandler_NormalizesUnknownKindToEval(t *testing.T) {
	got := runOneHandler(t, &mockHandler{
		typeName: "custom_wrapper",
		result:   &EvalResult{Kind: events.EvalKind("rubric"), Passed: boolPtr(true)},
	})

	assert.Equal(t, events.EvalKindEval, got.Kind)
	assert.Nil(t, got.Passed, "an unrecognized role must not carry a pass/fail through")
}

// TestExecuteHandler_KeepsPassedForCoercingRoles is the counterweight. A strip
// that fired on everything would satisfy the two tests above and silently
// break every assertion in the product.
func TestExecuteHandler_KeepsPassedForCoercingRoles(t *testing.T) {
	for _, kind := range []events.EvalKind{events.EvalKindAssertion, events.EvalKindGuardrail} {
		t.Run(string(kind), func(t *testing.T) {
			got := runOneHandler(t, &mockHandler{
				typeName: "wrapper",
				result:   &EvalResult{Kind: kind, Passed: boolPtr(false)},
			})
			assert.Equal(t, kind, got.Kind)
			require.NotNil(t, got.Passed, "%s coerces to a boolean; its result must keep one", kind)
			assert.False(t, *got.Passed)
		})
	}
}

// TestAssertionWrapper_PreservesTheInnerEvalsValue is #1875.
//
// The assertion wrapper used to do `result.Value = passed`. An assertion over an
// llm_judge therefore replaced the judge's structured output with the single
// word "true" — the richest thing the eval produced was destroyed by the act of
// asserting on it, and no consumer could get it back.
func TestAssertionWrapper_PreservesTheInnerEvalsValue(t *testing.T) {
	rubric := map[string]any{"accuracy": 0.9, "tone": "warm"}
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "llm_judge",
		result:   &EvalResult{Score: float64Ptr(0.9), Value: rubric},
	})
	handler, err := reg.Get(WrapperTypeAssertion)
	require.NoError(t, err)

	result, err := handler.Eval(context.Background(), &EvalContext{}, map[string]any{
		"eval_type": "llm_judge",
		"min_score": 0.8,
	})
	require.NoError(t, err)

	assert.Equal(t, rubric, result.Value,
		"the assertion overwrote the inner eval's value with its own boolean")
	assert.Equal(t, events.EvalKindAssertion, result.Kind)
	require.NotNil(t, result.Passed)
	assert.True(t, *result.Passed, "score 0.9 clears min_score 0.8")
}

// TestGuardrailWrapper_StatesPassed is #1874. A guardrail's outcome reached a
// consumer only as Details["triggered"] — a convention every consumer had to
// know, and one the event schema documented nowhere.
func TestGuardrailWrapper_StatesPassed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		score      float64
		wantPassed bool
	}{
		{"clears the threshold", 0.9, true},
		{"fires below the threshold", 0.2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := newWrapperTestRegistry(&mockHandler{
				typeName: "content_excludes",
				result:   &EvalResult{Score: float64Ptr(tc.score)},
			})
			handler, err := reg.Get(WrapperTypeGuardrail)
			require.NoError(t, err)

			result, err := handler.Eval(context.Background(), &EvalContext{}, map[string]any{
				"eval_type": "content_excludes",
				"min_score": 0.8,
			})
			require.NoError(t, err)

			assert.Equal(t, events.EvalKindGuardrail, result.Kind)
			require.NotNil(t, result.Passed, "a guardrail coerces to a boolean; it must state one")
			assert.Equal(t, tc.wantPassed, *result.Passed)

			// Details keeps what existing consumers read. Passed is the
			// inverse of triggered, and the two must not disagree.
			assert.Equal(t, !tc.wantPassed, result.Details["triggered"],
				"Passed and Details[\"triggered\"] disagree about the same guardrail")
			assert.Equal(t, "block", result.Details["action"])
		})
	}
}

// TestEmitResult_CarriesValueAndMetricValue closes the gap that made a live
// consumer's eval events nearly useless for a non-scoring handler.
//
// The event carried Score and Details but not Value or MetricValue, so a
// subscriber watching eval.completed for a rubric or a classifier received a
// result with nothing in it — the measurement stayed behind in the return
// value, reachable only by a caller of sdk.Evaluate.
func TestEmitResult_CarriesValueAndMetricValue(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.Event, 4)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) { received <- e })

	runner := NewEvalRunner(NewEvalTypeRegistry(),
		WithEmitter(events.NewEmitter(bus, "", "", "")))

	metric := 4.0
	rubric := map[string]any{"accuracy": 0.9, "tone": "warm"}
	runner.emitResult(nil, &EvalResult{
		EvalID: "rubric", Type: "llm_judge", Kind: events.EvalKindEval,
		Value: rubric, MetricValue: &metric,
	})

	select {
	case e := <-received:
		data := e.Data.(*events.EvalCompletedData)
		assert.Equal(t, rubric, data.Value, "the eval's own measurement never reached the bus")
		require.NotNil(t, data.MetricValue, "the graphable number never reached the bus")
		assert.InDelta(t, 4.0, *data.MetricValue, 0.0001)
		assert.False(t, data.ValueOmitted)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

// TestEmitResult_BoundsAnOversizedValue proves the bound is applied on the way
// to the bus, not merely available as a method nobody calls.
func TestEmitResult_BoundsAnOversizedValue(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.Event, 4)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) { received <- e })

	runner := NewEvalRunner(NewEvalTypeRegistry(),
		WithEmitter(events.NewEmitter(bus, "", "", "")))

	runner.emitResult(nil, &EvalResult{
		EvalID: "huge", Type: "llm_judge", Kind: events.EvalKindEval,
		Value: strings.Repeat("x", events.MaxEvalValueBytes+1),
	})

	select {
	case e := <-received:
		data := e.Data.(*events.EvalCompletedData)
		assert.Nil(t, data.Value, "an unbounded assignment would evict other events under burst")
		assert.True(t, data.ValueOmitted)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

// TestEmitResult_CarriesTurnIndex closes the last divergence between the two
// representations of an eval result.
//
// evals.EvalResult carries TurnIndex, set by runEvals from the EvalContext. The
// event did not, so a caller of sdk.Evaluate could place a result on a turn
// while a bus subscriber could not — for an every_turn eval, the single fact
// needed to make the result mean anything. It was reachable only when the eval
// happened to produce violations, since EvalViolationData carries its own.
//
// Turn 0 is deliberately one of the cases: the field must not be omitempty, or
// the first turn of every session is indistinguishable from "not set".
func TestEmitResult_CarriesTurnIndex(t *testing.T) {
	for _, turn := range []int{0, 3} {
		t.Run(fmt.Sprintf("turn %d", turn), func(t *testing.T) {
			bus := events.NewEventBus()
			defer bus.Close()

			received := make(chan *events.Event, 4)
			bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) { received <- e })

			runner := NewEvalRunner(NewEvalTypeRegistry(),
				WithEmitter(events.NewEmitter(bus, "", "", "")))
			runner.emitResult(nil, &EvalResult{
				EvalID: "e1", Type: "contains", Kind: events.EvalKindEval, TurnIndex: turn,
			})

			select {
			case e := <-received:
				data := e.Data.(*events.EvalCompletedData)
				assert.Equal(t, turn, data.TurnIndex,
					"a subscriber cannot place this result on a turn")
			case <-time.After(2 * time.Second):
				t.Fatal("timed out")
			}
		})
	}
}

// TestEmitResult_TurnIndexSurvivesJSON guards the omitempty trap specifically:
// the struct field can be right while the wire form drops turn 0.
func TestEmitResult_TurnIndexSurvivesJSON(t *testing.T) {
	encoded, err := json.Marshal(&events.EvalEventData{EvalID: "e1", TurnIndex: 0})
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"turn_index":0`,
		"turn 0 vanished from the wire form; omitempty makes the first turn of "+
			"every session look like an unset field")
}

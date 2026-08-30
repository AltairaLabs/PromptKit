package events

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An eval's Value is CONTENT, and it travels on the bus deliberately. The rule
// the bus enforces is about BINARY — a megabyte of base64 per turn would evict
// everything else — not about text. An eval whose value never arrived would
// leave a live consumer holding a score with no idea what was measured.
//
// Two conditions come with carrying it, and these tests hold both: a size
// bound, and redaction.

func TestSetValue_CarriesAnOrdinaryValue(t *testing.T) {
	d := &EvalEventData{}
	rubric := map[string]any{"accuracy": 0.9, "tone": "warm"}
	d.SetValue(rubric)

	assert.Equal(t, rubric, d.Value)
	assert.False(t, d.ValueOmitted, "a value that fits is not omitted")
}

func TestSetValue_DropsAValueOverTheBound(t *testing.T) {
	d := &EvalEventData{}
	d.SetValue(strings.Repeat("x", MaxEvalValueBytes+1))

	assert.Nil(t, d.Value, "an oversized value must not reach the bus")
	assert.True(t, d.ValueOmitted,
		"a dropped value must SAY it was dropped. Without this flag a consumer "+
			"cannot tell a 200KB rubric from an eval that produced no value, and "+
			"would render 'no value' for the one thing the user wanted to see")
}

// TestSetValue_BoundIsOnTheEncodingNotTheString catches a bound applied to
// len() of a string rather than to what actually goes on the wire.
func TestSetValue_BoundIsOnTheEncodingNotTheString(t *testing.T) {
	d := &EvalEventData{}
	// Each element encodes to more than one byte, so a naive count of elements
	// or of a single string would let this through.
	big := make([]string, MaxEvalValueBytes/4)
	for i := range big {
		big[i] = "abcd"
	}
	d.SetValue(big)

	assert.Nil(t, d.Value)
	assert.True(t, d.ValueOmitted)
}

func TestSetValue_NilValueIsAbsentNotOmitted(t *testing.T) {
	d := &EvalEventData{}
	d.SetValue(nil)

	assert.Nil(t, d.Value)
	assert.False(t, d.ValueOmitted,
		"an eval that produced no value has not had one omitted")
}

func TestSetValue_UnencodableIsReportedAsOmitted(t *testing.T) {
	d := &EvalEventData{}
	d.SetValue(make(chan int)) // channels do not marshal

	assert.Nil(t, d.Value)
	assert.True(t, d.ValueOmitted, "a value no consumer could decode is an omission, not an absence")
}

// --- redaction ---

// blank is a Redactor that replaces every value it is handed, so a field that
// was NOT passed through the redactor is visible as surviving text.
func blank(_ string, _ string) string { return "[redacted]" }

func TestRedacting_CoversEvalPayloads(t *testing.T) {
	original := &EvalEventData{
		EvalID:      "judge",
		Kind:        EvalKindEval,
		Explanation: "the reply told the customer their card ending 4321 was declined",
		Value: map[string]any{
			"quote":  "card ending 4321",
			"nested": []any{"card ending 4321"},
			"score":  0.9,
		},
		Details: map[string]any{"excerpt": "card ending 4321"},
		Violations: []EvalViolationData{{
			Description: "leaked a card number",
			Evidence:    map[string]any{"span": "card ending 4321"},
		}},
	}
	e := &Event{Type: EventEvalCompleted, Data: original}

	var got *Event
	Redacting(func(ev *Event) { got = ev }, blank)(e)
	require.NotNil(t, got)

	d, ok := got.Data.(*EvalEventData)
	require.True(t, ok)

	assert.Equal(t, "[redacted]", d.Explanation)
	value := d.Value.(map[string]any)
	assert.Equal(t, "[redacted]", value["quote"])
	assert.Equal(t, "[redacted]", value["nested"].([]any)[0],
		"a redactor that only rewrites top-level strings misses where quoted text actually lives")
	assert.InDelta(t, 0.9, value["score"], 0.0001, "numbers carry no text to rewrite")
	assert.Equal(t, "[redacted]", d.Details["excerpt"])
	assert.Equal(t, "[redacted]", d.Violations[0].Description)
	assert.Equal(t, "[redacted]", d.Violations[0].Evidence["span"])
}

// TestRedacting_LeavesTheOriginalEvalEventIntact is the invariant that makes
// per-subscriber redaction possible at all: the same event is fanned out to
// every subscriber, including the audit sink entitled to see it unredacted.
func TestRedacting_LeavesTheOriginalEvalEventIntact(t *testing.T) {
	original := &EvalEventData{
		Explanation: "secret",
		Value:       map[string]any{"quote": "secret", "nested": []any{"secret"}},
		Details:     map[string]any{"excerpt": "secret"},
		Violations: []EvalViolationData{{
			Description: "secret",
			Evidence:    map[string]any{"span": "secret"},
		}},
	}
	e := &Event{Type: EventEvalCompleted, Data: original}

	Redacting(func(*Event) {}, blank)(e)

	assert.Equal(t, "secret", original.Explanation)
	assert.Equal(t, "secret", original.Value.(map[string]any)["quote"])
	assert.Equal(t, "secret", original.Value.(map[string]any)["nested"].([]any)[0],
		"redaction rewrote a nested container in place; every other subscriber "+
			"just lost the content it was entitled to")
	assert.Equal(t, "secret", original.Details["excerpt"])
	assert.Equal(t, "secret", original.Violations[0].Description)
	assert.Equal(t, "secret", original.Violations[0].Evidence["span"])
}

// TestRedacting_KeepsEvalMeasurements — redacting the numbers would leave an
// audit unable to tell a blocked guardrail from a passing one, which is the
// thing it most needs.
func TestRedacting_KeepsEvalMeasurements(t *testing.T) {
	score := 0.42
	metric := 7.0
	passed := false
	e := &Event{Type: EventEvalCompleted, Data: &EvalEventData{
		EvalID: "g1", Kind: EvalKindGuardrail, Passed: &passed,
		Score: &score, MetricValue: &metric, DurationMs: 12,
	}}

	var got *Event
	Redacting(func(ev *Event) { got = ev }, blank)(e)
	d := got.Data.(*EvalEventData)

	require.NotNil(t, d.Passed)
	assert.False(t, *d.Passed)
	assert.Equal(t, EvalKindGuardrail, d.Kind)
	assert.Equal(t, "g1", d.EvalID)
	require.NotNil(t, d.Score)
	assert.InDelta(t, 0.42, *d.Score, 0.0001)
	require.NotNil(t, d.MetricValue)
	assert.InDelta(t, 7.0, *d.MetricValue, 0.0001)
	assert.Equal(t, int64(12), d.DurationMs)
}

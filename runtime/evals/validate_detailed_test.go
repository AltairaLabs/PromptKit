package evals

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ValidateEvalsDetailed names each finding's def, code and severity. Only the
// unknown-trigger finding is a warning; every other finding on the same def is
// an error.
func TestValidateEvalsDetailed_CodesAndSeverity(t *testing.T) {
	pct := 150.0
	defs := []EvalDef{
		{ID: "dup", Type: "contains", Trigger: "every_turn"},
		{ID: "dup", Type: "contains", Trigger: "on_custom_event",
			Metric: &MetricDef{Name: "bad-name", Type: "gauge"}},
		{Type: "", Trigger: "", SamplePercentage: &pct},
	}
	findings := ValidateEvalsDetailed(defs, "pack")

	type key struct {
		idx  int
		code EvalFindingCode
	}
	got := map[key]EvalFinding{}
	for _, f := range findings {
		got[key{f.Index, f.Code}] = f
	}
	require.Len(t, got, len(findings), "one finding per def and code: %v", findings)

	dup := got[key{1, EvalFindingDuplicateID}]
	assert.Equal(t, EvalFindingError, dup.Severity)
	assert.Equal(t, "dup", dup.ID)
	assert.Equal(t, `duplicate eval id "dup"`, dup.Reason)
	assert.Equal(t, `pack evals[1] (id="dup"): duplicate eval id "dup"`, dup.Message)

	trig := got[key{1, EvalFindingUnknownTrigger}]
	assert.Equal(t, EvalFindingWarning, trig.Severity)
	assert.Equal(t, `invalid trigger "on_custom_event"`, trig.Reason)

	assert.Equal(t, EvalFindingError, got[key{1, EvalFindingMetricNameInvalid}].Severity)

	for _, code := range []EvalFindingCode{
		EvalFindingIDRequired, EvalFindingTypeRequired, EvalFindingTriggerRequired, EvalFindingSamplePercentage,
	} {
		f, ok := got[key{2, code}]
		require.True(t, ok, "missing %s: %v", code, findings)
		assert.Equal(t, EvalFindingError, f.Severity)
		assert.Equal(t, "pack evals[2]: "+f.Reason, f.Message)
	}

	// ValidateEvals returns the same findings' messages, in the same order.
	msgs := make([]string, len(findings))
	for i := range findings {
		msgs[i] = findings[i].Message
	}
	assert.Equal(t, msgs, ValidateEvals(defs, "pack"))
}

func TestValidateEvalsDetailed_CleanDefsHaveNoFindings(t *testing.T) {
	defs := []EvalDef{{ID: "ok", Type: "contains", Trigger: "every_turn"}}
	assert.Empty(t, ValidateEvalsDetailed(defs, "pack"))
	assert.Nil(t, ValidateEvals(defs, "pack"))
}

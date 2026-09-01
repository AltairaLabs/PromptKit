package evals_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

// Values converts at the boundary where the generated Prompt's []*Eval meets
// the eval APIs, which take values. A nil entry there is not hypothetical: a
// pack can declare `evals: [null]`, and dereferencing it would panic on load.

func TestValuesSkipsNilEntries(t *testing.T) {
	require.Nil(t, evals.Values(nil))

	got := evals.Values([]*evals.EvalDef{
		{ID: "a", Type: "contains"},
		nil,
		{ID: "b", Type: "latency"},
	})
	require.Len(t, got, 2, "a nil entry must be skipped, not dereferenced")
	require.Equal(t, "a", got[0].ID)
	require.Equal(t, "b", got[1].ID)
}

func TestValuesOnAnEmptySlice(t *testing.T) {
	got := evals.Values([]*evals.EvalDef{})
	require.NotNil(t, got, "an empty slice is not absent")
	require.Empty(t, got)
}

// TestValuesCopies — the eval APIs take values, so a caller adjusting one must
// not reach back into the pack it came from.
func TestValuesCopies(t *testing.T) {
	def := &evals.EvalDef{ID: "a", Type: "contains"}
	got := evals.Values([]*evals.EvalDef{def})
	got[0].ID = "changed"
	require.Equal(t, "a", def.ID)
}

// TestGroupsFallsBackToTheTypeDefaults — an eval that declares no groups gets
// the well-known groups for its type, not an empty set, or group filtering
// would silently select nothing.
func TestGroupsFallsBackToTheTypeDefaults(t *testing.T) {
	require.Nil(t, evals.Groups(nil))

	implicit := evals.Groups(&evals.EvalDef{Type: "contains"})
	require.NotEmpty(t, implicit, "no explicit groups must yield the type defaults")
	require.Equal(t, evals.DefaultGroupsForType("contains"), implicit)

	explicit := evals.Groups(&evals.EvalDef{Type: "contains", Groups: []string{"nightly"}})
	require.Equal(t, []string{"nightly"}, explicit,
		"explicit groups override the defaults rather than adding to them")
}

func TestIsEnabledAndSamplePercentageHandleNil(t *testing.T) {
	require.True(t, evals.IsEnabled(nil), "absent means enabled")
	require.Equal(t, evals.DefaultSamplePercentage, evals.SamplePercentage(nil))

	require.False(t, evals.IsEnabled(&evals.EvalDef{Enabled: packspec.Ptr(false)}))
	require.InDelta(t, 25.0,
		evals.SamplePercentage(&evals.EvalDef{SamplePercentage: packspec.Ptr(25.0)}), 0.001)
}

// TestEvalWhenRoundTripsThroughTheOpenObject — $defs/Eval.when is
// additionalProperties:true with no named properties, so the generated field is
// map[string]any and EvalWhen is promptkit's reading of it.
func TestEvalWhenRoundTripsThroughTheOpenObject(t *testing.T) {
	when := &evals.EvalWhen{
		ToolCalled:   "search",
		MinToolCalls: 2,
	}

	raw := evals.EncodeEvalWhen(when)
	require.Equal(t, "search", raw["tool_called"])

	back := evals.DecodeEvalWhen(raw)
	require.NotNil(t, back)
	require.Equal(t, "search", back.ToolCalled)
	require.Equal(t, 2, back.MinToolCalls)
}

// TestEvalWhenAbsenceIsNotAGate — a `when` that decodes to no conditions must
// yield nil so the eval RUNS. Returning a zero-valued EvalWhen would be
// harmless today but is one field away from gating every eval off.
func TestEvalWhenAbsenceIsNotAGate(t *testing.T) {
	require.Nil(t, evals.DecodeEvalWhen(nil))
	require.Nil(t, evals.DecodeEvalWhen(map[string]any{}))
	require.Nil(t, evals.DecodeEvalWhen(map[string]any{"unrecognised": "key"}),
		"a shape promptkit does not understand must not gate the eval")

	require.Nil(t, evals.EncodeEvalWhen(nil))
	require.Nil(t, evals.EncodeEvalWhen(&evals.EvalWhen{}),
		"no conditions must encode to nothing, not to an empty object")

	// And the gate itself agrees: nothing declared means the eval runs.
	run, reason := evals.ShouldRunWhen(nil, nil)
	require.True(t, run)
	require.Empty(t, reason)
}

// TestDecodeEvalWhenRejectsAWrongShape — `when` comes from a pack, so its value
// can be anything.
func TestDecodeEvalWhenRejectsAWrongShape(t *testing.T) {
	require.Nil(t, evals.DecodeEvalWhen(map[string]any{"min_tool_calls": "lots"}))

	// Asserted alongside, so the test distinguishes "rejects a bad shape" from
	// "never decodes anything" — a decoder that always returned nil would gate
	// no eval and pass the negative case on its own.
	good := evals.DecodeEvalWhen(map[string]any{"min_tool_calls": 2})
	require.NotNil(t, good)
	require.Equal(t, 2, good.MinToolCalls)
}

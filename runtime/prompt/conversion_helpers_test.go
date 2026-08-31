package prompt

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
)

// The helpers here exist because the generated types hold optional object
// arrays as slices of POINTERS while the APIs behind them take values, and
// because two authoring types predate the typed shapes the spec now defines.
// They are small, they are on the load and emit paths for every pack, and their
// edge cases are all the same shape: nil in, nil out, and never a panic.

func TestPtrSliceDistinguishesNilFromEmpty(t *testing.T) {
	require.Nil(t, ptrSlice[int](nil), "nil must stay nil so omitempty keeps the key out")

	got := ptrSlice([]int{})
	require.NotNil(t, got, "an empty slice is not absent")
	require.Empty(t, got)
}

// TestPtrSliceGivesEachElementItsOwnPointer is the bug this shape invites: a
// loop that takes &v of the range variable hands every element the same
// pointer, so a pack's prompts would all end up as the last one.
func TestPtrSliceGivesEachElementItsOwnPointer(t *testing.T) {
	got := ptrSlice([]int{1, 2, 3})
	require.Len(t, got, 3)
	for i, want := range []int{1, 2, 3} {
		require.Equal(t, want, *got[i])
	}
	require.NotSame(t, got[0], got[1])
}

// TestPtrSliceCopiesRatherThanAliases — mutating the result must not reach back
// into the caller's slice.
func TestPtrSliceCopiesRatherThanAliases(t *testing.T) {
	in := []int{1}
	got := ptrSlice(in)
	*got[0] = 99
	require.Equal(t, 1, in[0])
}

func TestPtrMap(t *testing.T) {
	require.Nil(t, ptrMap[int](nil))

	got := ptrMap(map[string]int{"a": 1, "b": 2})
	require.Len(t, got, 2)
	require.Equal(t, 1, *got["a"])
	require.Equal(t, 2, *got["b"])
	require.NotSame(t, got["a"], got["b"], "each key needs its own pointer")

	// Copies, so mutating the result cannot reach the caller's map.
	in := map[string]int{"a": 1}
	out := ptrMap(in)
	*out["a"] = 99
	require.Equal(t, 1, in["a"])
}

func TestValidationRoundTripsThroughTheMapForm(t *testing.T) {
	typed := &VariableValidation{
		Pattern:   "^a+$",
		MinLength: packspec.Ptr(1),
		MaxLength: packspec.Ptr(10),
		Enum:      []any{"a", "aa"},
	}

	asMap := validationToMap(typed)
	require.Equal(t, "^a+$", asMap["pattern"])

	back := validationFromMap(asMap)
	require.NotNil(t, back)
	require.Equal(t, "^a+$", back.Pattern)
	require.Equal(t, 1, packspec.Deref(back.MinLength, 0))
	require.Equal(t, 10, packspec.Deref(back.MaxLength, 0))
	require.Len(t, back.Enum, 2)
}

// TestValidationEmptyIsAbsent — an all-zero validation must render as nothing,
// not as `"validation": {}`, which reads as "declared, with no rules".
func TestValidationEmptyIsAbsent(t *testing.T) {
	require.Nil(t, validationToMap(nil))
	require.Nil(t, validationToMap(&VariableValidation{}),
		"a validation with no rules must not produce a map")

	require.Nil(t, validationFromMap(nil))
	require.Nil(t, validationFromMap(map[string]any{}))

	// Both directions still carry a real value, so this is not passing because
	// the helpers return nil unconditionally.
	require.Equal(t, "^a$", validationToMap(&VariableValidation{Pattern: "^a$"})["pattern"])
	require.Equal(t, "^a$", validationFromMap(map[string]any{"pattern": "^a$"}).Pattern)
}

// TestValidationFromMapRejectsAWrongShape — the map form comes from authoring
// YAML, so the value can be anything. A partial decode would be worse than
// none: it would drop the entries that did not fit while looking successful.
func TestValidationFromMapRejectsAWrongShape(t *testing.T) {
	require.Nil(t, validationFromMap(map[string]any{"min_length": "not-a-number"}))

	// The same key with the right shape decodes, so the rejection is about the
	// value and not about the helper refusing everything.
	ok := validationFromMap(map[string]any{"min_length": 3})
	require.NotNil(t, ok)
	require.Equal(t, 3, packspec.Deref(ok.MinLength, 0))
}

func TestValidatorValuesSkipsNilEntries(t *testing.T) {
	require.Nil(t, ValidatorValues(nil))

	got := ValidatorValues([]*Validator{
		{Type: "max_length"},
		nil,
		{Type: "contains"},
	})
	require.Len(t, got, 2, "a nil entry must be skipped, not dereferenced")
	require.Equal(t, "max_length", got[0].Type)
	require.Equal(t, "contains", got[1].Type)
}

func TestDecodeExtraReadsBothShapes(t *testing.T) {
	// After a load, Extra holds decoded maps.
	fromDoc := decodeExtra[PerformanceMetrics](
		map[string]any{"performance": map[string]any{"avg_latency_ms": 42}}, "performance")
	require.NotNil(t, fromDoc)
	require.Equal(t, 42, fromDoc.AvgLatencyMs)

	// Set in memory this run, Extra holds the concrete value.
	inMemory := decodeExtra[PerformanceMetrics](
		map[string]any{"performance": &PerformanceMetrics{AvgLatencyMs: 7}}, "performance")
	require.NotNil(t, inMemory)
	require.Equal(t, 7, inMemory.AvgLatencyMs)

	require.Nil(t, decodeExtra[PerformanceMetrics](nil, "performance"))
	require.Nil(t, decodeExtra[PerformanceMetrics](map[string]any{}, "performance"))
	require.Nil(t, decodeExtra[PerformanceMetrics](
		map[string]any{"performance": nil}, "performance"))
	require.Nil(t, decodeExtra[PerformanceMetrics](
		map[string]any{"performance": "not-an-object"}, "performance"),
		"a wrong shape must yield nil rather than a half-filled struct")
}

// TestSetExtraDeletesRatherThanWritingNull — a nil value must remove the key, or
// a pack gains a meaningless `"performance": null`.
func TestSetExtraDeletesRatherThanWritingNull(t *testing.T) {
	extra := setExtra(nil, "performance", &PerformanceMetrics{AvgLatencyMs: 1})
	require.NotNil(t, extra, "setExtra allocates when the bag is nil")
	require.Contains(t, extra, "performance")

	extra = setExtra(extra, "performance", nil)
	require.NotContains(t, extra, "performance")

	// A TYPED nil pointer is the case a plain `value == nil` misses.
	extra = setExtra(extra, "performance", &PerformanceMetrics{})
	extra = setExtra(extra, "performance", (*PerformanceMetrics)(nil))
	require.NotContains(t, extra, "performance",
		"a typed nil pointer must delete, not store a non-nil interface holding nil")

	require.Nil(t, setExtra(nil, "performance", nil),
		"deleting from a nil bag must not allocate one")
}

func TestIsNil(t *testing.T) {
	require.True(t, isNil(nil))
	require.True(t, isNil((*PerformanceMetrics)(nil)))
	require.False(t, isNil(&PerformanceMetrics{}))
	require.False(t, isNil(0), "a zero value is not nil")
	require.False(t, isNil(""))
}

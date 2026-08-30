package packspec_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
)

// TestDerefAppliesTheFallbackOnlyWhenAbsent — Deref exists so the
// absent-versus-zero distinction is applied in one place instead of a nil check
// at every call site, which is where it gets quietly dropped. The case that
// matters is the third: an explicit zero must NOT be replaced by the fallback.
func TestDerefAppliesTheFallbackOnlyWhenAbsent(t *testing.T) {
	t.Run("nil yields the fallback", func(t *testing.T) {
		if got := packspec.Deref[int](nil, 5); got != 5 {
			t.Errorf("Deref(nil, 5) = %d, want 5", got)
		}
		if got := packspec.Deref[string](nil, "auto"); got != "auto" {
			t.Errorf("Deref(nil, \"auto\") = %q, want \"auto\"", got)
		}
	})

	t.Run("a set value wins over the fallback", func(t *testing.T) {
		if got := packspec.Deref(packspec.Ptr(9), 5); got != 9 {
			t.Errorf("Deref(ptr(9), 5) = %d, want 9", got)
		}
	})

	// The whole reason these fields are pointers. `max_tokens: 0` and
	// `temperature: 0` are real settings; substituting the default here would
	// silently override what the pack author wrote.
	t.Run("an explicit zero is preserved, not defaulted", func(t *testing.T) {
		if got := packspec.Deref(packspec.Ptr(0), 5); got != 0 {
			t.Errorf("Deref(ptr(0), 5) = %d, want 0 — an explicit zero must not "+
				"be replaced by the default", got)
		}
		if got := packspec.Deref(packspec.Ptr(false), true); got {
			t.Error("Deref(ptr(false), true) = true — an explicit false must not " +
				"be replaced by the default; this is how a disabled guardrail turns itself on")
		}
		if got := packspec.Deref(packspec.Ptr(""), "auto"); got != "" {
			t.Errorf("Deref(ptr(\"\"), \"auto\") = %q, want \"\"", got)
		}
	})
}

func TestPtrRoundTrips(t *testing.T) {
	p := packspec.Ptr(42)
	if p == nil || *p != 42 {
		t.Fatalf("Ptr(42) = %v, want a pointer to 42", p)
	}
	// Distinct pointers, so mutating one built value cannot affect another.
	if q := packspec.Ptr(42); q == p {
		t.Error("Ptr must allocate; two calls returned the same pointer")
	}
}

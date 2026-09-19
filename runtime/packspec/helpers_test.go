package packspec_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
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

// failingUnmarshaler and friends exercise the error paths of the YAML bridge.
// Extracting the bridge from 54 generated copies is what made these reachable
// at all: an error branch repeated in every generated method is an error branch
// nothing tests.
type failingUnmarshaler struct{}

func (failingUnmarshaler) UnmarshalJSON([]byte) error { return errBoom }

type failingMarshaler struct{}

func (failingMarshaler) MarshalJSON() ([]byte, error) { return nil, errBoom }

type badJSONMarshaler struct{}

func (badJSONMarshaler) MarshalJSON() ([]byte, error) { return []byte(`{"unterminated":`), nil }

type okMarshaler struct{}

func (okMarshaler) MarshalJSON() ([]byte, error) { return []byte(`{"a":1}`), nil }

var errBoom = errorString("boom")

type errorString string

func (e errorString) Error() string { return string(e) }

func TestDecodeYAMLViaJSONPropagatesErrors(t *testing.T) {
	t.Run("the yaml decode fails", func(t *testing.T) {
		unmarshal := func(any) error { return errBoom }
		if err := packspec.DecodeYAMLViaJSON(unmarshal, &packspec.MetricDef{}); err == nil {
			t.Error("a failing yaml decode must propagate, not be swallowed")
		}
	})

	t.Run("the target rejects the document", func(t *testing.T) {
		unmarshal := func(v any) error {
			*(v.(*any)) = map[string]any{"a": 1}
			return nil
		}
		if err := packspec.DecodeYAMLViaJSON(unmarshal, failingUnmarshaler{}); err == nil {
			t.Error("the target's own UnmarshalJSON error must propagate")
		}
	})

	t.Run("a value that cannot be re-encoded", func(t *testing.T) {
		// A channel has no JSON representation, so the bridge's json.Marshal
		// fails. Without propagation the caller would see a silently empty value.
		unmarshal := func(v any) error {
			*(v.(*any)) = make(chan int)
			return nil
		}
		if err := packspec.DecodeYAMLViaJSON(unmarshal, &packspec.MetricDef{}); err == nil {
			t.Error("a value with no JSON representation must error")
		}
	})
}

func TestEncodeYAMLViaJSONPropagatesErrors(t *testing.T) {
	if _, err := packspec.EncodeYAMLViaJSON(failingMarshaler{}); err == nil {
		t.Error("a failing MarshalJSON must propagate")
	}
	if _, err := packspec.EncodeYAMLViaJSON(badJSONMarshaler{}); err == nil {
		t.Error("output that is not valid JSON must error rather than yield a partial value")
	}
	out, err := packspec.EncodeYAMLViaJSON(okMarshaler{})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["a"] != float64(1) {
		t.Errorf("a valid marshaler must yield the decoded value, got %#v", out)
	}
}

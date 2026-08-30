package packspec_test

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt/packspec"
)

// TestStepInputRoundTripsBothShapes exercises the generated variant wrapper.
//
// StepInput is the spec's only union of bare shapes — a "${ref}" string or a
// free-form object — with no named field on either side. It was excluded from
// generation twice on the grounds that `any` was "the complete representation".
// It is not: `any` forces every caller to type-switch, documents neither shape,
// and cannot reject a third one. This test is what makes the wrapper worth
// having rather than a stylistic preference.
func TestStepInputRoundTripsBothShapes(t *testing.T) {
	t.Run("string reference", func(t *testing.T) {
		var in packspec.StepInput
		if err := json.Unmarshal([]byte(`"${input.order_id}"`), &in); err != nil {
			t.Fatal(err)
		}
		if in.String != "${input.order_id}" {
			t.Errorf("String = %q, want the reference", in.String)
		}
		if in.Object != nil {
			t.Errorf("Object must stay nil for a string input, got %v", in.Object)
		}
		out, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != `"${input.order_id}"` {
			t.Errorf("round trip changed the shape: %s", out)
		}
	})

	t.Run("object of literals and references", func(t *testing.T) {
		var in packspec.StepInput
		if err := json.Unmarshal([]byte(`{"id":"${input.id}","limit":5}`), &in); err != nil {
			t.Fatal(err)
		}
		if in.String != "" {
			t.Errorf("String must stay empty for an object input, got %q", in.String)
		}
		if in.Object["id"] != "${input.id}" {
			t.Errorf("Object lost its reference: %v", in.Object)
		}
		out, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var back map[string]any
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("round trip did not produce an object: %s", out)
		}
		if back["id"] != "${input.id}" {
			t.Errorf("round trip lost a value: %s", out)
		}
	})

	// The assertion `any` cannot make: a shape outside the union is rejected.
	t.Run("rejects a shape the union does not allow", func(t *testing.T) {
		for _, bad := range []string{`42`, `true`, `["a"]`} {
			var in packspec.StepInput
			if err := json.Unmarshal([]byte(bad), &in); err == nil {
				t.Errorf("%s is not a string or an object and must be rejected; got %+v", bad, in)
			}
		}
	})

	// An empty value must not masquerade as a populated one.
	t.Run("zero value marshals to null", func(t *testing.T) {
		out, err := json.Marshal(packspec.StepInput{})
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != "null" {
			t.Errorf("zero StepInput marshalled to %s, want null", out)
		}
	})
}

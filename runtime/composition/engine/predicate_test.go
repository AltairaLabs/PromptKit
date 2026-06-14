package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
)

func boolPtr(b bool) *bool { return &b }

func TestEvalPredicate_Compare(t *testing.T) {
	scope := Scope{
		"input": map[string]any{"type": "paper", "score": float64(8), "tags": []any{"a", "b"}},
	}
	cases := []struct {
		name string
		p    *composition.Predicate
		want bool
	}{
		{"equals true", &composition.Predicate{Path: "${input.type}", Op: "equals", Value: "paper"}, true},
		{"equals false", &composition.Predicate{Path: "${input.type}", Op: "equals", Value: "memo"}, false},
		{"not_equals", &composition.Predicate{Path: "${input.type}", Op: "not_equals", Value: "memo"}, true},
		{"less_than", &composition.Predicate{Path: "${input.score}", Op: "less_than", Value: float64(10)}, true},
		{"greater_than_or_equals", &composition.Predicate{Path: "${input.score}", Op: "greater_than_or_equals", Value: float64(8)}, true},
		{"in", &composition.Predicate{Path: "${input.type}", Op: "in", Value: []any{"paper", "memo"}}, true},
		{"not_in", &composition.Predicate{Path: "${input.type}", Op: "not_in", Value: []any{"memo"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := evalPredicate(c.p, scope)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestEvalPredicate_Exists(t *testing.T) {
	scope := Scope{"input": map[string]any{"type": "paper"}}
	got, _ := evalPredicate(&composition.Predicate{Path: "${input.type}", Exists: boolPtr(true)}, scope)
	if !got {
		t.Error("exists:true on present path should be true")
	}
	got, _ = evalPredicate(&composition.Predicate{Path: "${input.type}", Exists: boolPtr(false)}, scope)
	if got {
		t.Error("exists:false on present path should be false")
	}
	got, _ = evalPredicate(&composition.Predicate{Path: "${input.nope}", Exists: boolPtr(true)}, scope)
	if got {
		t.Error("exists:true on missing path should be false")
	}
}

func TestEvalPredicate_Composites(t *testing.T) {
	scope := Scope{"input": map[string]any{"type": "paper", "score": float64(8)}}
	allOf := &composition.Predicate{AllOf: []*composition.Predicate{
		{Path: "${input.type}", Op: "equals", Value: "paper"},
		{Path: "${input.score}", Op: "greater_than", Value: float64(5)},
	}}
	if got, _ := evalPredicate(allOf, scope); !got {
		t.Error("all_of should be true")
	}
	anyOf := &composition.Predicate{AnyOf: []*composition.Predicate{
		{Path: "${input.type}", Op: "equals", Value: "memo"},
		{Path: "${input.score}", Op: "greater_than", Value: float64(5)},
	}}
	if got, _ := evalPredicate(anyOf, scope); !got {
		t.Error("any_of should be true")
	}
	not := &composition.Predicate{Not: &composition.Predicate{Path: "${input.type}", Op: "equals", Value: "memo"}}
	if got, _ := evalPredicate(not, scope); !got {
		t.Error("not should be true")
	}
}

func TestEvalPredicate_TypeMismatchErrors(t *testing.T) {
	scope := Scope{"input": map[string]any{"type": "paper"}}
	if _, err := evalPredicate(&composition.Predicate{Path: "${input.type}", Op: "less_than", Value: float64(3)}, scope); err == nil {
		t.Error("expected error comparing string < number")
	}
}

func TestEvalPredicate_Nil(t *testing.T) {
	_, err := evalPredicate(nil, Scope{})
	if err == nil {
		t.Error("expected error for nil predicate")
	}
}

func TestEvalPredicate_AllOfShortCircuit(t *testing.T) {
	scope := Scope{"input": map[string]any{"type": "paper"}}
	// First sub false → short-circuit, second sub never evaluated.
	p := &composition.Predicate{AllOf: []*composition.Predicate{
		{Path: "${input.type}", Op: "equals", Value: "memo"},
		{Path: "${input.type}", Op: "equals", Value: "paper"},
	}}
	got, err := evalPredicate(p, scope)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("all_of with first-sub false should be false")
	}
}

func TestEvalPredicate_AnyOfAllFalse(t *testing.T) {
	scope := Scope{"input": map[string]any{"type": "paper"}}
	p := &composition.Predicate{AnyOf: []*composition.Predicate{
		{Path: "${input.type}", Op: "equals", Value: "memo"},
		{Path: "${input.type}", Op: "equals", Value: "fax"},
	}}
	got, err := evalPredicate(p, scope)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("any_of with all-false subs should be false")
	}
}

func TestEvalPredicate_MissingPathCompare(t *testing.T) {
	scope := Scope{"input": map[string]any{"type": "paper"}}
	// equals on a missing path should be false (not an error).
	got, err := evalPredicate(&composition.Predicate{Path: "${input.nope}", Op: "equals", Value: "paper"}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("equals on missing path should be false")
	}
	// not_equals on a missing path should be true.
	got, err = evalPredicate(&composition.Predicate{Path: "${input.nope}", Op: "not_equals", Value: "paper"}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("not_equals on missing path should be true")
	}
}

func TestEvalPredicate_UnknownOp(t *testing.T) {
	scope := Scope{"input": map[string]any{"type": "paper"}}
	if _, err := evalPredicate(&composition.Predicate{Path: "${input.type}", Op: "contains", Value: "paper"}, scope); err == nil {
		t.Error("expected error for unknown op")
	}
}

func TestEvalPredicate_InListError(t *testing.T) {
	scope := Scope{"input": map[string]any{"type": "paper"}}
	// Value must be []any for in/not_in; passing a string should error.
	if _, err := evalPredicate(&composition.Predicate{Path: "${input.type}", Op: "in", Value: "paper"}, scope); err == nil {
		t.Error("expected error when in-list value is not a slice")
	}
}

func TestEvalPredicate_LessThanOrEquals(t *testing.T) {
	scope := Scope{"input": map[string]any{"score": float64(8)}}
	got, err := evalPredicate(&composition.Predicate{Path: "${input.score}", Op: "less_than_or_equals", Value: float64(8)}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("less_than_or_equals with equal values should be true")
	}
}

func TestEvalPredicate_NumericCoercion(t *testing.T) {
	// Scope values of int kind (not float64) should compare numerically.
	scope := Scope{"input": map[string]any{"count": int(5)}}
	got, err := evalPredicate(&composition.Predicate{Path: "${input.count}", Op: "equals", Value: float64(5)}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("int(5) should equal float64(5) via numeric coercion")
	}
}

func TestEvalPredicate_NotError(t *testing.T) {
	// not wrapping a predicate that itself errors should propagate the error.
	p := &composition.Predicate{
		Not: &composition.Predicate{Path: "${input.score}", Op: "less_than", Value: float64(3)},
	}
	scope := Scope{"input": map[string]any{"score": "notanumber"}}
	if _, err := evalPredicate(p, scope); err == nil {
		t.Error("expected error propagated through not")
	}
}

func TestEvalPredicate_Int64Coercion(t *testing.T) {
	// int64 scope values should compare numerically via toFloat.
	scope := Scope{"input": map[string]any{"count": int64(42)}}
	got, err := evalPredicate(&composition.Predicate{Path: "${input.count}", Op: "greater_than", Value: float64(10)}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("int64(42) > float64(10) should be true")
	}
}

func TestEvalPredicate_Float32Coercion(t *testing.T) {
	// float32 scope values should compare numerically via toFloat.
	scope := Scope{"input": map[string]any{"ratio": float32(0.5)}}
	got, err := evalPredicate(&composition.Predicate{Path: "${input.ratio}", Op: "less_than", Value: float64(1.0)}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("float32(0.5) < float64(1.0) should be true")
	}
}

func TestEvalPredicate_EdgeCases(t *testing.T) {
	scope := Scope{"input": map[string]any{"type": "paper"}}

	// not_in on a missing path: actual is nil, nil is not in the list → true.
	got, err := evalPredicate(&composition.Predicate{Path: "${input.nope}", Op: "not_in", Value: []any{"memo"}}, scope)
	if err != nil || !got {
		t.Errorf("not_in missing path = (%v,%v), want (true,nil)", got, err)
	}

	// empty all_of is vacuously true.
	got, err = evalPredicate(&composition.Predicate{AllOf: []*composition.Predicate{}}, scope)
	if err != nil || !got {
		t.Errorf("empty all_of = (%v,%v), want (true,nil)", got, err)
	}

	// empty any_of is false.
	got, err = evalPredicate(&composition.Predicate{AnyOf: []*composition.Predicate{}}, scope)
	if err != nil || got {
		t.Errorf("empty any_of = (%v,%v), want (false,nil)", got, err)
	}
}

package engine

import (
	"reflect"
	"testing"
)

func TestStripRef(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"${input.text}", "input.text", true},
		{"${ classify.output.type }", "classify.output.type", true},
		{"input.text", "", false}, // RFC Q4: ${...} wrapper required
		{"prefix ${input.text}", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := stripRef(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("stripRef(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestResolvePath(t *testing.T) {
	scope := Scope{
		"input": map[string]any{"text": "hello", "n": float64(3)},
		"classify": map[string]any{
			"output": map[string]any{"type": "research_paper"},
		},
	}
	cases := []struct {
		ref    string
		want   any
		wantOK bool
	}{
		{"${input.text}", "hello", true},
		{"${input.n}", float64(3), true},
		{"${classify.output.type}", "research_paper", true},
		{"${classify.output.missing}", nil, false},
		{"${nope.output.x}", nil, false},
		{"${input}", map[string]any{"text": "hello", "n": float64(3)}, true},
		{"bare.path", nil, false},
	}
	for _, c := range cases {
		got, ok := resolvePath(c.ref, scope)
		if ok != c.wantOK {
			t.Fatalf("resolvePath(%q) ok = %v, want %v", c.ref, ok, c.wantOK)
		}
		if ok && !deepEqualJSON(got, c.want) {
			t.Errorf("resolvePath(%q) = %#v, want %#v", c.ref, got, c.want)
		}
	}
}

// deepEqualJSON compares two decoded-JSON values structurally.
func deepEqualJSON(a, b any) bool {
	return reflectDeepEqual(a, b)
}

// reflectDeepEqual is a thin wrapper over reflect.DeepEqual.
// NOTE: when Task 5 adds engine_test.go, this definition must move to exactly
// one place to avoid a redeclaration compile error.
func reflectDeepEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

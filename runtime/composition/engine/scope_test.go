package engine

import (
	"reflect"
	"strings"
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
		{"${input.text.extra}", nil, false}, // non-map value blocks mid-path descent
	}
	for _, c := range cases {
		got, ok := resolvePath(c.ref, scope)
		if ok != c.wantOK {
			t.Errorf("resolvePath(%q) ok = %v, want %v", c.ref, ok, c.wantOK)
			continue
		}
		if ok && !reflectDeepEqual(got, c.want) {
			t.Errorf("resolvePath(%q) = %#v, want %#v", c.ref, got, c.want)
		}
	}
}

// reflectDeepEqual is the shared structural-equality helper for this package's tests.
// It wraps reflect.DeepEqual for use across all _test.go files in the engine package.
func reflectDeepEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

func TestResolveInput(t *testing.T) {
	scope := Scope{
		"input":    map[string]any{"text": "doc body", "id": float64(7)},
		"classify": map[string]any{"output": map[string]any{"type": "paper"}},
	}

	// pure ref preserves the concrete value (here a map)
	got, err := resolveInput("${input}", scope)
	if err != nil {
		t.Fatal(err)
	}
	if !reflectDeepEqual(got, map[string]any{"text": "doc body", "id": float64(7)}) {
		t.Errorf("pure ref = %#v", got)
	}

	// pure ref to a scalar preserves type (number stays float64)
	got, _ = resolveInput("${input.id}", scope)
	if got != float64(7) {
		t.Errorf("scalar ref = %#v, want 7", got)
	}

	// interpolated string substitutes stringified values
	got, _ = resolveInput("type is ${classify.output.type}", scope)
	if got != "type is paper" {
		t.Errorf("interpolated = %#v", got)
	}

	// multiple embedded refs in one string, including a non-string value stringified
	got, _ = resolveInput("${input.text} has id ${input.id}", scope)
	if got != "doc body has id 7" {
		t.Errorf("multi/typed interpolation = %#v, want %q", got, "doc body has id 7")
	}

	// multiple unresolvable embedded refs report all of them
	if _, err := resolveInput("${input.nope} and ${input.also}", scope); err == nil {
		t.Error("expected error for multiple unresolvable embedded refs")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "input.nope") || !strings.Contains(msg, "input.also") {
			t.Errorf("joined error should name both refs, got %q", msg)
		}
	}

	// object combining literals + refs recurses
	got, _ = resolveInput(map[string]any{
		"content": "${input.text}",
		"kind":    "${classify.output.type}",
		"literal": "x",
	}, scope)
	want := map[string]any{"content": "doc body", "kind": "paper", "literal": "x"}
	if !reflectDeepEqual(got, want) {
		t.Errorf("object = %#v, want %#v", got, want)
	}

	// slice recurses
	got, _ = resolveInput([]any{"${input.text}", "lit"}, scope)
	if !reflectDeepEqual(got, []any{"doc body", "lit"}) {
		t.Errorf("slice = %#v", got)
	}

	// unresolvable ref is an error
	if _, err := resolveInput("${input.missing}", scope); err == nil {
		t.Error("expected error for unresolvable ref")
	}

	// nil passes through
	if got, err := resolveInput(nil, scope); err != nil || got != nil {
		t.Errorf("nil = (%#v,%v)", got, err)
	}
}

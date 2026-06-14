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

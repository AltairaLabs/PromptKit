package composition

import (
	"reflect"
	"sort"
	"testing"
)

func TestCollectRefRoots(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"string ref", "${input.text}", []string{"input"}},
		{"plain string", "no refs here", nil},
		{"nested object", map[string]any{
			"a": "${classify.output.type}",
			"b": map[string]any{"c": "${input.x}"},
		}, []string{"classify", "input"}},
		{"slice", []any{"${a.output.y}", "literal"}, []string{"a"}},
		{"multiple in one string", "${input.x} and ${meta.output.z}", []string{"input", "meta"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectRefRoots(tc.in)
			sort.Strings(got)
			sort.Strings(tc.want)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("collectRefRoots(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

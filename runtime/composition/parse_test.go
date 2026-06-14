package composition

import (
	"strings"
	"testing"
)

func TestParseConfig_Nil(t *testing.T) {
	got, err := ParseConfig(nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}

func TestParseConfig_Map(t *testing.T) {
	raw := map[string]any{
		"analyze": map[string]any{
			"version": 1,
			"steps": []any{
				map[string]any{"id": "classify", "kind": "prompt", "prompt_task": "c"},
			},
		},
	}
	comps, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	c, ok := comps["analyze"]
	if !ok {
		t.Fatalf("missing composition; got keys %v", comps)
	}
	if c.Version != 1 || len(c.Steps) != 1 || c.Steps[0].ID != "classify" {
		t.Fatalf("bad parse: %+v", c)
	}
}

func TestParseConfig_InvalidStructure(t *testing.T) {
	// Pass a non-object value so json.Unmarshal into map[string]*Composition fails.
	_, err := ParseConfig("not a map")
	if err == nil {
		t.Fatal("want error for invalid structure, got nil")
	}
	if !strings.Contains(err.Error(), "parsing compositions config") {
		t.Errorf("unexpected error message: %v", err)
	}
}

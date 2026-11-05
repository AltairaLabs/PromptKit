package base

import "testing"

func TestResolveContextWindow(t *testing.T) {
	table := map[string]int{"m": 1000}
	cases := []struct {
		name     string
		model    string
		override int
		table    map[string]int
		want     int
	}{
		{"table hit", "m", 0, table, 1000},
		{"vendor prefix stripped", "vendor/m", 0, table, 1000},
		{"override beats table", "m", 50, table, 50},
		{"unknown model", "x", 0, table, 0},
		{"nil table", "m", 0, nil, 0},
		{"negative override ignored", "m", -5, table, 1000},
	}
	for _, c := range cases {
		if got := ResolveContextWindow("id", c.model, c.override, c.table); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestContextWindowFromConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want int
	}{
		{"int", map[string]any{"context_window": 8192}, 8192},
		{"int64", map[string]any{"context_window": int64(8192)}, 8192},
		{"float64", map[string]any{"context_window": float64(8192)}, 8192},
		{"negative", map[string]any{"context_window": -1}, 0},
		{"string", map[string]any{"context_window": "8192"}, 0},
		{"absent", map[string]any{}, 0},
		{"nil map", nil, 0},
	}
	for _, c := range cases {
		if got := ContextWindowFromConfig(c.cfg); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

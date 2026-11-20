package base

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

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
		{"dated snapshot resolves to alias", "m-2026-04-23", 0, table, 1000},
		{"compact dated snapshot resolves to alias", "vendor/m-20260423", 0, table, 1000},
		{"non-date suffix is a different model", "m-pro", 0, table, 0},
		{"impossible date is not stripped", "m-2026-13-45", 0, table, 0},
	}
	for _, c := range cases {
		if got := ResolveContextWindow("id", c.model, c.override, c.table); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// A table-backed provider that does not know the model says so by name, once,
// because the compactor then silently budgets with its own default. A provider
// with no table (self-hosted) is expected to rely on the override and stays quiet.
func TestResolveContextWindow_UnknownModelWarnsOnce(t *testing.T) {
	ResetUnpricedWarnings()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	defer logger.SetOutput(nil)

	table := map[string]int{"m": 1000}
	ResolveContextWindow("id", "brand-new-window-model", 0, table)
	ResolveContextWindow("id", "brand-new-window-model", 0, table)
	ResolveContextWindow("id", "self-hosted-window-model", 0, nil)
	ResolveContextWindow("id", "m", 0, table)

	out := buf.String()
	if n := strings.Count(out, "no built-in context window for model"); n != 1 {
		t.Errorf("warning count = %d, want 1; log:\n%s", n, out)
	}
	if !strings.Contains(out, "brand-new-window-model") {
		t.Errorf("warning does not name the model; log:\n%s", out)
	}
	if strings.Contains(out, "self-hosted-window-model") {
		t.Errorf("table-less provider warned; log:\n%s", out)
	}
	// A row recording no window (0) is no better than a missing row.
	ResolveContextWindow("id", "zero-row", 0, map[string]int{"zero-row": 0})
	if !strings.Contains(buf.String(), "zero-row") {
		t.Errorf("zero-valued row did not warn; log:\n%s", buf.String())
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

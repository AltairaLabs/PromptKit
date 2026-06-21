package engine

import (
	"path/filepath"
	"testing"
)

const interactiveFixture = "testdata/interactive/config.arena.yaml"

func newFixtureEngine(t *testing.T) *Engine {
	t.Helper()
	eng, err := NewEngineFromConfigFile(filepath.Clean(interactiveFixture))
	if err != nil {
		t.Fatalf("NewEngineFromConfigFile: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

func TestEngine_Agents(t *testing.T) {
	eng := newFixtureEngine(t)
	agents := eng.Agents()
	if len(agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(agents))
	}
	if agents[0].TaskType != "basic" {
		t.Fatalf("want task_type basic, got %q", agents[0].TaskType)
	}
}

func TestEngine_ProviderIDs(t *testing.T) {
	eng := newFixtureEngine(t)
	ids := eng.ProviderIDs()
	if len(ids) != 1 || ids[0] != "mock" {
		t.Fatalf("want [mock], got %v", ids)
	}
}

func TestEngine_MissingRequiredVars(t *testing.T) {
	eng := newFixtureEngine(t)

	missing, err := eng.MissingRequiredVars("basic", nil)
	if err != nil {
		t.Fatalf("MissingRequiredVars: %v", err)
	}
	if len(missing) != 1 || missing[0] != "company" {
		t.Fatalf("want [company] missing, got %v", missing)
	}

	// Blank value counts as missing.
	missing, err = eng.MissingRequiredVars("basic", map[string]string{"company": ""})
	if err != nil {
		t.Fatalf("MissingRequiredVars (blank): %v", err)
	}
	if len(missing) != 1 || missing[0] != "company" {
		t.Fatalf("want [company] missing for blank value, got %v", missing)
	}

	missing, err = eng.MissingRequiredVars("basic", map[string]string{"company": "Acme"})
	if err != nil {
		t.Fatalf("MissingRequiredVars (provided): %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("want none missing, got %v", missing)
	}
}

func TestEngine_MissingRequiredVars_UnknownTaskType(t *testing.T) {
	eng := newFixtureEngine(t)
	_, err := eng.MissingRequiredVars("no-such-task", nil)
	if err == nil {
		t.Fatal("want error for unknown task type, got nil")
	}
}

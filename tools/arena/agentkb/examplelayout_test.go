package agentkb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codegenExampleDir = "../../../examples/test-a-codegen-agent"

// TestExampleLayout pins examples/test-a-codegen-agent to the repo's
// example-layout convention: a root config.arena.yaml, prompt configs under
// prompts/, *.provider.yaml under providers/, *.scenario.yaml under scenarios/,
// and no packs/ or configs/ subdirs.
func TestExampleLayout(t *testing.T) {
	if _, err := os.Stat(filepath.Join(codegenExampleDir, "config.arena.yaml")); err != nil {
		t.Errorf("missing root config.arena.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codegenExampleDir, "prompts", "authoring-agent.yaml")); err != nil {
		t.Errorf("expected prompts/authoring-agent.yaml: %v", err)
	}
	for _, banned := range []string{"packs", "configs"} {
		if fi, err := os.Stat(filepath.Join(codegenExampleDir, banned)); err == nil && fi.IsDir() {
			t.Errorf("non-conventional dir present: %s/", banned)
		}
	}
	checkExampleSuffix(t, filepath.Join(codegenExampleDir, "providers"), ".provider.yaml", map[string]bool{"mock-provider.yaml": true})
	checkExampleSuffix(t, filepath.Join(codegenExampleDir, "scenarios"), ".scenario.yaml", nil)
}

func checkExampleSuffix(t *testing.T, dir, suffix string, allow map[string]bool) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if allow[e.Name()] {
			continue
		}
		if !strings.HasSuffix(e.Name(), suffix) {
			t.Errorf("%s/%s does not match %q convention", filepath.Base(dir), e.Name(), suffix)
		}
	}
}

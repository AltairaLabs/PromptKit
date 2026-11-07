package yaml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// writeFragmentPack lays out base/prompts/support.yaml with a fragment at
// base/prompts/frags/intro.yaml, referenced by a path relative to the prompt.
func writeFragmentPack(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	promptDir := filepath.Join(base, "prompts")
	if err := os.MkdirAll(filepath.Join(promptDir, "frags"), 0o755); err != nil {
		t.Fatal(err)
	}
	promptYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: support
  version: v1.0.0
  description: Support
  system_template: "{{intro}}"
  fragments:
    - name: intro
      path: frags/intro.yaml
      required: true
`
	fragYAML := "fragment_type: text\nversion: v1\ncontent: Welcome to support.\n"
	if err := os.WriteFile(filepath.Join(promptDir, "support.yaml"), []byte(promptYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "frags", "intro.yaml"), []byte(fragYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return base
}

func TestPromptDir_IsTheConfigFilesDirectory(t *testing.T) {
	base := writeFragmentPack(t)
	repo := NewYAMLPromptRepository(base, map[string]string{"support": "prompts/support.yaml"})
	dir, err := repo.PromptDir("support")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "prompts"); dir != want {
		t.Fatalf("PromptDir = %q, want %q", dir, want)
	}
	if _, err := repo.PromptDir("missing"); err == nil {
		t.Fatal("PromptDir of an unknown task type should fail")
	}
}

// A fragment path is relative to the prompt config, not the process's working
// directory, both when rendering and when compiling a pack.
func TestFragmentPath_ResolvesAgainstPromptConfigDir(t *testing.T) {
	base := writeFragmentPack(t)
	repo := NewYAMLPromptRepository(base, map[string]string{"support": "prompts/support.yaml"})
	reg := prompt.NewRegistryWithRepository(repo)

	tmpl, err := reg.LoadTemplate("support", nil, "")
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}
	if got := tmpl.FragmentVars["intro"]; got != "Welcome to support." {
		t.Fatalf("fragment var = %q", got)
	}

	pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := pack.Fragments["intro"]; got != "Welcome to support." {
		t.Fatalf("pack fragment = %q", got)
	}
}

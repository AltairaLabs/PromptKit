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
	writePromptWithFragmentPath(t, base, "frags/intro.yaml")
	writeFragmentFile(t, filepath.Join(base, "prompts", "frags", "intro.yaml"), "Welcome to support.")
	return base
}

// writePromptWithFragmentPath writes base/prompts/support.yaml, whose one
// required fragment "intro" is referenced by fragPath.
func writePromptWithFragmentPath(t *testing.T, base, fragPath string) {
	t.Helper()
	promptDir := filepath.Join(base, "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
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
      path: ` + fragPath + `
      required: true
`
	if err := os.WriteFile(filepath.Join(promptDir, "support.yaml"), []byte(promptYAML), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeFragmentFile writes a text fragment with the given content at path.
func writeFragmentFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	fragYAML := "fragment_type: text\nversion: v1\ncontent: " + content + "\n"
	if err := os.WriteFile(path, []byte(fragYAML), 0o600); err != nil {
		t.Fatal(err)
	}
}

// assertIntroResolves checks the "intro" fragment resolves to want both when
// rendering the prompt and when compiling it into a pack.
func assertIntroResolves(t *testing.T, base, want string) {
	t.Helper()
	repo := NewYAMLPromptRepository(base, map[string]string{"support": "prompts/support.yaml"})
	reg := prompt.NewRegistryWithRepository(repo)

	tmpl, err := reg.LoadTemplate("support", nil, "")
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}
	if got := tmpl.FragmentVars["intro"]; got != want {
		t.Fatalf("fragment var = %q, want %q", got, want)
	}

	pack, err := prompt.NewPackCompiler(reg).Compile("support", "test")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := pack.Fragments["intro"]; got != want {
		t.Fatalf("pack fragment = %q, want %q", got, want)
	}
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
	assertIntroResolves(t, writeFragmentPack(t), "Welcome to support.")
}

// An absolute fragment path is used as it is, not joined onto the prompt
// config's directory.
func TestFragmentPath_AbsolutePathIsUsedAsIs(t *testing.T) {
	base := t.TempDir()
	fragPath := filepath.Join(t.TempDir(), "shared", "frags", "intro.yaml")
	writeFragmentFile(t, fragPath, "Shared intro.")
	writePromptWithFragmentPath(t, base, fragPath)
	assertIntroResolves(t, base, "Shared intro.")
}

// A relative fragment path that is not under the prompt config's directory
// still resolves the way it did before paths were prompt-relative: against the
// process's working directory.
func TestFragmentPath_RelativeFallsBackToWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	cwd := t.TempDir()
	t.Chdir(cwd)
	writeFragmentFile(t, filepath.Join(cwd, "frags", "intro.yaml"), "From the working directory.")
	writePromptWithFragmentPath(t, base, "frags/intro.yaml")
	assertIntroResolves(t, base, "From the working directory.")
}

// When the fragment exists under both, the prompt config's directory wins.
func TestFragmentPath_PromptDirWinsOverWorkingDirectory(t *testing.T) {
	base := writeFragmentPack(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	writeFragmentFile(t, filepath.Join(cwd, "frags", "intro.yaml"), "From the working directory.")
	assertIntroResolves(t, base, "Welcome to support.")
}

// A required fragment found under neither base is still an error.
func TestFragmentPath_MissingEverywhereIsAnError(t *testing.T) {
	base := t.TempDir()
	t.Chdir(t.TempDir())
	writePromptWithFragmentPath(t, base, "frags/intro.yaml")
	repo := NewYAMLPromptRepository(base, map[string]string{"support": "prompts/support.yaml"})
	reg := prompt.NewRegistryWithRepository(repo)
	if _, err := reg.LoadTemplate("support", nil, ""); err == nil {
		t.Fatal("LoadTemplate should fail for a required fragment that exists nowhere")
	}
	if _, err := prompt.NewPackCompiler(reg).Compile("support", "test"); err == nil {
		t.Fatal("Compile should fail for a required fragment that exists nowhere")
	}
}

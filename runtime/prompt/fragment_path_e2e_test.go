package prompt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	yamlrepo "github.com/AltairaLabs/PromptKit/runtime/v2/persistence/yaml"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

func writeFragmentFixture(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

const optionalEmptyPathPrompt = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: support
  version: 1.0.0
  description: Support
  system_template: "Support.{{persona}}"
  variables:
    - name: tone
      required: false
      default: ""
  fragments:
    - name: persona
      path: ../fragments/persona_{{tone}}.yaml
      required: false
`

// An optional fragment whose path a variable fills, with the variable
// empty (its declared default, or SetVar("tone", "")). The path names a
// missing file, so the optional fragment renders empty.
func TestFragmentPath_OptionalFragmentEmptyValueStillLoads(t *testing.T) {
	base := t.TempDir()
	writeFragmentFixture(t, filepath.Join(base, "prompts", "support.yaml"), optionalEmptyPathPrompt)
	writeFragmentFixture(t, filepath.Join(base, "fragments", "persona_warm.yaml"), "fragment_type: text\nversion: v1\ncontent: Warm.\n")
	for name, vars := range map[string]map[string]string{
		"default empty": nil,
		"set empty":     {"tone": ""},
	} {
		t.Run(name, func(t *testing.T) {
			reg := prompt.NewRegistryWithRepository(
				yamlrepo.NewYAMLPromptRepository(base, map[string]string{"support": "prompts/support.yaml"}))
			tmpl, err := reg.LoadTemplate("support", vars, "")
			require.NoError(t, err, "an optional fragment chosen by an empty variable must not fail the prompt")
			require.Equal(t, "", tmpl.FragmentVars["persona"], "the missing optional fragment renders empty")
		})
	}
}

// A pack fragment whose key holds "/" or ":" (the PromptPack schema puts
// no pattern on fragment keys), chosen by a name-only reference "{{persona}}".
// The memory repository reads no file, so nothing can escape: the key is
// accepted.
func TestFragmentPath_NameOnlyReferenceAcceptsAnyPackKey(t *testing.T) {
	for _, key := range []string{"support/formal", "persona:formal"} {
		t.Run(key, func(t *testing.T) {
			repo := memory.NewPromptRepository()
			repo.RegisterPrompt("support", &prompt.Config{
				APIVersion: "promptkit.altairalabs.ai/v1alpha1", Kind: "PromptConfig",
				Spec: prompt.Spec{
					TaskType: "support", Version: "1.0.0", Description: "s",
					SystemTemplate: "Support.{{" + key + "}}",
					Fragments:      []prompt.FragmentRef{{Name: "{{persona}}", Required: true}},
				},
			})
			repo.RegisterFragment(key, &prompt.Fragment{Type: "text", Content: "Formal."})
			reg := prompt.NewRegistryWithRepository(repo)
			tmpl, err := reg.LoadTemplate("support", map[string]string{"persona": key}, "")
			require.NoError(t, err)
			require.Equal(t, "Formal.", tmpl.FragmentVars[key])
		})
	}
}

// uncontainedPathRepo reads <baseDir>/<relativePath> with no containment, as
// PromptArena's promptrepo.Repository (arena/promptrepo) does.
type uncontainedPathRepo struct{ *memory.PromptRepository }

func (r uncontainedPathRepo) LoadFragment(name, rel, baseDir string) (*prompt.Fragment, error) {
	data, err := os.ReadFile(filepath.Join(baseDir, rel))
	if err != nil {
		return nil, err
	}
	var f prompt.Fragment
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Two adjacent placeholders, each filled with ".", form "..". Each value
// passes IsPlainFragmentValue, so the check that "a variable's value chooses a
// fragment, never a file elsewhere" is bypassed. With a repository that does
// not contain paths (PromptArena's), the render reads a file outside the
// prompt's directory.
func TestFragmentPath_AdjacentDotValuesCannotClimb(t *testing.T) {
	root := t.TempDir()
	promptDir := filepath.Join(root, "prompts")
	writeFragmentFixture(t, filepath.Join(root, "secret.yaml"), "fragment_type: text\nversion: v1\ncontent: SECRET\n")
	repo := &uncontainedDirRepo{PromptRepository: memory.NewPromptRepository(), dir: promptDir}
	repo.RegisterPrompt("support", &prompt.Config{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1", Kind: "PromptConfig",
		Spec: prompt.Spec{
			TaskType: "support", Version: "1.0.0", Description: "s",
			SystemTemplate: "Support. {{persona}}",
			Fragments: []prompt.FragmentRef{
				{Name: "persona", Path: "{{region}}{{tier}}/secret.yaml", Required: true},
			},
		},
	})
	reg := prompt.NewRegistryWithRepository(repo)
	tmpl, err := reg.LoadTemplate("support", map[string]string{"region": ".", "tier": "."}, "")
	if err == nil {
		require.NotContains(t, tmpl.DefaultVars["persona"], "SECRET",
			"values {region: \".\", tier: \".\"} read ../secret.yaml outside the prompt's directory")
	}
	require.ErrorIs(t, err, prompt.ErrUnsafeFragmentValue)
}

type uncontainedDirRepo struct {
	*memory.PromptRepository
	dir string
}

func (r *uncontainedDirRepo) LoadFragment(name, rel, baseDir string) (*prompt.Fragment, error) {
	return uncontainedPathRepo{r.PromptRepository}.LoadFragment(name, rel, baseDir)
}

func (r *uncontainedDirRepo) PromptDir(string) (string, error) { return r.dir, nil }

// A YAML repository over the prompts directory, with fragments in a
// sibling directory, referenced the conventional way ("../fragments/x.yaml").
// v2.18.0 read it; it now fails ErrFragmentPathOutsideBase, as the path leaves
// both the base and the prompt's directory.
func TestFragmentPath_SiblingFragmentsOfAPromptsBase(t *testing.T) {
	root := t.TempDir()
	writeFragmentFixture(t, filepath.Join(root, "prompts", "support.yaml"), `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: support
  version: 1.0.0
  description: Support
  system_template: "Support. {{tone}}"
  fragments:
    - name: tone
      path: ../fragments/tone.yaml
      required: true
`)
	writeFragmentFixture(t, filepath.Join(root, "fragments", "tone.yaml"), "fragment_type: text\nversion: v1\ncontent: Warm.\n")
	reg := prompt.NewRegistryWithRepository(
		yamlrepo.NewYAMLPromptRepository(filepath.Join(root, "prompts"), map[string]string{"support": "support.yaml"}))
	tmpl, err := reg.LoadTemplate("support", nil, "")
	require.NoError(t, err)
	require.Equal(t, "Warm.", tmpl.FragmentVars["tone"])
}

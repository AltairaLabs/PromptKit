package stage_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	yamlrepo "github.com/AltairaLabs/PromptKit/runtime/v2/persistence/yaml"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/runtime/v2/variables"
)

// runAssembly runs the variable, prompt-assembly and template stages in the
// order the SDK builds them and returns the rendered system prompt and the
// first stage error.
func runAssembly(t *testing.T, reg *prompt.Registry, static map[string]string, providers []variables.Provider) (string, error) {
	t.Helper()
	ts := stage.NewTurnState()
	stages := []stage.Stage{
		stage.NewVariableProviderStageWithVarsAndTurnState(static, providers, ts),
		stage.NewPromptAssemblyStageWithTurnState(reg, "chat", static, ts),
		stage.NewTemplateStageWithTurnState(nil, ts),
	}
	in := make(chan stage.StreamElement, 1)
	in <- stage.StreamElement{Message: &types.Message{Role: "user", Content: "hi"}}
	close(in)
	var cur <-chan stage.StreamElement = in
	errs := make(chan error, len(stages))
	for _, st := range stages {
		out := make(chan stage.StreamElement, 4)
		go func(st stage.Stage, in <-chan stage.StreamElement, out chan<- stage.StreamElement) {
			err := st.Process(context.Background(), in, out)
			for range in { // drain, as the pipeline does after a stage fails
			}
			errs <- err
		}(st, cur, out)
		cur = out
	}
	for range cur {
	}
	var first error
	for range stages {
		if err := <-errs; err != nil && first == nil {
			first = err
		}
	}
	return ts.SystemPrompt, first
}

// A variable's value fills a fragment's path, so it must name a fragment,
// never a file elsewhere: a value with a separator, "..", or an absolute path
// fails the turn, from any source, and one outside the variable's declared
// enum does too. The secret file is never read into the prompt.
func TestPromptAssembly_FragmentPathValuesAreChecked(t *testing.T) {
	base := t.TempDir()
	writeTestFile(t, filepath.Join(base, "secret.yaml"), "fragment_type: text\nversion: v1\ncontent: \"SECRET\"\n")
	writeTestFile(t, filepath.Join(base, "fragments", "us.yaml"), "fragment_type: text\nversion: v1\ncontent: \"US\"\n")
	writeTestFile(t, filepath.Join(base, "fragments", "fr.yaml"), "fragment_type: text\nversion: v1\ncontent: \"FR\"\n")
	writeTestFile(t, filepath.Join(base, "prompts", "chat.yaml"), `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: chat
spec:
  task_type: chat
  version: 1.0.0
  description: chat
  system_template: "Support. {{persona}}"
  fragments:
    - name: persona
      path: ../fragments/{{region}}.yaml
      required: false
  variables:
    - name: region
      required: true
      validation:
        enum: [us, uk]
    - name: tone
      required: false
`)
	reg := prompt.NewRegistryWithRepository(
		yamlrepo.NewYAMLPromptRepository(base, map[string]string{"chat": "prompts/chat.yaml"}))

	got, err := runAssembly(t, reg, nil, []variables.Provider{fixedVars{"region": "us"}})
	require.NoError(t, err)
	assert.Equal(t, "Support. US", got)

	bad := map[string]struct {
		static    map[string]string
		providers []variables.Provider
	}{
		"provider parent path": {providers: []variables.Provider{fixedVars{"region": "../secret"}}},
		"static parent path":   {static: map[string]string{"region": "../secret"}},
		"provider separator":   {providers: []variables.Provider{fixedVars{"region": "x/../../secret"}}},
		"provider absolute":    {providers: []variables.Provider{fixedVars{"region": filepath.Join(base, "secret")}}},
	}
	for name, tc := range bad {
		t.Run(name, func(t *testing.T) {
			got, err := runAssembly(t, reg, tc.static, tc.providers)
			assert.NotContains(t, got, "SECRET", "a variable value read a file outside the fragments")
			require.Error(t, err)
		})
	}

	t.Run("outside the declared enum", func(t *testing.T) {
		reg := prompt.NewRegistryWithRepository(
			yamlrepo.NewYAMLPromptRepository(base, map[string]string{"chat": "prompts/chat.yaml"}))
		_, err := runAssembly(t, reg, nil, []variables.Provider{fixedVars{"region": "fr"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"region"`)
	})
}

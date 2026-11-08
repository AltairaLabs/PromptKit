package stage_test

import (
	"context"
	"os"
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

type fixedVars map[string]string

func (fixedVars) Name() string                                         { return "fixed" }
func (f fixedVars) Provide(context.Context) (map[string]string, error) { return f, nil }

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// A fragment whose name a variable chooses resolves from the turn's variables,
// whichever source set it: the stage's own variables or a variable provider
// (the SDK's SetVar reaches the pipeline as one). It runs the stages in the
// order the SDK builds them, on a registry over YAML source files.
func TestPromptAssembly_DynamicFragmentResolvesFromTurnVariables(t *testing.T) {
	base := t.TempDir()
	writeTestFile(t, filepath.Join(base, "fragments", "persona_us.yaml"),
		"fragment_type: text\nversion: v1\ncontent: \"Use US spelling.\"\n")
	writeTestFile(t, filepath.Join(base, "prompts", "chat.yaml"), `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: chat
spec:
  task_type: chat
  version: 1.0.0
  description: chat
  system_template: "Support. {{persona_{{region}}}}"
  fragments:
    - name: persona_{{region}}
      path: ../fragments/persona_{{region}}.yaml
      required: true
  variables:
    - name: region
      required: true
`)
	reg := prompt.NewRegistryWithRepository(
		yamlrepo.NewYAMLPromptRepository(base, map[string]string{"chat": "prompts/chat.yaml"}))

	cases := map[string]struct {
		static    map[string]string
		providers []variables.Provider
	}{
		"static variables":  {static: map[string]string{"region": "us"}},
		"variable provider": {providers: []variables.Provider{fixedVars{"region": "us"}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ts := stage.NewTurnState()
			stages := []stage.Stage{
				stage.NewVariableProviderStageWithVarsAndTurnState(tc.static, tc.providers, ts),
				stage.NewPromptAssemblyStageWithTurnState(reg, "chat", tc.static, ts),
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
					errs <- st.Process(context.Background(), in, out)
				}(st, cur, out)
				cur = out
			}
			for range cur {
			}
			for range stages {
				require.NoError(t, <-errs)
			}
			assert.Equal(t, "Support. Use US spelling.", ts.SystemPrompt)
		})
	}
}

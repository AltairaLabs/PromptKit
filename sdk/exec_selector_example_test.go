package sdk

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// writeDescribedSkill writes a SKILL.md with the given description.
func writeDescribedSkill(t *testing.T, root, name, description string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\n\nFollow the policy.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644))
}

// The example exec selector (sdk/examples/selectors/exec_rerank/rerank.py),
// bound in a RuntimeConfig as the skills selector, reads the documented
// lowercase wire keys and narrows the skill__activate index to the skill that
// shares a word with the user's message. Only that skill becomes active.
func TestExecSelectorExample_NarrowsTheSkillIndex(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("examples", "selectors", "exec_rerank", "rerank.py"))
	require.NoError(t, err)
	require.FileExists(t, script)

	rc := writeRuntimeConfig(t, `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: exec-selector
spec:
  selectors:
    rerank:
      command: python3
      args: [`+strconv.Quote(script)+`]
      timeout_ms: 10000
  skills:
    selector: rerank
`)
	root := t.TempDir()
	writeDescribedSkill(t, root, "refund-processing", "Refund policy")
	writeDescribedSkill(t, root, "cancellations", "Cancellation policy")

	provider := &indexActivatingProvider{ToolProvider: mock.NewToolProvider("sel", "sel-model", false, nil)}
	conv, err := Open(writeWorkflowTestPack(t, selectorPackJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithSkillsDir(root),
		WithRuntimeConfig(rc),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	_, err = conv.Send(context.Background(), "refund please")
	require.NoError(t, err)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, "Activate a skill to load its instructions and tools.\n\n"+
		"Available skills:\n- refund-processing: Refund policy", provider.index)
	assert.Equal(t, []string{"refund-processing"}, provider.activated)
}

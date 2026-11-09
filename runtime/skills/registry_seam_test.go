package skills

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// TestSkillTools_ThroughToolRegistry registers the skill__ descriptors and the
// ToolExecutor in a tools.Registry and calls them the way a conversation does.
// The registry validates the model's arguments against the descriptors' input
// schemas and the executor's results against their output schemas, so this is
// where a disagreement between the descriptors and the executor would show.
func TestSkillTools_ThroughToolRegistry(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund", "invoice"})
	writeSkillWithTools(t, dir, "faq", "FAQ", "Answer from the FAQ.", nil)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "billing", "policy.md"), []byte("Refunds within 30 days."), 0o644))
	exec := newTestExecutor(t, dir, []string{"refund", "invoice"}, 0)

	reg := tools.NewRegistry()
	reg.RegisterExecutor(NewToolExecutor(exec))
	for _, d := range []*tools.ToolDescriptor{
		BuildSkillActivateDescriptor(), BuildSkillDeactivateDescriptor(), BuildSkillReadResourceDescriptor(),
	} {
		require.NoError(t, reg.Register(d))
	}
	set := NewActiveSet()
	ctx := WithActiveSet(context.Background(), set)

	res, err := reg.Execute(ctx, SkillActivateTool, json.RawMessage(`{"name":"billing"}`))
	require.NoError(t, err)
	require.Empty(t, res.Error, "the activate result must satisfy its output schema")
	assert.JSONEq(t, `{"instructions":"Handle billing.","added_tools":["invoice","refund"]}`, string(res.Result))
	assert.Equal(t, []string{"billing"}, set.Names())
	assert.ElementsMatch(t, []string{"refund", "invoice"}, exec.ToolsFor(set))

	res, err = reg.Execute(ctx, SkillReadResourceTool, json.RawMessage(`{"skill_name":"billing","path":"policy.md"}`))
	require.NoError(t, err)
	require.Empty(t, res.Error)
	assert.JSONEq(t, `{"content":"Refunds within 30 days."}`, string(res.Result))

	res, err = reg.Execute(ctx, SkillDeactivateTool, json.RawMessage(`{"name":"billing"}`))
	require.NoError(t, err)
	require.Empty(t, res.Error)
	var removed struct {
		RemovedTools []string `json:"removed_tools"`
	}
	require.NoError(t, json.Unmarshal(res.Result, &removed))
	assert.ElementsMatch(t, []string{"refund", "invoice"}, removed.RemovedTools)
	assert.Empty(t, set.Names())
	assert.Empty(t, exec.ToolsFor(set))

	// A skill that grants no tools still reports an (empty) array, which is
	// what the output schema promises.
	res, err = reg.Execute(ctx, SkillActivateTool, json.RawMessage(`{"name":"faq"}`))
	require.NoError(t, err)
	require.Empty(t, res.Error, "an empty added_tools must satisfy the output schema")
	assert.JSONEq(t, `{"instructions":"Answer from the FAQ.","added_tools":[]}`, string(res.Result))
	assert.Equal(t, []string{"faq"}, set.Names())

	res, err = reg.Execute(ctx, SkillActivateTool, json.RawMessage(`{"name":"nonexistent"}`))
	require.NoError(t, err, "an executor failure is reported in the tool result")
	assert.Contains(t, res.Error, "nonexistent")
	assert.Equal(t, []string{"faq"}, set.Names())

	_, err = reg.Execute(ctx, SkillActivateTool, json.RawMessage(`{}`))
	var verr *tools.ValidationError
	require.True(t, errors.As(err, &verr), "want *tools.ValidationError, got %v", err)
	assert.Contains(t, verr.Detail, "name is required")
}

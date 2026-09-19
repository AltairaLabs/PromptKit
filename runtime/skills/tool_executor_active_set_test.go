package skills

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

func activateViaTool(t *testing.T, te *ToolExecutor, ctx context.Context, name string) error {
	t.Helper()
	args, err := json.Marshal(map[string]string{"name": name})
	require.NoError(t, err)
	_, err = te.Execute(ctx, BuildSkillActivateDescriptor(), args)
	return err
}

// A single ToolExecutor -- which is what a tools.Registry holds, one per name --
// must route each conversation's activation into that conversation's own set.
//
// Without this, two concurrent conversations dispatching skill__activate through
// the same registry entry share one active set, and ProviderConfig.ToolGrants
// hands conversation A's skill tools to conversation B. See #2011.
func TestToolExecutor_ActivatesIntoTheContextActiveSet(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	writeSkillWithTools(t, dir, "shipping", "Shipping", "Handle shipping.", []string{"track"})
	exec := newTestExecutor(t, dir, []string{"refund", "track"}, 0)
	te := NewToolExecutor(exec)

	convA, convB := NewActiveSet(), NewActiveSet()
	ctxA := WithActiveSet(context.Background(), convA)
	ctxB := WithActiveSet(context.Background(), convB)

	require.NoError(t, activateViaTool(t, te, ctxA, "billing"))
	require.NoError(t, activateViaTool(t, te, ctxB, "shipping"))

	assert.Equal(t, []string{"refund"}, exec.ToolsFor(convA))
	assert.Equal(t, []string{"track"}, exec.ToolsFor(convB),
		"B must hold only what B activated")
	assert.Empty(t, exec.ActiveTools(),
		"neither activation belongs to the executor's own set")
}

// A set's filter is enforced on the tool path, not just the direct API. The
// filter used to arrive separately via a context value that had no producer, so
// every skill__activate ran unfiltered regardless of what SetFilter had said.
func TestToolExecutor_HonorsTheSetFilter(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)
	te := NewToolExecutor(exec)

	set := NewActiveSet()
	set.SetFilter("none")

	err := activateViaTool(t, te, WithActiveSet(context.Background(), set), "billing")
	require.Error(t, err, "a filtered-out skill must not activate through the tool path")
	assert.Empty(t, exec.ToolsFor(set))
}

// Deactivation routes to the same set activation used.
func TestToolExecutor_DeactivatesFromTheContextActiveSet(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)
	te := NewToolExecutor(exec)

	convA, convB := NewActiveSet(), NewActiveSet()
	ctxA := WithActiveSet(context.Background(), convA)
	require.NoError(t, activateViaTool(t, te, ctxA, "billing"))
	require.NoError(t, activateViaTool(t, te, WithActiveSet(context.Background(), convB), "billing"))

	args, err := json.Marshal(map[string]string{"name": "billing"})
	require.NoError(t, err)
	_, err = te.Execute(ctxA, BuildSkillDeactivateDescriptor(), args)
	require.NoError(t, err)

	assert.Empty(t, exec.ToolsFor(convA))
	assert.Equal(t, []string{"refund"}, exec.ToolsFor(convB),
		"A's deactivation must not revoke B's grant")
}

// With no set on the context the executor's own set is used, so hosts that
// never adopt WithActiveSet keep working exactly as before.
func TestToolExecutor_FallsBackToTheExecutorsOwnSet(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)
	te := NewToolExecutor(exec)

	require.NoError(t, activateViaTool(t, te, context.Background(), "billing"))
	assert.Equal(t, []string{"refund"}, exec.ActiveTools())
}

var _ tools.Executor = (*ToolExecutor)(nil)

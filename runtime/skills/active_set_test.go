package skills

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two conversations sharing one Executor must not see each other's activations.
//
// The active set used to be a field on the Executor, and the Executor is
// registered by name into a tools.Registry that holds exactly one per name. Any
// host running concurrent conversations over one registry -- which is what
// PromptArena does -- therefore had one active set for all of them. With
// ProviderConfig.ToolGrants reading that set (#1957/#1980), a skill activated
// in one conversation granted its tools in every other one: a permission leak,
// not merely stale state. See #2011.
func TestActivateIn_SetsAreIndependent(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	writeSkillWithTools(t, dir, "shipping", "Shipping", "Handle shipping.", []string{"track"})
	exec := newTestExecutor(t, dir, []string{"refund", "track"}, 0)

	convA, convB := NewActiveSet(), NewActiveSet()

	actA, err := exec.ActivateIn(convA, "billing")
	require.NoError(t, err)
	assert.Equal(t, "Handle billing.", actA.Instructions)
	assert.Equal(t, []string{"refund"}, actA.AddedTools)

	assert.Equal(t, []string{"refund"}, exec.ToolsFor(convA))
	assert.Empty(t, exec.ToolsFor(convB),
		"conversation B never activated a skill and must be granted nothing")

	_, err = exec.ActivateIn(convB, "shipping")
	require.NoError(t, err)
	assert.Equal(t, []string{"refund"}, exec.ToolsFor(convA),
		"B's activation must not reach A")
	assert.Equal(t, []string{"track"}, exec.ToolsFor(convB))
}

// Deactivating in one set leaves the other alone.
func TestDeactivateIn_SetsAreIndependent(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)

	convA, convB := NewActiveSet(), NewActiveSet()
	_, err := exec.ActivateIn(convA, "billing")
	require.NoError(t, err)
	_, err = exec.ActivateIn(convB, "billing")
	require.NoError(t, err)

	removed, err := exec.DeactivateIn(convA, "billing")
	require.NoError(t, err)
	assert.Equal(t, []string{"refund"}, removed)
	assert.Empty(t, exec.ToolsFor(convA))
	assert.Equal(t, []string{"refund"}, exec.ToolsFor(convB),
		"A's deactivation must not revoke B's grant")
}

// The max-active cap is per set, not per executor: one conversation filling its
// cap must not lock every other conversation out.
func TestActivateIn_MaxActiveIsPerSet(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	writeSkillWithTools(t, dir, "shipping", "Shipping", "Handle shipping.", []string{"track"})
	exec := newTestExecutor(t, dir, []string{"refund", "track"}, 1)

	convA, convB := NewActiveSet(), NewActiveSet()
	_, err := exec.ActivateIn(convA, "billing")
	require.NoError(t, err)

	_, err = exec.ActivateIn(convA, "shipping")
	require.Error(t, err, "A is at its cap of 1")

	_, err = exec.ActivateIn(convB, "shipping")
	require.NoError(t, err, "B has its own cap and has used none of it")
}

// The filter travels with the set, so one conversation's workflow state cannot
// narrow another's. This replaces Executor.SetFilter, which mutated shared
// state, and the WithSkillFilter context pair, which never had a producer.
func TestActiveSet_FilterIsPerSet(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)

	restricted := NewActiveSet()
	restricted.SetFilter("none")
	open := NewActiveSet()

	_, err := exec.ActivateIn(restricted, "billing")
	require.Error(t, err, `a set filtered to "none" activates nothing`)

	_, err = exec.ActivateIn(open, "billing")
	require.NoError(t, err, "the other conversation is unfiltered and unaffected")
}

// Narrowing a set's filter drops the skills that no longer match, and reports
// them, without touching any other set.
func TestActiveSet_SetFilterDeactivatesNonMatching(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)

	set := NewActiveSet()
	_, err := exec.ActivateIn(set, "billing")
	require.NoError(t, err)

	assert.Equal(t, []string{"billing"}, set.SetFilter("none"))
	assert.Empty(t, exec.ToolsFor(set))
}

// Concurrent activation on independent sets must be race-free: this is the
// shape arena actually runs, and the reason the state moved off the Executor.
func TestActivateIn_ConcurrentSetsAreRaceFree(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)

	const runs = 16
	sets := make([]*ActiveSet, runs)
	var wg sync.WaitGroup
	for i := range sets {
		sets[i] = NewActiveSet()
		wg.Add(1)
		go func(s *ActiveSet) {
			defer wg.Done()
			_, _ = exec.ActivateIn(s, "billing")
			_ = exec.ToolsFor(s)
		}(sets[i])
	}
	wg.Wait()

	for i, s := range sets {
		assert.Equal(t, []string{"refund"}, exec.ToolsFor(s), "set %d", i)
	}
}

// An ActiveSet reports its own filter and size.
func TestActiveSet_ReportsFilterAndLen(t *testing.T) {
	set := NewActiveSet()
	assert.Empty(t, set.Filter())
	assert.Equal(t, 0, set.Len())
	assert.Empty(t, set.Names())

	assert.Empty(t, set.SetFilter("skills/billing/*"),
		"nothing was active, so nothing was deactivated")
	assert.Equal(t, "skills/billing/*", set.Filter())
}

// SkillsIn names what the set holds, and the deprecated stateful accessors read
// the executor's own set so existing hosts see no change.
func TestSkillsIn_AndTheDeprecatedAccessorsAgree(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)

	set := NewActiveSet()
	_, err := exec.ActivateIn(set, "billing")
	require.NoError(t, err)
	assert.Equal(t, []string{"billing"}, exec.SkillsIn(set))
	assert.Empty(t, exec.ActiveSkills(), "the executor's own set is untouched")

	_, _, err = exec.Activate("billing") //nolint:staticcheck // exercising the deprecated path
	require.NoError(t, err)
	assert.Equal(t, []string{"billing"}, exec.ActiveSkills())
	assert.Same(t, exec.OwnActiveSet(), exec.OwnActiveSet())
}

// A nil set is the Executor's own set: hosts that never adopt ActiveSet keep
// the behavior they had.
func TestActivateIn_NilSetUsesTheExecutorsOwn(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "billing", "Billing", "Handle billing.", []string{"refund"})
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)

	_, err := exec.ActivateIn(nil, "billing")
	require.NoError(t, err)
	assert.Equal(t, []string{"refund"}, exec.ToolsFor(nil))
	assert.Equal(t, []string{"billing"}, exec.SkillsIn(nil))

	removed, err := exec.DeactivateIn(nil, "billing")
	require.NoError(t, err)
	assert.Equal(t, []string{"refund"}, removed)
}

// Activating an unknown skill fails, and deactivating one that was never
// activated fails, in the caller's set as in the executor's own.
func TestActivateIn_UnknownSkillAndInactiveDeactivate(t *testing.T) {
	dir := t.TempDir()
	exec := newTestExecutor(t, dir, []string{"refund"}, 0)
	set := NewActiveSet()

	_, err := exec.ActivateIn(set, "nope")
	require.Error(t, err)

	_, err = exec.DeactivateIn(set, "nope")
	require.ErrorContains(t, err, "not active")
}

// WithActiveSet ignores a nil set, and a context with none reports none, so the
// tool path's fallback is reachable rather than panicking.
func TestActiveSetContext_NilIsANoOp(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, ActiveSetFromContext(ctx))
	assert.Equal(t, ctx, WithActiveSet(ctx, nil), "a nil set must not wrap the context")
	//nolint:staticcheck // deliberately probing the nil-context guard
	assert.Nil(t, ActiveSetFromContext(nil))

	set := NewActiveSet()
	assert.Same(t, set, ActiveSetFromContext(WithActiveSet(ctx, set)))
}

package skills

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These cover #2326: the skill__activate index for one conversation lists only
// the skills its ActiveSet's filter admits — the same skills ActivateIn would
// let it activate.

func TestSkillIndexIn_ListsOnlyWhatTheFilterAdmits(t *testing.T) {
	root := t.TempDir()
	exec := writeBillingAndOrders(t, root)
	set := NewActiveSet()
	set.SetFilter(filepath.Join(root, "skills", "billing"))

	index, ok := exec.SkillIndexIn(set, nil)

	require.True(t, ok)
	assert.Equal(t, "Available skills:\n- pci: PCI", index)
}

func TestSkillIndexIn_NoFilterListsEverySkill(t *testing.T) {
	exec := writeBillingAndOrders(t, t.TempDir())

	index, ok := exec.SkillIndexIn(NewActiveSet(), nil)

	require.True(t, ok)
	assert.Equal(t, exec.SkillIndex(""), index, "an unfiltered set sees the registered index")
}

// "none", or a path no skill lies under, admits nothing: there is nothing to
// offer, so the caller withholds the tool rather than describing an empty set.
func TestSkillIndexIn_FilterAdmittingNothingIsNotOK(t *testing.T) {
	root := t.TempDir()
	exec := writeBillingAndOrders(t, root)

	for _, filter := range []string{"none", filepath.Join(root, "elsewhere")} {
		set := NewActiveSet()
		set.SetFilter(filter)
		_, ok := exec.SkillIndexIn(set, nil)
		assert.False(t, ok, "filter %q admits no skill", filter)
	}
}

// An empty catalog with no filter is still OK: the tool keeps its historical
// "No skills available." description rather than vanishing.
func TestSkillIndexIn_EmptyCatalogWithoutFilterIsOK(t *testing.T) {
	exec := newSelectorTestExecutor(t)

	index, ok := exec.SkillIndexIn(nil, nil)

	require.True(t, ok)
	assert.Equal(t, noSkillsIndex, index)
}

// The selector's choice narrows the admitted skills; a choice that shares
// nothing with them (made before a transition changed the filter) falls back
// to every admitted skill rather than an empty index.
func TestSkillIndexIn_SelectionNarrowsAndFallsBack(t *testing.T) {
	exec := newSelectorTestExecutor(t, "alpha", "beta", "gamma")
	set := NewActiveSet()

	index, ok := exec.SkillIndexIn(set, []string{"beta"})
	require.True(t, ok)
	assert.Equal(t, "Available skills:\n- beta: desc-beta", index)

	index, ok = exec.SkillIndexIn(set, []string{"not-a-skill"})
	require.True(t, ok)
	assert.Equal(t, exec.SkillIndex(""), index)
}

// A nil set means the Executor's own set, as for the other *In methods.
func TestSkillIndexIn_NilSetUsesTheExecutorsOwnSet(t *testing.T) {
	root := t.TempDir()
	exec := writeBillingAndOrders(t, root)
	exec.OwnActiveSet().SetFilter(filepath.Join(root, "skills", "orders"))

	index, ok := exec.SkillIndexIn(nil, nil)

	require.True(t, ok)
	assert.Equal(t, "Available skills:\n- track: Track", index)
	assert.Nil(t, exec.SelectSkillsIn(context.Background(), "q", nil), "no selector configured")
}

// The deprecated context-carried filter round-trips, and reads "" when unset.
func TestDeprecatedSkillFilterContext(t *testing.T) {
	assert.Empty(t, SkillFilterFromContext(context.Background()))
	ctx := WithSkillFilter(context.Background(), "skills/billing/*")
	assert.Equal(t, "skills/billing/*", SkillFilterFromContext(ctx))
}

// The deprecated ActivateWithFilter judges admission by the filter it is
// given, activates into the Executor's own set, and leaves that set's own
// filter as it found it.
func TestDeprecatedActivateWithFilter(t *testing.T) {
	root := t.TempDir()
	exec := writeBillingAndOrders(t, root)
	exec.OwnActiveSet().SetFilter("prior")

	_, _, err := exec.ActivateWithFilter("pci", filepath.Join(root, "skills", "orders"))
	require.ErrorContains(t, err, "not available in the current state")

	instructions, _, err := exec.ActivateWithFilter("pci", filepath.Join(root, "skills", "billing"))
	require.NoError(t, err)
	assert.Equal(t, "PCI rules.", instructions)
	assert.Equal(t, []string{"pci"}, exec.ActiveSkills())
	assert.Equal(t, "prior", exec.OwnActiveSet().Filter())
}

// The selector ranks only the skills the set admits, and nil means "no
// narrowing" — no selector, no query, or a choice of everything.
func TestSelectSkillsIn(t *testing.T) {
	exec := newSelectorTestExecutor(t, "alpha", "beta", "gamma")
	set := NewActiveSet()

	assert.Nil(t, exec.SelectSkillsIn(context.Background(), "q", set), "no selector")

	sel := &fakeSelector{name: "s", selected: []string{"beta"}}
	exec.SetNewSelector(sel)
	assert.Nil(t, exec.SelectSkillsIn(context.Background(), "", set), "no query")
	assert.Equal(t, []string{"beta"}, exec.SelectSkillsIn(context.Background(), "q", set))

	sel.selected = []string{"alpha", "beta", "gamma"}
	assert.Nil(t, exec.SelectSkillsIn(context.Background(), "q", set), "a choice of everything narrows nothing")
}

package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeBillingAndOrders lays out skills/billing/pci and skills/orders/track
// under root and returns an executor over root/skills built the way the SDK
// builds one: no ConfigDir, so a skill's filter path is its on-disk path.
func writeBillingAndOrders(t *testing.T, root string) *Executor {
	t.Helper()
	writeSkillWithTools(t, filepath.Join(root, "skills", "billing"), "pci", "PCI", "PCI rules.", nil)
	writeSkillWithTools(t, filepath.Join(root, "skills", "orders"), "track", "Track", "Tracking.", nil)
	return newTestExecutor(t, filepath.Join(root, "skills"), nil, 0)
}

// The PromptPack spec defines a workflow state's `skills` as "a path to a
// skill directory/file that scopes which skills are available in this state",
// with "./skills/billing" as its example. A path filter admits the skills under
// that directory and refuses the rest; before #2314 it was matched only as a
// glob, so the spec's own example admitted nothing.
func TestActivateIn_PathFilterAdmitsOnlySkillsUnderTheDirectory(t *testing.T) {
	root := t.TempDir()
	exec := writeBillingAndOrders(t, root)

	set := NewActiveSet()
	set.SetFilter(filepath.Join(root, "skills", "billing"))

	_, err := exec.ActivateIn(set, "pci")
	require.NoError(t, err, "pci lives under skills/billing")

	_, err = exec.ActivateIn(set, "track")
	require.ErrorContains(t, err, "not available in the current state")
}

// A relative path resolves the way a pack's skill sources do (against the
// working directory), so "./skills/billing" in a state names the same directory
// "./skills" in the pack's skills list does.
func TestActivateIn_RelativePathFilterResolvesLikeSkillSources(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeSkillWithTools(t, filepath.Join("skills", "billing"), "pci", "PCI", "PCI rules.", nil)
	writeSkillWithTools(t, filepath.Join("skills", "orders"), "track", "Track", "Tracking.", nil)
	exec := newTestExecutor(t, "./skills", nil, 0)

	set := NewActiveSet()
	set.SetFilter("./skills/billing")

	_, err := exec.ActivateIn(set, "pci")
	require.NoError(t, err)
	_, err = exec.ActivateIn(set, "track")
	require.Error(t, err)
}

// With a ConfigDir, skill paths are relative to it, and so is the filter.
func TestActivateIn_PathFilterRelativeToConfigDir(t *testing.T) {
	root := t.TempDir()
	writeSkillWithTools(t, filepath.Join(root, "skills", "billing"), "pci", "PCI", "PCI rules.", nil)
	writeSkillWithTools(t, filepath.Join(root, "skills", "orders"), "track", "Track", "Tracking.", nil)
	reg := NewRegistry()
	require.NoError(t, reg.Discover([]SkillSource{{Dir: filepath.Join(root, "skills")}}))
	exec := NewExecutor(ExecutorConfig{Registry: reg, ConfigDir: root})

	set := NewActiveSet()
	set.SetFilter("skills/billing")

	_, err := exec.ActivateIn(set, "pci")
	require.NoError(t, err)
	_, err = exec.ActivateIn(set, "track")
	require.Error(t, err)
}

// The spec allows the path to name a single skill, as its directory or its
// SKILL.md file. Either admits that skill alone.
func TestActivateIn_PathFilterNamingOneSkill(t *testing.T) {
	root := t.TempDir()
	writeSkillWithTools(t, filepath.Join(root, "skills", "billing"), "pci", "PCI", "PCI rules.", nil)
	writeSkillWithTools(t, filepath.Join(root, "skills", "billing"), "refunds", "Refunds", "Refunds.", nil)
	exec := newTestExecutor(t, filepath.Join(root, "skills"), nil, 0)

	for _, filter := range []string{
		filepath.Join(root, "skills", "billing", "pci"),
		filepath.Join(root, "skills", "billing", "pci", skillMDFile),
	} {
		set := NewActiveSet()
		set.SetFilter(filter)
		_, err := exec.ActivateIn(set, "pci")
		require.NoError(t, err, "filter %q", filter)
		_, err = exec.ActivateIn(set, "refunds")
		require.Error(t, err, "filter %q names pci only", filter)
	}
}

// Directory containment is by path segment, not string prefix: skills/bill
// does not contain skills/billing.
func TestActivateIn_PathFilterMatchesWholeSegments(t *testing.T) {
	root := t.TempDir()
	exec := writeBillingAndOrders(t, root)

	set := NewActiveSet()
	set.SetFilter(filepath.Join(root, "skills", "bill"))

	_, err := exec.ActivateIn(set, "pci")
	require.Error(t, err)
}

// Narrowing to a path deactivates the active skills outside it.
func TestActiveSet_SetFilterPathDeactivatesSkillsOutsideIt(t *testing.T) {
	root := t.TempDir()
	exec := writeBillingAndOrders(t, root)

	set := NewActiveSet()
	_, err := exec.ActivateIn(set, "pci")
	require.NoError(t, err)
	_, err = exec.ActivateIn(set, "track")
	require.NoError(t, err)

	dropped := set.SetFilter(filepath.Join(root, "skills", "billing"))
	assert.Equal(t, []string{"track"}, dropped)
	assert.Equal(t, []string{"pci"}, set.Names())
}

// An inline skill has no directory, so no path contains it.
func TestActivateIn_PathFilterRefusesInlineSkills(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Discover([]SkillSource{{Name: "tone", Description: "d", Instructions: "i"}}))
	exec := NewExecutor(ExecutorConfig{Registry: reg})

	set := NewActiveSet()
	set.SetFilter(string(os.PathSeparator))
	_, err := exec.ActivateIn(set, "tone")
	require.Error(t, err)
}

// A glob filter keeps its glob semantics.
func TestMatchesFilter_GlobStillMatchesAsAGlob(t *testing.T) {
	assert.True(t, matchesFilter("skills/billing/pci", "skills/billing/*"))
	assert.False(t, matchesFilter("skills/orders/track", "skills/billing/*"))
	assert.False(t, matchesFilter("skills/billing/pci", "skills/*"),
		"a glob is not also read as a directory")
}

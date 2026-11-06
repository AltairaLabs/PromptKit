package sdk

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

func initPreloadedCapability(t *testing.T, names ...string) *SkillsCapability {
	t.Helper()
	c := NewSkillsCapability(preloadInlineSources(names...))
	require.NoError(t, c.Init(CapabilityContext{Pack: &pack.Pack{}, PromptName: "chat"}))
	return c
}

// Reading the preloaded instructions must never activate a skill. The seam lands
// a deactivation after rendering has started and just before that skill is read,
// the interleaving a concurrent skill__deactivate can produce. The skill must
// stay deactivated and its instructions must be left out (#2399).
func TestPreloadedInstructions_NeverReactivatesADeactivatedSkill(t *testing.T) {
	c := initPreloadedCapability(t, "alpha", "beta")
	set := c.NewActiveSet()
	require.Equal(t, []string{"alpha", "beta"}, set.Names())

	c.beforePreloadRead = func(name string) {
		if name == "beta" {
			_, err := c.executor.DeactivateIn(set, "beta")
			require.NoError(t, err)
		}
	}

	got := c.preloadedInstructions(set)
	require.Equal(t, "## Skill: alpha\n\ndo alpha", got)
	require.Equal(t, []string{"alpha"}, set.Names(),
		"reading instructions re-activated a skill the conversation had deactivated")
}

func TestPreloadedInstructions_SkipsInactiveAndKeepsActive(t *testing.T) {
	c := initPreloadedCapability(t, "alpha", "beta")
	set := c.NewActiveSet()
	_, err := c.executor.DeactivateIn(set, "alpha")
	require.NoError(t, err)

	got := c.preloadedInstructions(set)
	require.Equal(t, "## Skill: beta\n\ndo beta", got)
	require.Equal(t, []string{"beta"}, set.Names())
}

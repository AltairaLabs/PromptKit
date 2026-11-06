package sdk

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// Reading the preloaded instructions must never activate a skill. A skill the
// conversation deactivates while the instructions are being rendered stays
// deactivated, whatever the interleaving.
func TestPreloadedInstructions_NeverReactivatesADeactivatedSkill(t *testing.T) {
	names := make([]string, 0, 32)
	for i := range 32 {
		names = append(names, fmt.Sprintf("s%02d", i))
	}
	last := names[len(names)-1] // rendered last, so the widest window
	c := NewSkillsCapability(preloadInlineSources(names...))
	require.NoError(t, c.Init(CapabilityContext{Pack: &pack.Pack{}, PromptName: "chat"}))

	for iter := range 500 {
		set := c.NewActiveSet()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = c.preloadedInstructions(set) }()
		go func() { defer wg.Done(); _, _ = c.executor.DeactivateIn(set, last) }()
		wg.Wait()
		require.NotContains(t, set.Names(), last,
			"iteration %d: reading instructions re-activated a skill the conversation had deactivated", iter)
	}
}

func TestPreloadedInstructions_SkipsInactiveAndKeepsActive(t *testing.T) {
	c := NewSkillsCapability(preloadInlineSources("alpha", "beta"))
	require.NoError(t, c.Init(CapabilityContext{Pack: &pack.Pack{}, PromptName: "chat"}))
	set := c.NewActiveSet()
	_, err := c.executor.DeactivateIn(set, "alpha")
	require.NoError(t, err)

	got := c.preloadedInstructions(set)
	require.Equal(t, "## Skill: beta\n\ndo beta", got)
	require.Equal(t, []string{"beta"}, set.Names())
}

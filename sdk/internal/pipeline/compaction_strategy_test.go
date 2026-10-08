package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// The default compactor's budget follows the running provider, so a workflow
// handoff to a provider with a smaller window compacts for it (#2208). A
// host-supplied strategy is the host's, and is used as given.
func TestBuildCompactionStrategy_BudgetFollowsProvider(t *testing.T) {
	got, ok := buildCompactionStrategy(&Config{Provider: mock.NewProvider("p", "m", false)}).(*stage.ContextCompactor)
	require.True(t, ok)
	assert.True(t, got.BudgetFromProvider)
	assert.Equal(t, stage.DefaultBudgetTokens, got.BudgetTokens)

	custom := &stage.ContextCompactor{BudgetTokens: 42}
	assert.Same(t, custom, buildCompactionStrategy(&Config{CompactionStrategy: custom}))
}

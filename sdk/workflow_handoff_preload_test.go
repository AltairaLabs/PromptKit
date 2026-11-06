package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
)

// A preloaded skill's instructions are part of the system prompt on every
// round of a workflow conversation, including the round after a handoff and
// every round of a later Send. The handoff re-renders the state's prompt, and
// that render must carry the same suffix TemplateStage appends, or the
// comparison against the loaded prompt always differs and the suffix is
// replaced before the model sees it.
func TestWorkflowConversation_HandoffKeepsPreloadedSkillInstructions(t *testing.T) {
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	provider := &handoffSDKProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
	}

	wc, err := OpenWorkflow(packPath,
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithSkillSource(skills.SkillSource{
			Name:         "house-style",
			Description:  "House style",
			Instructions: "Always answer in haiku.",
			Preload:      true,
		}),
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, wc.Close()) }()

	ctx := context.Background()
	_, err = wc.Send(ctx, "I need help with my account")
	require.NoError(t, err)
	require.Equal(t, "processing", wc.CurrentState())

	_, err = wc.Send(ctx, "thanks, and one more thing")
	require.NoError(t, err)

	require.Len(t, provider.seenPrompts, 3, "two rounds on turn 1, one on turn 2")
	wantPrefixes := []string{"You gather information.", "You process requests.", "You process requests."}
	for i, prompt := range provider.seenPrompts {
		require.True(t, strings.HasPrefix(prompt, wantPrefixes[i]),
			"round %d ran on the wrong state's prompt: %q", i+1, prompt)
		require.Contains(t, prompt, "## Skill: house-style\n\nAlways answer in haiku.",
			"round %d lost the preloaded skill's instructions: %q", i+1, prompt)
	}
}

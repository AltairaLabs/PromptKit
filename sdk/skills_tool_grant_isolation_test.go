package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
)

// openSharedSkillsConv opens a conversation over a caller-supplied
// SkillsCapability, which is what a host does when it builds one capability and
// reuses the option -- the shape that leaks.
func openSharedSkillsConv(
	t *testing.T, provider *toolGrantProvider, cap *SkillsCapability,
) *Conversation {
	t.Helper()
	conv, err := Open(writeWorkflowTestPack(t, toolGrantPackJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithCapability(cap),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup_order", func(map[string]any) (any, error) { return "ok", nil })
	conv.OnTool("refund", func(map[string]any) (any, error) { return "ok", nil })
	return conv
}

// A skill activated in one conversation must not grant its tools in another,
// even when both were opened over the same SkillsCapability.
//
// WithCapability is public, and SkillsCapability built its executor -- which
// held the active set -- in Init. Two conversations over one capability shared
// that set, and since #1980 wired it into ProviderConfig.ToolGrants the
// consequence is a permission leak: conversation B is offered a tool only
// conversation A's skill unlocked. See #2011.
func TestSkillToolGrant_DoesNotLeakBetweenConversations(t *testing.T) {
	dir := writeToolGrantSkill(t)
	shared := NewSkillsCapability([]skills.SkillSource{{Dir: dir}})

	activator := newToolGrantProvider()
	convA := openSharedSkillsConv(t, activator, shared)

	// B never activates anything: round 1 answers with plain text.
	bystander := newToolGrantProvider()
	bystander.rounds = 1
	convB := openSharedSkillsConv(t, bystander, shared)

	_, err := convA.Send(context.Background(), "I need a refund for ORD-1")
	require.NoError(t, err)
	require.Contains(t, lastBuild(t, activator), "refund",
		"precondition: A's own activation must still grant the tool")

	_, err = convB.Send(context.Background(), "Just checking my order")
	require.NoError(t, err)

	for i, build := range bystander.buildsSeen() {
		assert.NotContains(t, build, "refund",
			"build %d: B activated no skill and must not be offered A's granted tool", i)
		assert.Contains(t, build, "lookup_order", "build %d: B keeps the prompt baseline", i)
	}
}

// A fork carries its own active skills: activating in the fork must not grant
// the tool in the parent it was forked from.
func TestSkillToolGrant_ForkDoesNotLeakIntoParent(t *testing.T) {
	provider := newToolGrantProvider()
	parent := openToolGrantConv(t, provider)

	fork, err := parent.Fork()
	require.NoError(t, err)
	t.Cleanup(func() { _ = fork.Close() })

	_, err = fork.Send(context.Background(), "I need a refund for ORD-1")
	require.NoError(t, err)

	assert.Empty(t, parent.skillToolGrants(),
		"the parent activated nothing and must hold no grant")
	assert.Equal(t, []string{"refund"}, fork.skillToolGrants(),
		"the fork holds what the fork activated")
}

func lastBuild(t *testing.T, p *toolGrantProvider) []string {
	t.Helper()
	builds := p.buildsSeen()
	require.NotEmpty(t, builds)
	return builds[len(builds)-1]
}

package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/skills"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// These cover #1957. A skill's allowed-tools are meant to extend the prompt's
// baseline tool set on activation, capped by the pack's tools (the documented
// three-level scoping: pack = ceiling, prompt = baseline, skill = additional).
// The skills executor computed all of that and the provider stage never read
// it, so the tools array the model saw was identical before and after
// activation. Every assertion here is on the descriptor list handed to
// BuildTooling — the provider's wire-format entry — not on executor state.

// toolGrantProvider records the tool names handed to BuildTooling on every
// build and plays a fixed script: the first provider round of the first Send
// activates the skill, the first round of a later Send may deactivate it, and
// every other round answers with text.
type toolGrantProvider struct {
	*mock.ToolProvider

	mu     sync.Mutex
	builds [][]string // tool names per BuildTooling call, in order
	rounds int        // PredictWithTools calls so far

	deactivateOnRound int // 1-based round on which to call skill__deactivate (0 = never)
}

func newToolGrantProvider() *toolGrantProvider {
	return &toolGrantProvider{ToolProvider: mock.NewToolProvider("grant", "grant-model", false, nil)}
}

// Pin to the unary tool loop for determinism.
func (p *toolGrantProvider) SupportsStreaming() bool { return false }

func (p *toolGrantProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	names := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		names = append(names, d.Name)
	}
	p.mu.Lock()
	p.builds = append(p.builds, names)
	p.mu.Unlock()
	return p.ToolProvider.BuildTooling(descriptors)
}

func (p *toolGrantProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()

	switch {
	case round == 1:
		calls := []types.MessageToolCall{{
			ID: "call-activate", Name: "skill__activate", Args: []byte(`{"name":"refund-processing"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	case p.deactivateOnRound > 0 && round == p.deactivateOnRound:
		calls := []types.MessageToolCall{{
			ID: "call-deactivate", Name: "skill__deactivate", Args: []byte(`{"name":"refund-processing"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	default:
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
}

func (p *toolGrantProvider) buildsSeen() [][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]string, len(p.builds))
	copy(out, p.builds)
	return out
}

// toolGrantPackJSON declares two pack tools. Only lookup_order is in the
// prompt's baseline; refund is reachable solely through the skill.
const toolGrantPackJSON = `{
	"id": "skill-tool-grant-test",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a support agent.",
			"tools": ["lookup_order"]
		}
	},
	"tools": {
		"lookup_order": {
			"name": "lookup_order",
			"description": "Look up an order",
			"mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}
		},
		"refund": {
			"name": "refund",
			"description": "Issue a refund",
			"mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}
		}
	}
}`

// writeToolGrantSkill writes a refund-processing skill whose allowed-tools
// claim refund (a pack tool outside the prompt baseline) and not_in_pack (a
// name the pack does not declare, which the ceiling must drop).
func writeToolGrantSkill(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skillDir := filepath.Join(root, "refund-processing")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	body := "---\n" +
		"name: refund-processing\n" +
		"description: How to process refunds\n" +
		"allowed-tools:\n" +
		"  - refund\n" +
		"  - not_in_pack\n" +
		"---\n\nFollow the refund policy.\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644))
	return root
}

func openToolGrantConv(t *testing.T, provider *toolGrantProvider) *Conversation {
	t.Helper()
	conv, err := Open(writeWorkflowTestPack(t, toolGrantPackJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithSkillsDir(writeToolGrantSkill(t)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup_order", func(map[string]any) (any, error) { return "ok", nil })
	conv.OnTool("refund", func(map[string]any) (any, error) { return "ok", nil })
	return conv
}

// TestSkillToolGrant_ActivationExtendsToolsWithinTheSameSend pins the core
// contract: a tool claimed by a skill and declared by the pack is absent from
// the baseline request and present in the very next provider round after
// skill__activate returns — not one user turn later.
func TestSkillToolGrant_ActivationExtendsToolsWithinTheSameSend(t *testing.T) {
	provider := newToolGrantProvider()
	conv := openToolGrantConv(t, provider)

	_, err := conv.Send(context.Background(), "I need a refund for ORD-1")
	require.NoError(t, err)

	builds := provider.buildsSeen()
	require.GreaterOrEqual(t, len(builds), 2, "activation must trigger a rebuild of the tools array mid-Send")

	assert.Contains(t, builds[0], "lookup_order", "the prompt baseline is always offered")
	assert.NotContains(t, builds[0], "refund", "a skill-gated tool must not be offered before activation")

	last := builds[len(builds)-1]
	assert.Contains(t, last, "refund", "the round after skill__activate must offer the granted tool")
	assert.Contains(t, last, "lookup_order", "granting must not drop the baseline")
}

// TestSkillToolGrant_PersistsAcrossSends pins that the grant is a property of
// the active-skill set, not of the Send that activated it.
func TestSkillToolGrant_PersistsAcrossSends(t *testing.T) {
	provider := newToolGrantProvider()
	conv := openToolGrantConv(t, provider)

	_, err := conv.Send(context.Background(), "I need a refund for ORD-1")
	require.NoError(t, err)
	before := len(provider.buildsSeen())

	_, err = conv.Send(context.Background(), "Go ahead")
	require.NoError(t, err)

	builds := provider.buildsSeen()
	require.Greater(t, len(builds), before, "the second Send builds its own tools array")
	assert.Contains(t, builds[before], "refund", "the skill is still active, so its tool is still offered")
}

// TestSkillToolGrant_DeactivationRetractsTools pins the reverse: after
// skill__deactivate the next round no longer offers the tool, and the
// baseline is untouched.
func TestSkillToolGrant_DeactivationRetractsTools(t *testing.T) {
	provider := newToolGrantProvider()
	provider.deactivateOnRound = 3 // first round of the second Send
	conv := openToolGrantConv(t, provider)

	_, err := conv.Send(context.Background(), "I need a refund for ORD-1")
	require.NoError(t, err)
	_, err = conv.Send(context.Background(), "Actually, never mind")
	require.NoError(t, err)

	builds := provider.buildsSeen()
	last := builds[len(builds)-1]
	assert.NotContains(t, last, "refund", "the round after skill__deactivate must retract the tool")
	assert.Contains(t, last, "lookup_order", "retracting a grant must not drop the baseline")
}

// TestSkillToolGrant_PackIsTheCeiling pins that a skill cannot grant a tool
// the pack does not declare, whatever its allowed-tools claim.
func TestSkillToolGrant_PackIsTheCeiling(t *testing.T) {
	provider := newToolGrantProvider()
	conv := openToolGrantConv(t, provider)

	_, err := conv.Send(context.Background(), "I need a refund for ORD-1")
	require.NoError(t, err)

	for i, build := range provider.buildsSeen() {
		assert.NotContains(t, build, "not_in_pack", "build %d offered a tool the pack does not declare", i)
	}
}

// TestSkillsCapability_CeilingIsPackToolsNotPromptTools pins the second half
// of #1957: the executor's ceiling must be the pack's declared tools. With the
// prompt's list as the ceiling, a skill could only ever "grant" a tool the
// model already had, so every grant was a no-op.
func TestSkillsCapability_CeilingIsPackToolsNotPromptTools(t *testing.T) {
	cap := NewSkillsCapability([]skills.SkillSource{{Dir: writeToolGrantSkill(t)}})

	p := &pack.Pack{Pack: packspec.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat", Tools: []string{"lookup_order"}}},
		Tools: map[string]*pack.Tool{
			"lookup_order": {Name: "lookup_order"},
			"refund":       {Name: "refund"},
		},
	}}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "chat"}))

	_, added, err := cap.Executor().Activate("refund-processing")
	require.NoError(t, err)
	assert.Equal(t, []string{"refund"}, added,
		"a pack tool outside the prompt baseline is grantable; a name the pack lacks is not")
}

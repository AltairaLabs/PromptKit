package sdk

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover #2355. Both assert on what the provider receives, not on
// capability or executor state.

// systemRecorder records the system prompt and the tool names of every
// provider round.
type systemRecorder struct {
	*mock.ToolProvider

	mu      sync.Mutex
	systems []string
	tools   [][]string
	descs   []string // skill__activate description per build
}

func newSystemRecorder() *systemRecorder {
	return &systemRecorder{ToolProvider: mock.NewToolProvider("rec", "rec-model", false, nil)}
}

// Pin to the unary tool loop for determinism.
func (p *systemRecorder) SupportsStreaming() bool { return false }

func (p *systemRecorder) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	names := make([]string, 0, len(descriptors))
	var activateDesc string
	for _, d := range descriptors {
		names = append(names, d.Name)
		if d.Name == "skill__activate" {
			activateDesc = d.Description
		}
	}
	p.mu.Lock()
	p.tools = append(p.tools, names)
	p.descs = append(p.descs, activateDesc)
	p.mu.Unlock()
	return p.ToolProvider.BuildTooling(descriptors)
}

func (p *systemRecorder) record(req providers.PredictionRequest) {
	p.mu.Lock()
	p.systems = append(p.systems, req.System)
	p.mu.Unlock()
}

func (p *systemRecorder) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req)
	return providers.PredictionResponse{Content: "ok"}, nil
}

func (p *systemRecorder) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.record(req)
	return providers.PredictionResponse{Content: "ok"}, nil, nil
}

func (p *systemRecorder) firstSystem(t *testing.T) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.NotEmpty(t, p.systems, "the provider was never called")
	return p.systems[0]
}

const skillsPlainPackJSON = `{
	"id": "skill-sources-test",
	"version": "1.0.0",
	"prompts": {"chat": {"id": "chat", "name": "Chat", "system_template": "You are a support agent."}}
}`

const skillsPackDeclaringSkillJSON = `{
	"id": "skill-sources-test",
	"version": "1.0.0",
	"prompts": {"chat": {"id": "chat", "name": "Chat", "system_template": "You are a support agent."}},
	"skills": [{"name": "from-pack", "description": "declared by the pack", "instructions": "PACK-SKILL-BODY"}]
}`

// TestPreload_InstructionsReachTheSystemPrompt: a skill marked preload must put
// its instructions in the system prompt the provider receives on the first turn.
func TestPreload_InstructionsReachTheSystemPrompt(t *testing.T) {
	provider := newSystemRecorder()
	conv, err := Open(writeWorkflowTestPack(t, skillsPlainPackJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithSkillSource(skills.SkillSource{
			Name: "greeter", Description: "greets", Instructions: "PRELOADED-BODY", Preload: true,
		}),
		WithSkillSource(skills.SkillSource{
			Name: "lazy", Description: "on demand", Instructions: "LAZY-BODY",
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	system := provider.firstSystem(t)
	assert.Contains(t, system, "You are a support agent.", "the prompt itself must survive")
	assert.Contains(t, system, "PRELOADED-BODY", "a preloaded skill's instructions belong in the system prompt")
	assert.NotContains(t, system, "LAZY-BODY", "a skill that is not preloaded stays on demand")
}

// TestWithSkillSource_MergesWithPackSkills: sources added in code must not be
// dropped when the pack declares skills of its own.
func TestWithSkillSource_MergesWithPackSkills(t *testing.T) {
	provider := newSystemRecorder()
	conv, err := Open(writeWorkflowTestPack(t, skillsPackDeclaringSkillJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithSkillSource(skills.SkillSource{
			Name: "from-code", Description: "added in code", Instructions: "CODE-SKILL-BODY", Preload: true,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.NotEmpty(t, provider.descs)
	index := provider.descs[0]
	assert.Contains(t, index, "from-pack", "the pack's skill must stay available")
	assert.Contains(t, index, "from-code", "a skill added in code must be merged in")
	assert.Contains(t, provider.systems[0], "CODE-SKILL-BODY",
		"the code-added preloaded skill must reach the system prompt")
}

// TestPreload_InstructionsLeaveThePromptWhenTheSkillIsDropped: a workflow
// filter that drops a preloaded skill takes its instructions out of the next
// turn's system prompt, along with its tools.
func TestPreload_InstructionsLeaveThePromptWhenTheSkillIsDropped(t *testing.T) {
	provider := newSystemRecorder()
	conv, err := Open(writeWorkflowTestPack(t, skillsPlainPackJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithSkillSource(skills.SkillSource{
			Name: "greeter", Description: "greets", Instructions: "PRELOADED-BODY", Preload: true,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)
	conv.applySkillFilter("closed", "none")
	_, err = conv.Send(context.Background(), "hello again")
	require.NoError(t, err)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.Len(t, provider.systems, 2)
	assert.Contains(t, provider.systems[0], "PRELOADED-BODY")
	assert.NotContains(t, provider.systems[1], "PRELOADED-BODY")
}

func TestEnsureSkillsCapability_MergesIntoExistingWithoutDuplicates(t *testing.T) {
	existing := NewSkillsCapability([]skills.SkillSource{{Name: "from-pack", Instructions: "x"}})
	cfg := &config{skillSources: []skills.SkillSource{{Name: "from-code", Instructions: "y"}}}

	caps := ensureSkillsCapability([]Capability{existing}, cfg)
	caps = ensureSkillsCapability(caps, cfg)

	require.Len(t, caps, 1)
	assert.Equal(t, []skills.SkillSource{
		{Name: "from-pack", Instructions: "x"},
		{Name: "from-code", Instructions: "y"},
	}, existing.sources)
}

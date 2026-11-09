package sdk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// grantsToolSelector is a SkillSelector that keeps the skills whose
// allowed-tools include tool.
type grantsToolSelector struct{ tool string }

func (s grantsToolSelector) Select(_ context.Context, _ string, available []skills.SkillMetadata) ([]string, error) {
	var names []string
	for _, sk := range available {
		if slices.Contains(sk.AllowedTools, s.tool) {
			names = append(names, sk.Name)
		}
	}
	return names, nil
}

// indexActivatingProvider activates, on its first round, every skill the
// skill__activate index lists, then answers in text. It records the skills it
// activated and the tool names offered on every round.
type indexActivatingProvider struct {
	*mock.ToolProvider

	mu        sync.Mutex
	index     string
	activated []string
	offered   [][]string
}

func (p *indexActivatingProvider) SupportsStreaming() bool { return false }

func (p *indexActivatingProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	names := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		names = append(names, d.Name)
		if d.Name == skills.SkillActivateTool {
			p.index = d.Description
		}
	}
	p.offered = append(p.offered, names)
	return p.ToolProvider.BuildTooling(descriptors)
}

func (p *indexActivatingProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.activated != nil {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	p.activated = []string{}
	var calls []types.MessageToolCall
	for _, line := range strings.Split(p.index, "\n") {
		name, _, ok := strings.Cut(strings.TrimPrefix(line, "- "), ":")
		if !ok || !strings.HasPrefix(line, "- ") {
			continue
		}
		args, _ := json.Marshal(map[string]string{"name": name})
		calls = append(calls, types.MessageToolCall{ID: "activate-" + name, Name: skills.SkillActivateTool, Args: args})
		p.activated = append(p.activated, name)
	}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

// selectorPackJSON declares lookup_order in the prompt and two tools that only
// a skill grants: refund and cancel_order.
const selectorPackJSON = `{
	"id": "skill-selector-allowed-tools", "version": "1.0.0",
	"prompts": {"chat": {"id": "chat", "name": "Chat", "system_template": "You are a support agent.",
		"tools": ["lookup_order"]}},
	"tools": {
		"lookup_order": {"name": "lookup_order", "description": "Look up an order", "mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}},
		"refund": {"name": "refund", "description": "Issue a refund", "mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}},
		"cancel_order": {"name": "cancel_order", "description": "Cancel an order", "mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}}
	}
}`

func writeGrantingSkill(t *testing.T, root, name string, tools ...string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := "---\nname: " + name + "\ndescription: " + name + " policy\nallowed-tools:\n"
	for _, tool := range tools {
		body += "  - " + tool + "\n"
	}
	body += "---\n\nFollow the policy.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644))
}

// A SkillSelector sees each skill's allowed-tools. One that keeps the skills
// granting refund narrows the index to refund-processing, so that is the only
// skill the model can see and activate: refund is offered after activation,
// and cancel_order, which only the other skill grants, never is.
func TestSkillSelector_ChoosesByAllowedTools(t *testing.T) {
	root := t.TempDir()
	writeGrantingSkill(t, root, "refund-processing", "refund")
	writeGrantingSkill(t, root, "cancellations", "cancel_order")

	provider := &indexActivatingProvider{ToolProvider: mock.NewToolProvider("sel", "sel-model", false, nil)}
	conv, err := Open(writeWorkflowTestPack(t, selectorPackJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithSkillsDir(root),
		WithSkillSelectorOption(grantsToolSelector{tool: "refund"}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	for _, tool := range []string{"lookup_order", "refund", "cancel_order"} {
		conv.OnTool(tool, func(map[string]any) (any, error) { return "ok", nil })
	}

	_, err = conv.Send(context.Background(), "I need a refund for ORD-1")
	require.NoError(t, err)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, "Activate a skill to load its instructions and tools.\n\nAvailable skills:\n- refund-processing: refund-processing policy", provider.index)
	assert.Equal(t, []string{"refund-processing"}, provider.activated)
	require.GreaterOrEqual(t, len(provider.offered), 2, "activation rebuilds the tools")
	last := provider.offered[len(provider.offered)-1]
	assert.Contains(t, last, "refund")
	assert.NotContains(t, last, "cancel_order")
}

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/integration/probes"
)

// memoryScope is a fixed scope used by override tests; the memory capability
// no-ops when user_id is empty (see capability_memory.go), so it must be
// non-empty for tools to actually register.
var memoryScope = map[string]string{"user_id": "override-test-user"}

// runWithMemoryAndRegistry opens a probed conversation with the memory
// capability wired up via a test-owned registry, so the test can inspect
// final descriptor state after capability + override registration runs.
func runWithMemoryAndRegistry(t *testing.T, extra ...sdk.Option) (*tools.Registry, *sdk.Conversation) {
	t.Helper()
	registry := tools.NewRegistry()
	store := memory.NewInMemoryStore()
	opts := []sdk.Option{
		sdk.WithToolRegistry(registry),
		sdk.WithMemory(store, memoryScope),
	}
	opts = append(opts, extra...)
	_, conv := probes.Run(t, probes.RunOptions{SDKOptions: opts})
	return registry, conv
}

// TestContract_ToolDescriptorOverride_PatchesDescription verifies a basic
// case: an override on memory__remember replaces the description string.
func TestContract_ToolDescriptorOverride_PatchesDescription(t *testing.T) {
	const customDesc = "OMNIA-CUSTOM: store with consent category."

	registry, conv := runWithMemoryAndRegistry(t,
		sdk.WithToolDescriptorOverride(memory.RememberToolName,
			func(d *tools.ToolDescriptor) { d.Description = customDesc }),
	)

	// Trigger pipeline build (which is where capabilities + overrides apply).
	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)

	desc := registry.Get(memory.RememberToolName)
	require.NotNil(t, desc, "memory__remember should be registered")
	assert.Equal(t, customDesc, desc.Description)
}

// TestContract_ToolDescriptorOverride_InputSchemaReplacement covers the
// schema path: the memory category use case from #1033.
func TestContract_ToolDescriptorOverride_InputSchemaReplacement(t *testing.T) {
	customSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"content":  {"type": "string"},
			"category": {"type": "string", "enum": ["memory:health", "memory:identity"]}
		},
		"required": ["content", "category"]
	}`)

	registry, conv := runWithMemoryAndRegistry(t,
		sdk.WithToolDescriptorOverride(memory.RememberToolName,
			func(d *tools.ToolDescriptor) { d.InputSchema = customSchema }),
	)

	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)

	desc := registry.Get(memory.RememberToolName)
	require.NotNil(t, desc)
	assert.Contains(t, string(desc.InputSchema), `"enum"`)
	assert.Contains(t, string(desc.InputSchema), `"memory:health"`)
}

// TestContract_ToolDescriptorOverride_ComposesInOrder verifies that two
// overrides for the same tool compose: the second sees the descriptor
// already mutated by the first.
func TestContract_ToolDescriptorOverride_ComposesInOrder(t *testing.T) {
	registry, conv := runWithMemoryAndRegistry(t,
		sdk.WithToolDescriptorOverride(memory.RememberToolName,
			func(d *tools.ToolDescriptor) { d.Description = "step1" }),
		sdk.WithToolDescriptorOverride(memory.RememberToolName,
			func(d *tools.ToolDescriptor) { d.Description += " | step2" }),
	)

	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)

	desc := registry.Get(memory.RememberToolName)
	require.NotNil(t, desc)
	assert.Equal(t, "step1 | step2", desc.Description)
}

// TestContract_ToolDescriptorOverride_MissingToolWarnsAndSkips ensures the
// override is tolerant of version skew. Naming a tool that doesn't exist
// must not break Send.
func TestContract_ToolDescriptorOverride_MissingToolWarnsAndSkips(t *testing.T) {
	registry, conv := runWithMemoryAndRegistry(t,
		sdk.WithToolDescriptorOverride("nonexistent__tool",
			func(d *tools.ToolDescriptor) { d.Description = "won't apply" }),
		sdk.WithToolDescriptorOverride(memory.RememberToolName,
			func(d *tools.ToolDescriptor) { d.Description = "this one applies" }),
	)

	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)

	// The valid override still applies.
	desc := registry.Get(memory.RememberToolName)
	require.NotNil(t, desc)
	assert.Equal(t, "this one applies", desc.Description)
	// The invalid override is silently absent.
	assert.Nil(t, registry.Get("nonexistent__tool"))
}

// TestContract_ToolDescriptorOverride_DoesNotAffectOtherTools confirms a
// patch on one descriptor doesn't leak into others — the cloning behavior
// in applyToolDescriptorOverrides matters for shared registry state.
func TestContract_ToolDescriptorOverride_DoesNotAffectOtherTools(t *testing.T) {
	registry, conv := runWithMemoryAndRegistry(t,
		sdk.WithToolDescriptorOverride(memory.RememberToolName,
			func(d *tools.ToolDescriptor) { d.Description = "PATCHED" }),
	)

	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)

	other := registry.Get(memory.RecallToolName)
	require.NotNil(t, other)
	assert.NotEqual(t, "PATCHED", other.Description,
		"recall tool description should be untouched")
	assert.Contains(t, strings.ToLower(other.Description), "search")
}

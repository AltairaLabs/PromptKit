package sdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestWithToolDescriptorOverride_RejectsEmptyName(t *testing.T) {
	c := &config{}
	err := WithToolDescriptorOverride("", func(*tools.ToolDescriptor) {})(c)
	require.ErrorIs(t, err, errEmptyToolOverrideName)
	assert.Empty(t, c.toolDescriptorOverrides)
}

func TestWithToolDescriptorOverride_RejectsNilFn(t *testing.T) {
	c := &config{}
	err := WithToolDescriptorOverride("memory__remember", nil)(c)
	require.ErrorIs(t, err, errNilToolOverrideFn)
	assert.Empty(t, c.toolDescriptorOverrides)
}

func TestWithToolDescriptorOverride_AppendsToConfigSlice(t *testing.T) {
	c := &config{}
	require.NoError(t, WithToolDescriptorOverride("a", func(*tools.ToolDescriptor) {})(c))
	require.NoError(t, WithToolDescriptorOverride("b", func(*tools.ToolDescriptor) {})(c))
	require.NoError(t, WithToolDescriptorOverride("a", func(*tools.ToolDescriptor) {})(c))

	require.Len(t, c.toolDescriptorOverrides, 3)
	assert.Equal(t, "a", c.toolDescriptorOverrides[0].name)
	assert.Equal(t, "b", c.toolDescriptorOverrides[1].name)
	assert.Equal(t, "a", c.toolDescriptorOverrides[2].name,
		"duplicate names are preserved so they compose in order")
}

func TestApplyToolDescriptorOverrides_PatchesDescriptor(t *testing.T) {
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "test__tool",
		Description: "original",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}))

	applyToolDescriptorOverrides(registry, []toolDescriptorOverride{
		{name: "test__tool", fn: func(d *tools.ToolDescriptor) { d.Description = "patched" }},
	})

	got := registry.Get("test__tool")
	require.NotNil(t, got)
	assert.Equal(t, "patched", got.Description)
}

func TestApplyToolDescriptorOverrides_SkipsMissingTool(t *testing.T) {
	registry := tools.NewRegistry()
	// Empty registry — nothing to override.
	applyToolDescriptorOverrides(registry, []toolDescriptorOverride{
		{name: "absent__tool", fn: func(d *tools.ToolDescriptor) { d.Description = "never" }},
	})

	assert.Nil(t, registry.Get("absent__tool"))
}

func TestApplyToolDescriptorOverrides_ClonesBeforePatch(t *testing.T) {
	registry := tools.NewRegistry()
	original := &tools.ToolDescriptor{
		Name:        "test__tool",
		Description: "original",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	require.NoError(t, registry.Register(original))

	applyToolDescriptorOverrides(registry, []toolDescriptorOverride{
		{name: "test__tool", fn: func(d *tools.ToolDescriptor) {
			d.Description = "patched"
			d.InputSchema = json.RawMessage(`{"type":"string"}`)
		}},
	})

	// The original struct passed to Register must not be mutated by the
	// override — clones isolate them. (Registry.Register stores a pointer,
	// so this also guards against accidental shared-state mutation.)
	assert.Equal(t, "original", original.Description)
	assert.JSONEq(t, `{"type":"object"}`, string(original.InputSchema))
}

func TestApplyToolDescriptorOverrides_ComposeOrder(t *testing.T) {
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "test__tool",
		Description: "base",
	}))

	applyToolDescriptorOverrides(registry, []toolDescriptorOverride{
		{name: "test__tool", fn: func(d *tools.ToolDescriptor) { d.Description = "x" }},
		{name: "test__tool", fn: func(d *tools.ToolDescriptor) { d.Description += "y" }},
		{name: "test__tool", fn: func(d *tools.ToolDescriptor) { d.Description += "z" }},
	})

	assert.Equal(t, "xyz", registry.Get("test__tool").Description)
}

func TestCloneToolDescriptor_NilInput(t *testing.T) {
	assert.Nil(t, cloneToolDescriptor(nil))
}

func TestCloneToolDescriptor_DeepCopiesSchemas(t *testing.T) {
	src := &tools.ToolDescriptor{
		Name:         "x",
		Description:  "d",
		InputSchema:  json.RawMessage(`{"a":1}`),
		OutputSchema: json.RawMessage(`{"b":2}`),
	}

	cp := cloneToolDescriptor(src)
	require.NotNil(t, cp)
	require.NotSame(t, src, cp)

	// Mutate clone's schemas; src must remain intact.
	cp.InputSchema[0] = '['
	cp.OutputSchema[0] = '['

	assert.Equal(t, byte('{'), src.InputSchema[0])
	assert.Equal(t, byte('{'), src.OutputSchema[0])
}

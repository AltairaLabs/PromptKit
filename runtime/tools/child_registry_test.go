package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tagExec answers with the tag it was built with, so a test can tell which
// instance handled a call.
type tagExec struct {
	name string
	tag  string
}

func (e *tagExec) Name() string { return e.name }

func (e *tagExec) Execute(
	_ context.Context, _ *ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	return json.Marshal(map[string]string{"tag": e.tag})
}

func mustRegister(t *testing.T, r *Registry, name, mode string) {
	t.Helper()
	require.NoError(t, r.Register(&ToolDescriptor{
		Name:        name,
		Description: name,
		Mode:        mode,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}))
}

func execTag(t *testing.T, r *Registry, tool string) string {
	t.Helper()
	res, err := r.Execute(context.Background(), tool, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Empty(t, res.Error)
	var decoded map[string]string
	require.NoError(t, json.Unmarshal(res.Result, &decoded))
	return decoded["tag"]
}

// A child registry owns its executors: registering one must not reach the
// parent, nor any sibling built from the same parent.
//
// A Registry keys executors by name and holds exactly one per name. A host that
// shares one registry across conversations therefore had each conversation's
// executors overwrite the last one's. A child per conversation makes that
// unrepresentable rather than merely avoided. See AltairaLabs/PromptKit#2011.
func TestChild_OwnsItsExecutors(t *testing.T) {
	parent := NewRegistry()
	mustRegister(t, parent, "do_thing", "local")
	parent.RegisterExecutor(&tagExec{name: "local", tag: "parent"})

	a := parent.Child()
	b := parent.Child()
	a.RegisterExecutor(&tagExec{name: "local", tag: "a"})
	b.RegisterExecutor(&tagExec{name: "local", tag: "b"})

	assert.Equal(t, "a", execTag(t, a, "do_thing"))
	assert.Equal(t, "b", execTag(t, b, "do_thing"), "a's registration must not reach b")
	assert.Equal(t, "parent", execTag(t, parent, "do_thing"),
		"neither child may overwrite the parent")
}

// A child inherits the parent's executors for names it has not registered
// itself, so a host that registered a custom executor on the registry it passed
// in still has it used.
func TestChild_InheritsParentExecutors(t *testing.T) {
	parent := NewRegistry()
	mustRegister(t, parent, "do_thing", "custom")
	parent.RegisterExecutor(&tagExec{name: "custom", tag: "parent"})

	child := parent.Child()
	child.RegisterExecutor(&tagExec{name: "local", tag: "child-local"})

	assert.Equal(t, "parent", execTag(t, child, "do_thing"),
		"an executor the child never registered comes from the parent")
}

// Descriptor lookup is live, not a snapshot: a tool registered on the parent
// AFTER the child was created is still visible to the child. A copy-at-creation
// child would go stale here.
func TestChild_SeesParentToolsRegisteredLater(t *testing.T) {
	parent := NewRegistry()
	child := parent.Child()

	mustRegister(t, parent, "late_tool", "local")

	got, err := child.GetTool("late_tool")
	require.NoError(t, err)
	assert.Equal(t, "late_tool", got.Name)
	assert.Contains(t, child.List(), "late_tool")
}

// Descriptors register THROUGH to the parent: a host that passes a registry in
// with WithToolRegistry reads it back to inspect and override the tool set.
//
// This is the deliberate half of the split. Isolating descriptors too would
// leave the host's registry empty and break that readback --
// sdk/integration/contract_tool_overrides_test.go asserts it, which is how the
// requirement surfaced. Executors are the half that must not be shared;
// descriptors are data the host asked to share by passing the registry in.
func TestChild_DescriptorsRegisterThroughToTheParent(t *testing.T) {
	parent := NewRegistry()
	child := parent.Child()

	mustRegister(t, child, "conversation_tool", "local")

	assert.Contains(t, parent.List(), "conversation_tool",
		"the host must be able to read back what the conversation registered")
	got, err := parent.GetTool("conversation_tool")
	require.NoError(t, err)
	assert.Equal(t, "local", got.Mode)
	assert.Contains(t, child.List(), "conversation_tool")
}

// A descriptor override applied through a child reaches the parent's copy, so
// a host's WithToolDescriptorOverride stays observable on the registry it
// supplied.
func TestChild_DescriptorOverrideIsVisibleToTheParent(t *testing.T) {
	parent := NewRegistry()
	mustRegister(t, parent, "do_thing", "local")

	child := parent.Child()
	require.NoError(t, child.Register(&ToolDescriptor{
		Name:        "do_thing",
		Description: "patched",
		Mode:        "local",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}))

	assert.Equal(t, "patched", parent.Get("do_thing").Description)
	assert.Equal(t, "patched", child.Get("do_thing").Description)
}

// Child() on a nil receiver returns a usable standalone registry, so callers
// need no nil check before branching on "did the host pass one in".
func TestChild_NilParentGivesAStandaloneRegistry(t *testing.T) {
	var parent *Registry
	child := parent.Child()

	require.NotNil(t, child)
	mustRegister(t, child, "do_thing", "local")
	child.RegisterExecutor(&tagExec{name: "local", tag: "solo"})
	assert.Equal(t, "solo", execTag(t, child, "do_thing"))
}

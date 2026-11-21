package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A descriptor registered with RegisterLocal belongs to that registry alone:
// a child's never reaches its parent or a sibling, each child resolves its
// own, and it is not listed. A descriptor the parent chain holds under the
// same name wins over it, even one registered later.
func TestRegisterLocal_IsScopedToTheRegistry(t *testing.T) {
	host := NewRegistry()
	a, b := host.Child(), host.Child()
	mineA := &ToolDescriptor{Name: "pick", Description: "A's", Mode: "client"}
	mineB := &ToolDescriptor{Name: "pick", Description: "B's", Mode: "client"}
	require.NoError(t, a.RegisterLocal(mineA))
	require.NoError(t, b.RegisterLocal(mineB))

	assert.Same(t, mineA, a.Get("pick"))
	assert.Same(t, mineB, b.Get("pick"))
	got, err := a.GetTool("pick")
	require.NoError(t, err)
	assert.Same(t, mineA, got)
	assert.Nil(t, host.Get("pick"), "never registered on the parent")
	assert.Empty(t, host.List())
	assert.Empty(t, a.List(), "not listed")
	assert.Empty(t, a.GetTools())
	assert.Equal(t, "", mineA.Namespace)

	hostTool := &ToolDescriptor{Name: "pick", Description: "host's", Mode: "live"}
	require.NoError(t, host.Register(hostTool))
	assert.Same(t, hostTool, a.Get("pick"), "the parent chain's descriptor wins")

	assert.True(t, a.UnregisterLocal("pick"))
	assert.False(t, a.UnregisterLocal("pick"))
	assert.Same(t, hostTool, a.Get("pick"))
	assert.Same(t, mineB, b.localTool("pick"))
	assert.Nil(t, a.localTool("pick"))

	forked := b.Fork()
	assert.Same(t, mineB, forked.localTool("pick"), "Fork keeps local descriptors")
	forked.UnregisterLocal("pick")
	assert.Same(t, mineB, b.localTool("pick"), "and copies them")
}

// RegisterLocal refuses what Register refuses.
func TestRegisterLocal_RefusesInvalidDescriptors(t *testing.T) {
	r := NewRegistry()
	assert.Error(t, r.RegisterLocal(nil))
	assert.Error(t, r.RegisterLocal(&ToolDescriptor{}))
}

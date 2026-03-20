package sdk

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalAgentExecutor_Name(t *testing.T) {
	exec := NewLocalAgentExecutor(nil)
	assert.Equal(t, "a2a", exec.Name())
}

func TestLocalAgentExecutor_Execute_UnknownMember(t *testing.T) {
	exec := NewLocalAgentExecutor(map[string]*Conversation{})

	desc := &tools.ToolDescriptor{Name: "nonexistent"}
	args := json.RawMessage(`{"query":"hello"}`)

	_, err := exec.Execute(context.Background(), desc, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent member: nonexistent")
}

func TestLocalAgentExecutor_Execute_InvalidArgs(t *testing.T) {
	exec := NewLocalAgentExecutor(map[string]*Conversation{
		"agent1": {},
	})

	desc := &tools.ToolDescriptor{Name: "agent1"}
	args := json.RawMessage(`{invalid json`)

	_, err := exec.Execute(context.Background(), desc, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse agent tool args")
}

func TestLocalAgentExecutor_Execute_SendError(t *testing.T) {
	// A closed conversation returns ErrConversationClosed on Send,
	// exercising the send path and timeout wrapping.
	closedConv := &Conversation{closed: true}
	exec := NewLocalAgentExecutor(map[string]*Conversation{
		"worker": closedConv,
	})

	desc := &tools.ToolDescriptor{Name: "a2a__worker"}
	args := json.RawMessage(`{"query":"hello"}`)

	_, err := exec.Execute(context.Background(), desc, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent worker failed")
}

func TestLocalAgentExecutor_Execute_WithDeadline(t *testing.T) {
	// When context already has a deadline, the executor should NOT add its own timeout.
	closedConv := &Conversation{closed: true}
	exec := NewLocalAgentExecutor(map[string]*Conversation{
		"worker": closedConv,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	desc := &tools.ToolDescriptor{Name: "worker"}
	args := json.RawMessage(`{"query":"hello"}`)

	_, err := exec.Execute(ctx, desc, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent worker failed")
}

func TestResolveMemberName(t *testing.T) {
	assert.Equal(t, "worker", resolveMemberName("a2a__worker"))
	assert.Equal(t, "agent", resolveMemberName("agent"))
	assert.Equal(t, "billing", resolveMemberName("a2a__billing"))
}

func TestCloseAll(t *testing.T) {
	conv1 := &Conversation{config: &config{}, handlers: make(map[string]ToolHandler)}
	conv2 := &Conversation{config: &config{}, handlers: make(map[string]ToolHandler)}
	members := map[string]*Conversation{
		"a": conv1,
		"b": conv2,
	}
	closeAll(members)
	assert.True(t, conv1.closed)
	assert.True(t, conv2.closed)
}

func TestCloseAll_Empty(t *testing.T) {
	// Should not panic on empty map
	closeAll(map[string]*Conversation{})
	closeAll(nil)
}

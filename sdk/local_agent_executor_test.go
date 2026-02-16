package sdk

import (
	"encoding/json"
	"testing"

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

	_, err := exec.Execute(desc, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent member: nonexistent")
}

func TestLocalAgentExecutor_Execute_InvalidArgs(t *testing.T) {
	exec := NewLocalAgentExecutor(map[string]*Conversation{
		"agent1": {},
	})

	desc := &tools.ToolDescriptor{Name: "agent1"}
	args := json.RawMessage(`{invalid json`)

	_, err := exec.Execute(desc, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse agent tool args")
}

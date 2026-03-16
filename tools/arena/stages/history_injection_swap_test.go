package stages

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryInjectionStage_SwappedRoles(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "Hello from user"},
		{Role: "assistant", Content: "Hello from assistant"},
		{Role: "user", Content: "Follow-up from user"},
	}

	s := NewHistoryInjectionStageSwapped(history)
	results := runStage(t, s, nil)

	require.Len(t, results, 3)
	assert.Equal(t, "assistant", results[0].Message.Role)
	assert.Equal(t, "Hello from user", results[0].Message.Content)

	assert.Equal(t, "user", results[1].Message.Role)
	assert.Equal(t, "Hello from assistant", results[1].Message.Content)

	assert.Equal(t, "assistant", results[2].Message.Role)
	assert.Equal(t, "Follow-up from user", results[2].Message.Content)
}

func TestHistoryInjectionStage_SwappedRoles_SystemUnchanged(t *testing.T) {
	history := []types.Message{
		{Role: "system", Content: "You are a bot"},
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hey"},
	}

	s := NewHistoryInjectionStageSwapped(history)
	results := runStage(t, s, nil)

	require.Len(t, results, 3)
	assert.Equal(t, "system", results[0].Message.Role)
	assert.Equal(t, "You are a bot", results[0].Message.Content)

	assert.Equal(t, "assistant", results[1].Message.Role)
	assert.Equal(t, "Hi", results[1].Message.Content)

	assert.Equal(t, "user", results[2].Message.Role)
	assert.Equal(t, "Hey", results[2].Message.Content)
}

func TestHistoryInjectionStage_SwappedRoles_ForwardsInput(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "Past message"},
	}

	s := NewHistoryInjectionStageSwapped(history)

	input := []stage.StreamElement{
		newTestMessageElement("user", "New message"),
	}

	results := runStage(t, s, input)

	require.Len(t, results, 2)
	// History message should be swapped
	assert.Equal(t, "assistant", results[0].Message.Role)
	assert.Equal(t, "Past message", results[0].Message.Content)
	// Forwarded input should NOT be swapped
	assert.Equal(t, "user", results[1].Message.Role)
	assert.Equal(t, "New message", results[1].Message.Content)
}

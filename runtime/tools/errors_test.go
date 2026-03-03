package tools

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrToolsPending_Error(t *testing.T) {
	t.Run("single pending tool", func(t *testing.T) {
		err := &ErrToolsPending{
			Pending: []PendingToolExecution{
				{ToolName: "get_location"},
			},
		}
		assert.Equal(t, "tools pending: get_location", err.Error())
	})

	t.Run("multiple pending tools", func(t *testing.T) {
		err := &ErrToolsPending{
			Pending: []PendingToolExecution{
				{ToolName: "get_location"},
				{ToolName: "read_contacts"},
			},
		}
		assert.Equal(t, "tools pending: get_location, read_contacts", err.Error())
	})

	t.Run("no pending tools", func(t *testing.T) {
		err := &ErrToolsPending{}
		assert.Equal(t, "tools pending: ", err.Error())
	})
}

func TestIsErrToolsPending(t *testing.T) {
	t.Run("matches ErrToolsPending", func(t *testing.T) {
		original := &ErrToolsPending{
			Pending: []PendingToolExecution{
				{CallID: "c1", ToolName: "get_location"},
			},
		}
		ep, ok := IsErrToolsPending(original)
		require.True(t, ok)
		assert.Len(t, ep.Pending, 1)
		assert.Equal(t, "get_location", ep.Pending[0].ToolName)
	})

	t.Run("matches wrapped ErrToolsPending", func(t *testing.T) {
		original := &ErrToolsPending{
			Pending: []PendingToolExecution{{ToolName: "tool_a"}},
		}
		wrapped := fmt.Errorf("outer: %w", original)
		ep, ok := IsErrToolsPending(wrapped)
		require.True(t, ok)
		assert.Equal(t, "tool_a", ep.Pending[0].ToolName)
	})

	t.Run("does not match other errors", func(t *testing.T) {
		ep, ok := IsErrToolsPending(fmt.Errorf("some other error"))
		assert.False(t, ok)
		assert.Nil(t, ep)
	})

	t.Run("does not match nil", func(t *testing.T) {
		ep, ok := IsErrToolsPending(nil)
		assert.False(t, ok)
		assert.Nil(t, ep)
	})
}

package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingResult(t *testing.T) {
	t.Run("empty result is not pending", func(t *testing.T) {
		result := PendingResult{}
		assert.False(t, result.IsPending())
	})

	t.Run("result with reason is pending", func(t *testing.T) {
		result := PendingResult{Reason: "high_value"}
		assert.True(t, result.IsPending())
	})

	t.Run("result with message is pending", func(t *testing.T) {
		result := PendingResult{Message: "Requires approval"}
		assert.True(t, result.IsPending())
	})

	t.Run("result with both is pending", func(t *testing.T) {
		result := PendingResult{
			Reason:  "sensitive",
			Message: "This action requires approval",
		}
		assert.True(t, result.IsPending())
	})
}

func TestPendingStore(t *testing.T) {
	t.Run("add and get", func(t *testing.T) {
		store := NewPendingStore()
		call := &PendingToolCall{
			ID:        "call-1",
			Name:      "test_tool",
			Arguments: map[string]any{"key": "value"},
			Reason:    "test",
		}

		store.Add(call)

		retrieved, ok := store.Get("call-1")
		assert.True(t, ok)
		assert.Equal(t, "test_tool", retrieved.Name)
		assert.Equal(t, "value", retrieved.Arguments["key"])
	})

	t.Run("get non-existent", func(t *testing.T) {
		store := NewPendingStore()
		_, ok := store.Get("non-existent")
		assert.False(t, ok)
	})

	t.Run("remove", func(t *testing.T) {
		store := NewPendingStore()
		store.Add(&PendingToolCall{ID: "call-1", Name: "tool1"})
		store.Add(&PendingToolCall{ID: "call-2", Name: "tool2"})

		assert.Equal(t, 2, store.Len())

		store.Remove("call-1")
		assert.Equal(t, 1, store.Len())

		_, ok := store.Get("call-1")
		assert.False(t, ok)
	})

	t.Run("list", func(t *testing.T) {
		store := NewPendingStore()
		store.Add(&PendingToolCall{ID: "call-1", Name: "tool1"})
		store.Add(&PendingToolCall{ID: "call-2", Name: "tool2"})

		calls := store.List()
		assert.Len(t, calls, 2)
	})

	t.Run("clear", func(t *testing.T) {
		store := NewPendingStore()
		store.Add(&PendingToolCall{ID: "call-1", Name: "tool1"})
		store.Add(&PendingToolCall{ID: "call-2", Name: "tool2"})

		store.Clear()
		assert.Equal(t, 0, store.Len())
	})
}

func TestPendingStoreResolve(t *testing.T) {
	t.Run("resolve successful", func(t *testing.T) {
		store := NewPendingStore()
		call := &PendingToolCall{
			ID:        "call-1",
			Name:      "test_tool",
			Arguments: map[string]any{"x": float64(10)},
			handler: func(args map[string]any) (any, error) {
				x := args["x"].(float64)
				return map[string]any{"result": x * 2}, nil
			},
		}
		store.Add(call)

		resolution, err := store.Resolve("call-1")
		require.NoError(t, err)
		assert.Equal(t, "call-1", resolution.ID)
		assert.False(t, resolution.Rejected)
		assert.Nil(t, resolution.Error)
		assert.NotNil(t, resolution.ResultJSON)

		// Verify call is removed
		_, ok := store.Get("call-1")
		assert.False(t, ok)
	})

	t.Run("resolve non-existent", func(t *testing.T) {
		store := NewPendingStore()
		_, err := store.Resolve("non-existent")
		assert.Error(t, err)
	})

	t.Run("resolve handler error", func(t *testing.T) {
		store := NewPendingStore()
		call := &PendingToolCall{
			ID:   "call-1",
			Name: "failing_tool",
			handler: func(_ map[string]any) (any, error) {
				return nil, assert.AnError
			},
		}
		store.Add(call)

		resolution, err := store.Resolve("call-1")
		require.NoError(t, err) // Resolution itself succeeds
		assert.Equal(t, assert.AnError, resolution.Error)
	})
}

func TestPendingStoreReject(t *testing.T) {
	t.Run("reject successful", func(t *testing.T) {
		store := NewPendingStore()
		store.Add(&PendingToolCall{ID: "call-1", Name: "test_tool"})

		resolution, err := store.Reject("call-1", "not authorized")
		require.NoError(t, err)
		assert.Equal(t, "call-1", resolution.ID)
		assert.True(t, resolution.Rejected)
		assert.Equal(t, "not authorized", resolution.RejectionReason)

		// Verify call is removed
		_, ok := store.Get("call-1")
		assert.False(t, ok)
	})

	t.Run("reject non-existent", func(t *testing.T) {
		store := NewPendingStore()
		_, err := store.Reject("non-existent", "reason")
		assert.Error(t, err)
	})
}

func TestAsyncToolHandler(t *testing.T) {
	t.Run("handler returning pending", func(t *testing.T) {
		handler := AsyncToolHandler(func(args map[string]any) PendingResult {
			amount := args["amount"].(float64)
			if amount > 100 {
				return PendingResult{
					Reason:  "high_value",
					Message: "Amount exceeds $100",
				}
			}
			return PendingResult{}
		})

		// High value - should be pending
		result := handler(map[string]any{"amount": float64(500)})
		assert.True(t, result.IsPending())
		assert.Equal(t, "high_value", result.Reason)

		// Low value - should not be pending
		result = handler(map[string]any{"amount": float64(50)})
		assert.False(t, result.IsPending())
	})
}

func TestPendingToolCall_SetHandler(t *testing.T) {
	t.Run("sets handler and can resolve", func(t *testing.T) {
		store := NewPendingStore()
		call := &PendingToolCall{
			ID:        "call-1",
			Name:      "test_tool",
			Arguments: map[string]any{"x": float64(10)},
		}

		// Set handler using public method
		call.SetHandler(func(args map[string]any) (any, error) {
			x := args["x"].(float64)
			return map[string]any{"result": x * 2}, nil
		})

		store.Add(call)

		resolution, err := store.Resolve("call-1")
		assert.NoError(t, err)
		assert.NotNil(t, resolution.Result)
	})
}

func TestResolvedStore(t *testing.T) {
	t.Run("new store is empty", func(t *testing.T) {
		store := NewResolvedStore()
		resolutions := store.PopAll()
		assert.Empty(t, resolutions)
	})

	t.Run("add and pop all", func(t *testing.T) {
		store := NewResolvedStore()

		res1 := &ToolResolution{ID: "res-1", Result: "result1"}
		res2 := &ToolResolution{ID: "res-2", Result: "result2"}

		store.Add(res1)
		store.Add(res2)

		resolutions := store.PopAll()
		assert.Len(t, resolutions, 2)
		assert.Equal(t, "res-1", resolutions[0].ID)
		assert.Equal(t, "res-2", resolutions[1].ID)

		// PopAll should clear the store
		resolutions = store.PopAll()
		assert.Empty(t, resolutions)
	})

	t.Run("add nil is safe", func(t *testing.T) {
		store := NewResolvedStore()
		store.Add(nil)
		resolutions := store.PopAll()
		assert.Len(t, resolutions, 1)
		assert.Nil(t, resolutions[0])
	})

	t.Run("concurrent access is safe", func(t *testing.T) {
		store := NewResolvedStore()
		done := make(chan bool)

		// Add from multiple goroutines
		for i := range 10 {
			go func(id int) {
				store.Add(&ToolResolution{ID: string(rune('0' + id))})
				done <- true
			}(i)
		}

		// Wait for all adds
		for range 10 {
			<-done
		}

		resolutions := store.PopAll()
		assert.Len(t, resolutions, 10)
	})

	t.Run("len returns count", func(t *testing.T) {
		store := NewResolvedStore()
		assert.Equal(t, 0, store.Len())

		store.Add(&ToolResolution{ID: "res-1"})
		assert.Equal(t, 1, store.Len())

		store.Add(&ToolResolution{ID: "res-2"})
		assert.Equal(t, 2, store.Len())

		store.PopAll()
		assert.Equal(t, 0, store.Len())
	})
}

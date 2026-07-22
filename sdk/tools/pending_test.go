package tools

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testConv = "conv-1"

func TestPendingResult(t *testing.T) {
	t.Run("empty result is not pending", func(t *testing.T) {
		assert.False(t, PendingResult{}.IsPending())
	})
	t.Run("result with reason is pending", func(t *testing.T) {
		assert.True(t, PendingResult{Reason: "high_value"}.IsPending())
	})
	t.Run("result with message is pending", func(t *testing.T) {
		assert.True(t, PendingResult{Message: "Requires approval"}.IsPending())
	})
}

func TestMemoryPendingStore_AddGetListClaim(t *testing.T) {
	ctx := context.Background()

	t.Run("add and get", func(t *testing.T) {
		store := NewMemoryPendingStore()
		defer store.Close()
		call := &PendingToolCall{
			ID: "call-1", ConversationID: testConv, Name: "test_tool",
			Arguments: map[string]any{"key": "value"}, Reason: "test",
		}
		require.NoError(t, store.Add(ctx, call))

		got, ok, err := store.Get(ctx, testConv, "call-1")
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "test_tool", got.Name)
		assert.Equal(t, "value", got.Arguments["key"])
	})

	t.Run("get non-existent", func(t *testing.T) {
		store := NewMemoryPendingStore()
		defer store.Close()
		_, ok, err := store.Get(ctx, testConv, "nope")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("list is scoped by conversation", func(t *testing.T) {
		store := NewMemoryPendingStore()
		defer store.Close()
		require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "a", ConversationID: "c1", Name: "t"}))
		require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "b", ConversationID: "c1", Name: "t"}))
		require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "c", ConversationID: "c2", Name: "t"}))

		c1, err := store.List(ctx, "c1")
		require.NoError(t, err)
		assert.Len(t, c1, 2)

		c2, err := store.List(ctx, "c2")
		require.NoError(t, err)
		assert.Len(t, c2, 1)
	})

	t.Run("claim removes and returns", func(t *testing.T) {
		store := NewMemoryPendingStore()
		defer store.Close()
		require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "call-1", ConversationID: testConv, Name: "t"}))

		got, ok, err := store.Claim(ctx, testConv, "call-1")
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "call-1", got.ID)

		_, present, err := store.Get(ctx, testConv, "call-1")
		require.NoError(t, err)
		assert.False(t, present, "claimed call must be gone")
	})

	t.Run("claim missing returns ok=false", func(t *testing.T) {
		store := NewMemoryPendingStore()
		defer store.Close()
		_, ok, err := store.Claim(ctx, testConv, "nope")
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

// TestMemoryPendingStore_ClaimSingleWinner is the concurrency invariant: N
// goroutines claiming the same id see exactly one ok=true.
func TestMemoryPendingStore_ClaimSingleWinner(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPendingStore()
	defer store.Close()
	require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "x", ConversationID: testConv, Name: "t"}))

	const racers = 32
	var winners atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, ok, err := store.Claim(ctx, testConv, "x"); err == nil && ok {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	assert.Equal(t, int32(1), winners.Load(), "exactly one claimer must win")
}

func TestResolveApproved(t *testing.T) {
	t.Run("runs handler as proposed", func(t *testing.T) {
		call := &PendingToolCall{ID: "c", Name: "t", Arguments: map[string]any{"x": float64(10)}}
		res := ResolveApproved(call, func(a map[string]any) (any, error) {
			return map[string]any{"result": a["x"].(float64) * 2}, nil
		}, nil)
		require.NoError(t, res.Error)
		assert.False(t, res.Edited)
		assert.NotNil(t, res.ResultJSON)
	})

	t.Run("merges overrides and flags edited without mutating original", func(t *testing.T) {
		var gotArgs map[string]any
		call := &PendingToolCall{
			ID: "c", Name: "send_message",
			Arguments: map[string]any{"to": "Dana", "body": "original", "channel": "SMS"},
		}
		res := ResolveApproved(call, func(a map[string]any) (any, error) {
			gotArgs = a
			return map[string]any{"sent": a["body"]}, nil
		}, map[string]any{"body": "edited"})

		assert.Equal(t, "edited", gotArgs["body"])
		assert.Equal(t, "Dana", gotArgs["to"])
		assert.Equal(t, "SMS", gotArgs["channel"])
		assert.True(t, res.Edited)
		assert.Equal(t, "edited", res.Arguments["body"])
		assert.Equal(t, "original", call.Arguments["body"], "original must not be mutated")
	})

	t.Run("handler error is captured on the resolution", func(t *testing.T) {
		call := &PendingToolCall{ID: "c", Name: "failing"}
		res := ResolveApproved(call, func(map[string]any) (any, error) {
			return nil, assert.AnError
		}, nil)
		assert.Equal(t, assert.AnError, res.Error)
	})
}

func TestResolveRejected(t *testing.T) {
	res := ResolveRejected("call-1", "not authorized")
	assert.Equal(t, "call-1", res.ID)
	assert.True(t, res.Rejected)
	assert.Equal(t, "not authorized", res.RejectionReason)
}

func TestAsyncToolHandler(t *testing.T) {
	handler := AsyncToolHandler(func(args map[string]any) PendingResult {
		if args["amount"].(float64) > 100 {
			return PendingResult{Reason: "high_value", Message: "Amount exceeds $100"}
		}
		return PendingResult{}
	})
	assert.True(t, handler(map[string]any{"amount": float64(500)}).IsPending())
	assert.False(t, handler(map[string]any{"amount": float64(50)}).IsPending())
}

func TestMemoryPendingStore_MaxEntries(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects when full", func(t *testing.T) {
		store := NewMemoryPendingStore(WithMaxPending(2))
		defer store.Close()
		require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "1", ConversationID: testConv, Name: "t1"}))
		require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "2", ConversationID: testConv, Name: "t2"}))
		assert.ErrorIs(t, store.Add(ctx, &PendingToolCall{ID: "3", ConversationID: testConv, Name: "t3"}),
			ErrPendingStoreFull)
	})

	t.Run("accepts after a claim frees space", func(t *testing.T) {
		store := NewMemoryPendingStore(WithMaxPending(2))
		defer store.Close()
		require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "1", ConversationID: testConv, Name: "t1"}))
		require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "2", ConversationID: testConv, Name: "t2"}))
		_, _, err := store.Claim(ctx, testConv, "1")
		require.NoError(t, err)
		require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "3", ConversationID: testConv, Name: "t3"}))
	})
}

func TestMemoryPendingStore_TTL(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := NewMemoryPendingStore(WithPendingTTL(1 * time.Minute))
	store.nowFunc = func() time.Time { return now }
	defer store.Close()

	require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "old", ConversationID: testConv, Name: "t1"}))

	store.nowFunc = func() time.Time { return now.Add(2 * time.Minute) }
	require.NoError(t, store.Add(ctx, &PendingToolCall{ID: "fresh", ConversationID: testConv, Name: "t2"}))
	store.removeExpired()

	_, oldOk, _ := store.Get(ctx, testConv, "old")
	assert.False(t, oldOk, "expired 'old' should be removed")
	_, freshOk, _ := store.Get(ctx, testConv, "fresh")
	assert.True(t, freshOk, "fresh entry should remain")
}

func TestMemoryPendingStore_Close(t *testing.T) {
	store := NewMemoryPendingStore()
	require.NoError(t, store.Close())
	require.NoError(t, store.Close(), "double close should not panic")

	select {
	case <-store.stopped:
	default:
		t.Error("stopped channel should be closed after Close()")
	}
}

func TestMemoryPendingStore_Options(t *testing.T) {
	store := NewMemoryPendingStore(WithPendingTTL(10*time.Second), WithMaxPending(50))
	defer store.Close()
	assert.Equal(t, 10*time.Second, store.ttl)
	assert.Equal(t, 50, store.maxPending)

	def := NewMemoryPendingStore()
	defer def.Close()
	assert.Equal(t, DefaultPendingTTL, def.ttl)
	assert.Equal(t, DefaultMaxPending, def.maxPending)
}

// TestMemoryPendingStore_GetReturnsCopy proves Get/List hand back a defensive
// copy: mutating the returned value must not corrupt the stored record.
func TestMemoryPendingStore_GetReturnsCopy(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPendingStore()
	defer store.Close()
	require.NoError(t, store.Add(ctx, &PendingToolCall{
		ID: "1", ConversationID: testConv, Name: "orig", Arguments: map[string]any{"k": "v"},
	}))

	got, ok, err := store.Get(ctx, testConv, "1")
	require.NoError(t, err)
	require.True(t, ok)
	got.Name = "mutated"
	got.Arguments["k"] = "tampered"

	again, _, err := store.Get(ctx, testConv, "1")
	require.NoError(t, err)
	assert.Equal(t, "orig", again.Name, "stored Name must be unaffected by caller mutation")
	assert.Equal(t, "v", again.Arguments["k"], "stored Arguments must be unaffected")
}

func TestResolvedStore(t *testing.T) {
	t.Run("add and pop all", func(t *testing.T) {
		store := NewResolvedStore()
		store.Add(&ToolResolution{ID: "res-1"})
		store.Add(&ToolResolution{ID: "res-2"})

		resolutions := store.PopAll()
		assert.Len(t, resolutions, 2)
		assert.Empty(t, store.PopAll(), "PopAll should clear the store")
	})

	t.Run("add nil is safe", func(t *testing.T) {
		store := NewResolvedStore()
		store.Add(nil)
		resolutions := store.PopAll()
		require.Len(t, resolutions, 1)
		assert.Nil(t, resolutions[0])
	})

	t.Run("concurrent access is safe", func(t *testing.T) {
		store := NewResolvedStore()
		var wg sync.WaitGroup
		for i := range 10 {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				store.Add(&ToolResolution{ID: string(rune('0' + id))})
			}(i)
		}
		wg.Wait()
		assert.Len(t, store.PopAll(), 10)
	})

	t.Run("len returns count", func(t *testing.T) {
		store := NewResolvedStore()
		assert.Equal(t, 0, store.Len())
		store.Add(&ToolResolution{ID: "res-1"})
		assert.Equal(t, 1, store.Len())
	})
}

func TestToolResolution_PartsField(t *testing.T) {
	text := "result text"
	res := &ToolResolution{
		ID: "call-1",
		Parts: []types.ContentPart{
			types.NewTextPart(text),
			types.NewImagePartFromData("base64data", "image/png", nil),
		},
	}
	require.Len(t, res.Parts, 2)
	assert.Equal(t, "text", res.Parts[0].Type)
	assert.Equal(t, "image/png", res.Parts[1].Media.MIMEType)
}

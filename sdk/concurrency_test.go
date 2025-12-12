package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestConcurrentConversations verifies that multiple conversations
// can run concurrently without interfering with each other.
func TestConcurrentConversations(t *testing.T) {
	const numGoroutines = 10
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			conv := newTestConversation()
			conv.SetVar("id", string(rune('A'+id)))

			// Each conversation should see its own variable
			assert.Equal(t, string(rune('A'+id)), conv.GetVar("id"))

			// Register a handler
			conv.OnTool("test", func(args map[string]any) (any, error) {
				return args, nil
			})
		}(i)
	}

	wg.Wait()
}

// TestConversationVariableIsolation verifies that variables set
// in one conversation don't affect others.
func TestConversationVariableIsolation(t *testing.T) {
	conv1 := newTestConversation()
	conv2 := newTestConversation()

	conv1.SetVar("name", "Alice")
	conv2.SetVar("name", "Bob")

	assert.Equal(t, "Alice", conv1.GetVar("name"))
	assert.Equal(t, "Bob", conv2.GetVar("name"))

	// Changing one shouldn't affect the other
	conv1.SetVar("name", "Charlie")
	assert.Equal(t, "Charlie", conv1.GetVar("name"))
	assert.Equal(t, "Bob", conv2.GetVar("name"))
}

// TestConcurrentSetVar verifies thread-safety of SetVar.
func TestConcurrentSetVar(t *testing.T) {
	conv := newTestConversation()
	const numGoroutines = 100
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := string(rune('A' + (id % 26)))
			conv.SetVar(key, "value")
			_ = conv.GetVar(key)
		}(i)
	}

	wg.Wait()
}

// TestConcurrentOnTool verifies thread-safety of OnTool registration.
func TestConcurrentOnTool(t *testing.T) {
	conv := newTestConversation()
	const numGoroutines = 50
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			toolName := "tool_" + string(rune('A'+id))
			conv.OnTool(toolName, func(args map[string]any) (any, error) {
				return id, nil
			})
		}(i)
	}

	wg.Wait()

	// Verify some tools were registered
	conv.handlersMu.RLock()
	assert.GreaterOrEqual(t, len(conv.handlers), 1)
	conv.handlersMu.RUnlock()
}

// TestForkIsolation verifies that forked conversations are fully independent.
func TestForkIsolation(t *testing.T) {
	original := newTestConversation()
	original.SetVar("branch", "original")
	original.OnTool("shared_tool", func(args map[string]any) (any, error) {
		return "original", nil
	})

	forked := original.Fork()

	// Both should start with the same variable
	assert.Equal(t, "original", original.GetVar("branch"))
	assert.Equal(t, "original", forked.GetVar("branch"))

	// Modify the forked conversation
	forked.SetVar("branch", "forked")

	// Original should be unchanged
	assert.Equal(t, "original", original.GetVar("branch"))
	assert.Equal(t, "forked", forked.GetVar("branch"))

	// Both have the shared tool
	original.handlersMu.RLock()
	_, origHas := original.handlers["shared_tool"]
	original.handlersMu.RUnlock()

	forked.handlersMu.RLock()
	_, forkHas := forked.handlers["shared_tool"]
	forked.handlersMu.RUnlock()

	assert.True(t, origHas)
	assert.True(t, forkHas)
}

// TestConcurrentFork verifies thread-safety of forking operations.
func TestConcurrentFork(t *testing.T) {
	original := newTestConversation()
	original.SetVar("base", "value")

	const numForks = 50
	forks := make([]*Conversation, numForks)
	var wg sync.WaitGroup

	for i := 0; i < numForks; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			forks[idx] = original.Fork()
			forks[idx].SetVar("fork_id", string(rune('A'+idx)))
		}(i)
	}

	wg.Wait()

	// Verify all forks have the base variable
	for _, fork := range forks {
		assert.Equal(t, "value", fork.GetVar("base"))
	}

	// Original is unchanged
	assert.Equal(t, "value", original.GetVar("base"))
	assert.Equal(t, "", original.GetVar("fork_id"))
}

// TestConcurrentMessagesAccess verifies thread-safety of Messages() access.
func TestConcurrentMessagesAccess(t *testing.T) {
	conv := newTestConversation()
	const numReaders = 50
	var wg sync.WaitGroup

	// Start readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < 100; j++ {
				_ = conv.Messages(ctx)
				time.Sleep(time.Microsecond)
			}
		}()
	}

	// Concurrent writes (simulated)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			conv.SetVar("counter", string(rune('0'+j)))
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()
}

// TestClosedConversationReturnsError verifies that operations on
// a closed conversation return appropriate errors.
func TestClosedConversationReturnsError(t *testing.T) {
	conv := newTestConversation()
	_ = conv.Close()

	_, err := conv.Send(context.Background(), "test")
	assert.Equal(t, ErrConversationClosed, err)
}

// TestConcurrentClose verifies thread-safety of Close().
func TestConcurrentClose(t *testing.T) {
	conv := newTestConversation()
	const numClosers = 10
	var wg sync.WaitGroup

	for i := 0; i < numClosers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = conv.Close()
		}()
	}

	wg.Wait()
	assert.True(t, conv.closed)
}

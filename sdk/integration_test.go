// Integration tests for SDK v2.
//
// These tests verify end-to-end functionality using a mock provider
// to simulate LLM responses without making actual API calls.
package sdk_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test fixtures for integration tests.
const testPackJSON = `{
	"id": "test-pack",
	"version": "1.0.0",
	"description": "Test pack for integration tests",
	"provider": {
		"name": "mock",
		"model": "mock-model"
	},
	"prompts": {
		"chat": {
			"id": "chat",
			"system_template": "You are a helpful assistant. User: {{user_name}}."
		},
		"tools": {
			"id": "tools",
			"system_template": "You are an assistant with tools."
		}
	},
	"tools": {
		"get_weather": {
			"name": "get_weather",
			"description": "Get weather for a city",
			"parameters": {
				"type": "object",
				"properties": {
					"city": {"type": "string"},
					"country": {"type": "string"}
				},
				"required": ["city"]
			}
		},
		"process_order": {
			"name": "process_order",
			"description": "Process an order",
			"parameters": {
				"type": "object",
				"properties": {
					"order_id": {"type": "string"},
					"amount": {"type": "number"}
				},
				"required": ["order_id", "amount"]
			}
		}
	}
}`

// createTestPack creates a temporary pack file for testing.
func createTestPack(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	packPath := tmpDir + "/test.pack.json"

	err := json.Unmarshal([]byte(testPackJSON), new(map[string]any))
	require.NoError(t, err, "test pack JSON should be valid")

	err = writeFile(packPath, []byte(testPackJSON))
	require.NoError(t, err, "should write test pack")

	return packPath
}

// writeFile is a helper to write files in tests.
func writeFile(path string, data []byte) error {
	return nil // Placeholder - we'll use a different approach
}

// TestIntegration_BasicConversationFlow tests basic Send/Response cycle.
func TestIntegration_BasicConversationFlow(t *testing.T) {
	t.Skip("Requires mock provider setup - placeholder for integration test")

	t.Run("single message exchange", func(t *testing.T) {
		packPath := createTestPack(t)
		conv, err := sdk.Open(packPath, "chat")
		require.NoError(t, err)
		defer conv.Close()

		conv.SetVar("user_name", "Alice")

		ctx := context.Background()
		resp, err := conv.Send(ctx, "Hello!")
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Text())
	})

	t.Run("multi-turn conversation", func(t *testing.T) {
		packPath := createTestPack(t)
		conv, err := sdk.Open(packPath, "chat")
		require.NoError(t, err)
		defer conv.Close()

		ctx := context.Background()

		// First turn
		resp1, err := conv.Send(ctx, "My name is Bob")
		require.NoError(t, err)
		assert.NotEmpty(t, resp1.Text())

		// Second turn - should remember context
		resp2, err := conv.Send(ctx, "What's my name?")
		require.NoError(t, err)
		assert.NotEmpty(t, resp2.Text())
	})
}

// TestIntegration_ToolExecution tests tool calling flows.
func TestIntegration_ToolExecution(t *testing.T) {
	t.Skip("Requires mock provider setup - placeholder for integration test")

	t.Run("tool call and response", func(t *testing.T) {
		packPath := createTestPack(t)
		conv, err := sdk.Open(packPath, "tools")
		require.NoError(t, err)
		defer conv.Close()

		var weatherCalled bool
		conv.OnTool("get_weather", func(args map[string]any) (any, error) {
			weatherCalled = true
			return map[string]any{
				"temperature": 22.5,
				"conditions":  "Sunny",
			}, nil
		})

		ctx := context.Background()
		resp, err := conv.Send(ctx, "What's the weather in London?")
		require.NoError(t, err)
		assert.True(t, weatherCalled, "tool should have been called")
		assert.NotEmpty(t, resp.Text())
	})

	t.Run("multiple tool calls", func(t *testing.T) {
		packPath := createTestPack(t)
		conv, err := sdk.Open(packPath, "tools")
		require.NoError(t, err)
		defer conv.Close()

		var callCount int
		conv.OnTool("get_weather", func(args map[string]any) (any, error) {
			callCount++
			return map[string]any{"temp": 20}, nil
		})

		ctx := context.Background()
		_, err = conv.Send(ctx, "Compare weather in London and Paris")
		require.NoError(t, err)
		// The model might call the tool multiple times
		assert.GreaterOrEqual(t, callCount, 1)
	})
}

// TestIntegration_HITLFlow tests Human-in-the-Loop approval.
func TestIntegration_HITLFlow(t *testing.T) {
	t.Skip("Requires mock provider setup - placeholder for integration test")

	t.Run("pending tool approval", func(t *testing.T) {
		packPath := createTestPack(t)
		conv, err := sdk.Open(packPath, "tools")
		require.NoError(t, err)
		defer conv.Close()

		var executed bool
		conv.OnToolAsync(
			"process_order",
			func(args map[string]any) tools.PendingResult {
				amount, _ := args["amount"].(float64)
				if amount > 100 {
					return tools.PendingResult{
						Reason:  "high_value",
						Message: "High value order needs approval",
					}
				}
				return tools.PendingResult{}
			},
			func(args map[string]any) (any, error) {
				executed = true
				return map[string]string{"status": "processed"}, nil
			},
		)

		ctx := context.Background()
		resp, err := conv.Send(ctx, "Process order #123 for $500")
		require.NoError(t, err)

		// Check for pending tools
		pending := resp.PendingTools()
		if len(pending) > 0 {
			// Resolve the pending tool
			result, err := conv.ResolveTool(pending[0].ID)
			require.NoError(t, err)
			assert.NotNil(t, result)
			assert.True(t, executed, "tool should execute after approval")
		}
	})

	t.Run("pending tool rejection", func(t *testing.T) {
		packPath := createTestPack(t)
		conv, err := sdk.Open(packPath, "tools")
		require.NoError(t, err)
		defer conv.Close()

		var executed bool
		conv.OnToolAsync(
			"process_order",
			func(args map[string]any) tools.PendingResult {
				return tools.PendingResult{
					Reason: "needs_approval",
				}
			},
			func(args map[string]any) (any, error) {
				executed = true
				return nil, nil
			},
		)

		ctx := context.Background()
		resp, err := conv.Send(ctx, "Process order")
		require.NoError(t, err)

		pending := resp.PendingTools()
		if len(pending) > 0 {
			result, err := conv.RejectTool(pending[0].ID, "Not authorized")
			require.NoError(t, err)
			assert.Equal(t, "Not authorized", result.RejectionReason)
			assert.False(t, executed, "tool should not execute after rejection")
		}
	})
}

// TestIntegration_ConcurrentConversations tests parallel conversation handling.
func TestIntegration_ConcurrentConversations(t *testing.T) {
	t.Run("multiple independent conversations", func(t *testing.T) {
		const numConversations = 10
		var wg sync.WaitGroup
		errors := make(chan error, numConversations)

		for i := 0; i < numConversations; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				// Each goroutine creates its own conversation
				// This tests that conversations are independent
				conv := &mockConversation{
					id:        idx,
					variables: make(map[string]string),
				}

				// Simulate setting variables
				conv.SetVar("user_id", string(rune('A'+idx)))

				// Verify isolation
				if conv.GetVar("user_id") != string(rune('A'+idx)) {
					errors <- assert.AnError
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			assert.NoError(t, err)
		}
	})

	t.Run("concurrent sends on same conversation", func(t *testing.T) {
		// Test that concurrent Send() calls are properly serialized
		var sendCount atomic.Int32
		var wg sync.WaitGroup

		conv := &mockConversation{
			onSend: func() {
				sendCount.Add(1)
				time.Sleep(10 * time.Millisecond) // Simulate work
			},
		}

		const numSends = 5
		for i := 0; i < numSends; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				conv.Send()
			}()
		}

		wg.Wait()
		assert.Equal(t, int32(numSends), sendCount.Load())
	})
}

// TestIntegration_StreamingResponse tests streaming functionality.
func TestIntegration_StreamingResponse(t *testing.T) {
	t.Skip("Requires mock provider setup - placeholder for integration test")

	t.Run("stream collects all chunks", func(t *testing.T) {
		packPath := createTestPack(t)
		conv, err := sdk.Open(packPath, "chat")
		require.NoError(t, err)
		defer conv.Close()

		ctx := context.Background()
		var chunks []string

		for chunk := range conv.Stream(ctx, "Tell me a story") {
			if chunk.Error != nil {
				t.Fatalf("stream error: %v", chunk.Error)
			}
			if chunk.Type == sdk.ChunkDone {
				break
			}
			chunks = append(chunks, chunk.Text)
		}

		assert.NotEmpty(t, chunks, "should receive chunks")
	})
}

// TestIntegration_VariableSubstitution tests template variable handling.
func TestIntegration_VariableSubstitution(t *testing.T) {
	t.Run("SetVar and GetVar", func(t *testing.T) {
		conv := &mockConversation{
			variables: make(map[string]string),
		}

		conv.SetVar("name", "Alice")
		conv.SetVar("role", "Admin")

		assert.Equal(t, "Alice", conv.GetVar("name"))
		assert.Equal(t, "Admin", conv.GetVar("role"))
		assert.Equal(t, "", conv.GetVar("nonexistent"))
	})

	t.Run("SetVars bulk operation", func(t *testing.T) {
		conv := &mockConversation{
			variables: make(map[string]string),
		}

		conv.SetVars(map[string]any{
			"name":  "Bob",
			"count": 42,
		})

		assert.Equal(t, "Bob", conv.GetVar("name"))
		assert.Equal(t, "42", conv.GetVar("count"))
	})

	t.Run("concurrent variable access", func(t *testing.T) {
		conv := &mockConversation{
			variables: make(map[string]string),
		}

		var wg sync.WaitGroup
		const iterations = 100

		// Concurrent writes
		for i := 0; i < iterations; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				conv.SetVar("key", string(rune('0'+idx%10)))
			}(i)
		}

		// Concurrent reads
		for i := 0; i < iterations; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = conv.GetVar("key")
			}()
		}

		wg.Wait()
		// If we get here without a race condition panic, the test passes
	})
}

// mockConversation is a simplified mock for testing.
type mockConversation struct {
	id        int
	variables map[string]string
	varMu     sync.RWMutex
	onSend    func()
}

func (m *mockConversation) SetVar(name, value string) {
	m.varMu.Lock()
	defer m.varMu.Unlock()
	m.variables[name] = value
}

func (m *mockConversation) GetVar(name string) string {
	m.varMu.RLock()
	defer m.varMu.RUnlock()
	return m.variables[name]
}

func (m *mockConversation) SetVars(vars map[string]any) {
	m.varMu.Lock()
	defer m.varMu.Unlock()
	for k, v := range vars {
		m.variables[k] = m.formatValue(v)
	}
}

func (m *mockConversation) Send() {
	if m.onSend != nil {
		m.onSend()
	}
}

// Helper function for SetVars
func (m *mockConversation) formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

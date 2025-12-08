// Benchmarks for SDK v2.
//
// Run with: go test -bench=. -benchmem ./sdk/...
package sdk_test

import (
	"sync"
	"testing"
)

// BenchmarkVariableAccess measures variable get/set performance.
func BenchmarkVariableAccess(b *testing.B) {
	b.Run("SetVar", func(b *testing.B) {
		conv := &benchConversation{
			variables: make(map[string]string),
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			conv.SetVar("key", "value")
		}
	})

	b.Run("GetVar", func(b *testing.B) {
		conv := &benchConversation{
			variables: map[string]string{"key": "value"},
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = conv.GetVar("key")
		}
	})

	b.Run("SetVar_Concurrent", func(b *testing.B) {
		conv := &benchConversation{
			variables: make(map[string]string),
		}
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				conv.SetVar("key", "value")
			}
		})
	})

	b.Run("GetVar_Concurrent", func(b *testing.B) {
		conv := &benchConversation{
			variables: map[string]string{"key": "value"},
		}
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = conv.GetVar("key")
			}
		})
	})
}

// BenchmarkToolHandlerRegistration measures handler registration overhead.
func BenchmarkToolHandlerRegistration(b *testing.B) {
	b.Run("OnTool_Single", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			conv := &benchConversation{
				handlers: make(map[string]func(map[string]any) (any, error)),
			}
			conv.OnTool("test", func(args map[string]any) (any, error) {
				return nil, nil
			})
		}
	})

	b.Run("OnTool_Multiple", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			conv := &benchConversation{
				handlers: make(map[string]func(map[string]any) (any, error)),
			}
			for j := 0; j < 10; j++ {
				name := string(rune('a' + j))
				conv.OnTool(name, func(args map[string]any) (any, error) {
					return nil, nil
				})
			}
		}
	})
}

// BenchmarkHandlerLookup measures tool handler lookup performance.
func BenchmarkHandlerLookup(b *testing.B) {
	conv := &benchConversation{
		handlers: make(map[string]func(map[string]any) (any, error)),
	}

	// Register 100 handlers
	for i := 0; i < 100; i++ {
		name := string(rune('a'+i/26)) + string(rune('a'+i%26))
		conv.OnTool(name, func(args map[string]any) (any, error) {
			return nil, nil
		})
	}

	b.Run("Lookup_First", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = conv.GetHandler("aa")
		}
	})

	b.Run("Lookup_Last", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = conv.GetHandler("dv")
		}
	})

	b.Run("Lookup_NotFound", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = conv.GetHandler("zz")
		}
	})
}

// BenchmarkToolExecution measures tool execution overhead.
func BenchmarkToolExecution(b *testing.B) {
	b.Run("Simple_Handler", func(b *testing.B) {
		handler := func(args map[string]any) (any, error) {
			return "result", nil
		}
		args := map[string]any{"key": "value"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = handler(args)
		}
	})

	b.Run("Map_Result_Handler", func(b *testing.B) {
		handler := func(args map[string]any) (any, error) {
			return map[string]any{
				"status": "ok",
				"count":  42,
			}, nil
		}
		args := map[string]any{"key": "value"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = handler(args)
		}
	})
}

// BenchmarkConversationCreation measures conversation initialization.
func BenchmarkConversationCreation(b *testing.B) {
	b.Run("New_Conversation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = &benchConversation{
				variables: make(map[string]string),
				handlers:  make(map[string]func(map[string]any) (any, error)),
			}
		}
	})

	b.Run("New_With_Variables", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			conv := &benchConversation{
				variables: make(map[string]string),
				handlers:  make(map[string]func(map[string]any) (any, error)),
			}
			conv.SetVar("name", "Alice")
			conv.SetVar("role", "User")
			conv.SetVar("context", "Testing")
		}
	})
}

// BenchmarkMessageHistory measures message history operations.
func BenchmarkMessageHistory(b *testing.B) {
	b.Run("AddMessage_100", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			history := make([]string, 0, 100)
			for j := 0; j < 100; j++ {
				history = append(history, "test message")
			}
		}
	})

	b.Run("AddMessage_1000", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			history := make([]string, 0, 1000)
			for j := 0; j < 1000; j++ {
				history = append(history, "test message")
			}
		}
	})
}

// benchConversation is a minimal mock for benchmarking.
type benchConversation struct {
	variables  map[string]string
	varMu      sync.RWMutex
	handlers   map[string]func(map[string]any) (any, error)
	handlersMu sync.RWMutex
}

func (c *benchConversation) SetVar(name, value string) {
	c.varMu.Lock()
	c.variables[name] = value
	c.varMu.Unlock()
}

func (c *benchConversation) GetVar(name string) string {
	c.varMu.RLock()
	v := c.variables[name]
	c.varMu.RUnlock()
	return v
}

func (c *benchConversation) OnTool(name string, handler func(map[string]any) (any, error)) {
	c.handlersMu.Lock()
	c.handlers[name] = handler
	c.handlersMu.Unlock()
}

func (c *benchConversation) GetHandler(name string) func(map[string]any) (any, error) {
	c.handlersMu.RLock()
	h := c.handlers[name]
	c.handlersMu.RUnlock()
	return h
}

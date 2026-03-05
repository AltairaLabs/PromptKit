package tools_test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// TestRegistryConcurrentRegisterAndGet verifies that concurrent Register and Get
// calls do not race (protected by the internal RWMutex).
func TestRegistryConcurrentRegisterAndGet(t *testing.T) {
	registry := tools.NewRegistry()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n * 2)

	// Concurrently register tools
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			name := "tool_" + string(rune('A'+idx%26)) + string(rune('0'+idx/26))
			desc := &tools.ToolDescriptor{
				Name:         name,
				Description:  "Test tool",
				InputSchema:  json.RawMessage(`{"type":"object"}`),
				OutputSchema: json.RawMessage(`{"type":"object"}`),
			}
			_ = registry.Register(desc)
		}(i)
	}

	// Concurrently read tools
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			name := "tool_" + string(rune('A'+idx%26)) + string(rune('0'+idx/26))
			_ = registry.Get(name)
			_ = registry.List()
			_ = registry.GetTools()
		}(i)
	}

	wg.Wait()

	// Verify at least some tools were registered
	if len(registry.List()) == 0 {
		t.Error("expected some tools to be registered")
	}
}

// TestRegistryConcurrentRegisterExecutor verifies concurrent executor registration.
func TestRegistryConcurrentRegisterExecutor(t *testing.T) {
	registry := tools.NewRegistry()

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func(idx int) {
			defer wg.Done()
			// Re-registering the same executors concurrently should be safe
			registry.RegisterExecutor(tools.NewMockStaticExecutor())
			registry.RegisterExecutor(tools.NewMockScriptedExecutor())
		}(i)
	}

	wg.Wait()
}

// TestMaxToolResultSizeOption verifies the WithMaxToolResultSize option.
func TestMaxToolResultSizeOption(t *testing.T) {
	t.Run("default is 1MB", func(t *testing.T) {
		registry := tools.NewRegistry()
		if registry.MaxToolResultSize() != tools.DefaultMaxToolResultSize {
			t.Errorf("expected %d, got %d", tools.DefaultMaxToolResultSize, registry.MaxToolResultSize())
		}
	})

	t.Run("custom value", func(t *testing.T) {
		registry := tools.NewRegistry(tools.WithMaxToolResultSize(512))
		if registry.MaxToolResultSize() != 512 {
			t.Errorf("expected 512, got %d", registry.MaxToolResultSize())
		}
	})

	t.Run("zero disables limit", func(t *testing.T) {
		registry := tools.NewRegistry(tools.WithMaxToolResultSize(0))
		if registry.MaxToolResultSize() != 0 {
			t.Errorf("expected 0, got %d", registry.MaxToolResultSize())
		}
	})
}

package tools_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
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

	// Every concurrent Register must land: no write may be lost.
	if got := len(registry.List()); got != n {
		t.Errorf("registered %d tools, want %d", got, n)
	}
}

// TestRegistryConcurrentRegisterExecutor re-registers the mock executors while
// other goroutines execute mock tools through the same registry, all released
// from one barrier. Every call must still be served by a mock executor and
// return the descriptor's result: a replacement mid-flight must never leave a
// name without an executor. Run under -race.
func TestRegistryConcurrentRegisterExecutor(t *testing.T) {
	registry := tools.NewRegistry()
	staticDesc := &tools.ToolDescriptor{
		Name: "static_tool", Mode: "mock", MockResult: json.RawMessage(`{"v":"static"}`),
	}
	scriptedDesc := &tools.ToolDescriptor{
		Name: "scripted_tool", Mode: "mock", MockTemplate: `{"v":"{{.who}}"}`,
	}
	if err := registry.Register(staticDesc); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(scriptedDesc); err != nil {
		t.Fatal(err)
	}

	const n = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan string, n*2)
	for i := range n {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			registry.RegisterExecutor(tools.NewMockStaticExecutor())
			registry.RegisterExecutor(tools.NewMockScriptedExecutor())
		}()
		go func(idx int) {
			defer wg.Done()
			<-start
			desc, args, want := staticDesc, `{}`, `{"v":"static"}`
			if idx%2 == 1 {
				desc, args, want = scriptedDesc, `{"who":"w"}`, `{"v":"w"}`
			}
			res, err := registry.Execute(context.Background(), desc.Name, json.RawMessage(args))
			switch {
			case err != nil:
				errs <- err.Error()
			case res.Error != "":
				errs <- res.Error
			case string(res.Result) != want:
				errs <- "got " + string(res.Result) + ", want " + want
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
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

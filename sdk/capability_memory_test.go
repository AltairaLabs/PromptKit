package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestMemoryCapability_RegisterTools(t *testing.T) {
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "test"}
	cap := NewMemoryCapability(store, scope)

	if cap.Name() != memory.ExecutorMode {
		t.Errorf("Name() = %q, want %q", cap.Name(), memory.ExecutorMode)
	}

	if err := cap.Init(CapabilityContext{}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	// Verify all 4 memory tools are registered
	for _, name := range []string{
		memory.RecallToolName,
		memory.RememberToolName,
		memory.ListToolName,
		memory.ForgetToolName,
	} {
		if registry.Get(name) == nil {
			t.Errorf("tool %q not registered", name)
		}
	}

	if err := cap.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestMemoryCapability_WithExtractorRetriever(t *testing.T) {
	store := memory.NewInMemoryStore()
	cap := NewMemoryCapability(store, nil)
	cap.WithExtractor(nil) // nil is valid (no-op)
	cap.WithRetriever(nil)

	if cap.extractor != nil {
		t.Error("extractor should be nil")
	}
}

func TestWithMemoryOption(t *testing.T) {
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "test"}

	opt := WithMemory(store, scope)
	cfg := &config{}
	if err := opt(cfg); err != nil {
		t.Fatalf("WithMemory: %v", err)
	}
	if len(cfg.capabilities) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(cfg.capabilities))
	}
	if cfg.capabilities[0].Name() != memory.ExecutorMode {
		t.Errorf("capability name = %q", cfg.capabilities[0].Name())
	}
}

func TestWithMemoryContextFormatter_StoredOnCapability(t *testing.T) {
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "test"}

	sentinel := "from-host-formatter"
	formatter := func(_ []*memory.Memory) string { return sentinel }

	cfg := &config{}
	opt := WithMemory(store, scope, WithMemoryContextFormatter(formatter))
	if err := opt(cfg); err != nil {
		t.Fatalf("WithMemory: %v", err)
	}
	cap, ok := cfg.capabilities[0].(*MemoryCapability)
	if !ok {
		t.Fatalf("expected *MemoryCapability, got %T", cfg.capabilities[0])
	}
	if cap.formatter == nil {
		t.Fatal("expected formatter to be set on capability")
	}
	if got := cap.formatter(nil); got != sentinel {
		t.Errorf("formatter returned %q, want %q", got, sentinel)
	}
}

func TestWithMemoryContextFormatter_NilFormatterIsAccepted(t *testing.T) {
	// Passing nil should be allowed — it just leaves the default in place.
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "test"}

	cfg := &config{}
	opt := WithMemory(store, scope, WithMemoryContextFormatter(nil))
	if err := opt(cfg); err != nil {
		t.Fatalf("WithMemory: %v", err)
	}
	cap := cfg.capabilities[0].(*MemoryCapability)
	if cap.formatter != nil {
		t.Error("formatter should remain nil when WithMemoryContextFormatter(nil)")
	}
}

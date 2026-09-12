package sdk

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
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

func TestMemoryCapability_ToolsDisabled_SkipsRegistration(t *testing.T) {
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "test"}
	cap := NewMemoryCapability(store, scope)
	cap.toolsDisabled = true

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	// No memory tools should be registered when tools are disabled,
	// even though the user_id scope is present.
	for _, name := range []string{
		memory.RecallToolName,
		memory.RememberToolName,
		memory.ListToolName,
		memory.ForgetToolName,
	} {
		if registry.Get(name) != nil {
			t.Errorf("tool %q registered, want skipped when tools disabled", name)
		}
	}
}

func TestWithMemoryToolsDisabled_StoredOnCapability(t *testing.T) {
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "test"}

	retriever := memory.Retriever(nil)
	cfg := &config{}
	opt := WithMemory(store, scope,
		WithMemoryRetriever(retriever),
		WithMemoryToolsDisabled(),
	)
	if err := opt(cfg); err != nil {
		t.Fatalf("WithMemory: %v", err)
	}
	cap, ok := cfg.capabilities[0].(*MemoryCapability)
	if !ok {
		t.Fatalf("expected *MemoryCapability, got %T", cfg.capabilities[0])
	}
	if !cap.toolsDisabled {
		t.Error("expected toolsDisabled to be true")
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

func TestMemoryCapability_AnonymousScope_SkipsTools(t *testing.T) {
	// Regression guard for #852: with the default subject key and no value
	// for it, the tools stay unregistered.
	store := memory.NewInMemoryStore()
	cap := NewMemoryCapability(store, map[string]string{"workspace_id": "w1"})

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	for _, name := range memoryToolNames() {
		if registry.Get(name) != nil {
			t.Errorf("tool %q registered, want skipped for anonymous scope", name)
		}
	}
}

func TestMemoryCapability_CustomSubjectKey_RegistersTools(t *testing.T) {
	store := memory.NewInMemoryStore()
	scope := map[string]string{"virtual_user_id": "vu-1", "workspace_id": "w1"}
	cap := NewMemoryCapability(store, scope)
	cap.subjectKey = "virtual_user_id"

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	for _, name := range memoryToolNames() {
		if registry.Get(name) == nil {
			t.Errorf("tool %q not registered under custom subject key", name)
		}
	}
}

func TestMemoryCapability_CustomSubjectKey_IgnoresDefaultKey(t *testing.T) {
	// The configured key is the only one that counts — a stray "user_id"
	// must not satisfy the gate.
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "u-1"}
	cap := NewMemoryCapability(store, scope)
	cap.subjectKey = "virtual_user_id"

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	for _, name := range memoryToolNames() {
		if registry.Get(name) != nil {
			t.Errorf("tool %q registered, want skipped when the configured subject key is absent", name)
		}
	}
}

func TestMemoryCapability_EmptySubjectKey_DisablesGate(t *testing.T) {
	store := memory.NewInMemoryStore()
	cap := NewMemoryCapability(store, nil)
	cap.subjectKey = ""

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	for _, name := range memoryToolNames() {
		if registry.Get(name) == nil {
			t.Errorf("tool %q not registered, want registered when the subject gate is disabled", name)
		}
	}
}

func TestMemoryCapability_SkipLogsWarnNamingTheKeys(t *testing.T) {
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	store := memory.NewInMemoryStore()
	scope := map[string]string{"workspace_id": "w1", "virtual_user_id": "vu-1"}
	NewMemoryCapability(store, scope).RegisterTools(tools.NewRegistry())

	out := buf.String()
	if !strings.Contains(out, "memory tools skipped") {
		t.Fatalf("expected a warn-level skip line, got %q", out)
	}
	if !strings.Contains(out, `"expected_key":"user_id"`) {
		t.Errorf("skip line does not name the key it looked for: %q", out)
	}
	for _, k := range []string{"workspace_id", "virtual_user_id"} {
		if !strings.Contains(out, k) {
			t.Errorf("skip line does not report present scope key %q: %q", k, out)
		}
	}
}

func TestWithMemorySubjectKey_StoredOnCapability(t *testing.T) {
	store := memory.NewInMemoryStore()
	scope := map[string]string{"virtual_user_id": "vu-1"}

	cfg := &config{}
	if err := WithMemory(store, scope, WithMemorySubjectKey("virtual_user_id"))(cfg); err != nil {
		t.Fatalf("WithMemory: %v", err)
	}
	cap, ok := cfg.capabilities[0].(*MemoryCapability)
	if !ok {
		t.Fatalf("expected *MemoryCapability, got %T", cfg.capabilities[0])
	}
	if cap.subjectKey != "virtual_user_id" {
		t.Errorf("subjectKey = %q, want %q", cap.subjectKey, "virtual_user_id")
	}
}

func TestWithMemory_DefaultsSubjectKeyToUserID(t *testing.T) {
	cfg := &config{}
	if err := WithMemory(memory.NewInMemoryStore(), nil)(cfg); err != nil {
		t.Fatalf("WithMemory: %v", err)
	}
	cap := cfg.capabilities[0].(*MemoryCapability)
	if cap.subjectKey != "user_id" {
		t.Errorf("subjectKey = %q, want default %q", cap.subjectKey, "user_id")
	}
}

func memoryToolNames() []string {
	return []string{
		memory.RecallToolName,
		memory.RememberToolName,
		memory.ListToolName,
		memory.ForgetToolName,
	}
}

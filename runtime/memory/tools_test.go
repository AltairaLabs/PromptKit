package memory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestMemoryExecutor_Recall(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	scope := map[string]string{"user_id": "u1"}

	store.Save(ctx, &Memory{Content: "User likes Go", Scope: scope, Confidence: 0.9})

	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: RecallToolName}

	args, _ := json.Marshal(map[string]string{"query": "Go"})
	result, err := exec.Execute(ctx, desc, args)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}

	var resp struct {
		Count    int       `json:"count"`
		Memories []*Memory `json:"memories"`
	}
	json.Unmarshal(result, &resp)
	if resp.Count != 1 {
		t.Errorf("count = %d, want 1", resp.Count)
	}
}

func TestMemoryExecutor_Remember(t *testing.T) {
	store := NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}
	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: RememberToolName}

	args, _ := json.Marshal(map[string]any{
		"content": "User's name is Craig",
		"type":    "fact",
	})
	result, err := exec.Execute(context.Background(), desc, args)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["status"] != "remembered" {
		t.Errorf("status = %q", resp["status"])
	}
	if resp["id"] == "" {
		t.Error("id should be set")
	}

	// Verify it was stored
	all, _ := store.List(context.Background(), scope, ListOptions{})
	if len(all) != 1 || all[0].Content != "User's name is Craig" {
		t.Errorf("memory not stored correctly")
	}
}

func TestMemoryExecutor_RememberEmptyContent(t *testing.T) {
	store := NewInMemoryStore()
	exec := NewExecutor(store, nil)
	desc := &tools.ToolDescriptor{Name: RememberToolName}

	args, _ := json.Marshal(map[string]string{"content": ""})
	_, err := exec.Execute(context.Background(), desc, args)
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestMemoryExecutor_List(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	scope := map[string]string{"user_id": "u1"}

	store.Save(ctx, &Memory{Type: "fact", Content: "a", Scope: scope})
	store.Save(ctx, &Memory{Type: "pref", Content: "b", Scope: scope})

	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: ListToolName}

	args, _ := json.Marshal(map[string]any{"types": []string{"fact"}})
	result, err := exec.Execute(ctx, desc, args)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var resp struct{ Count int }
	json.Unmarshal(result, &resp)
	if resp.Count != 1 {
		t.Errorf("count = %d, want 1", resp.Count)
	}
}

func TestMemoryExecutor_Forget(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	scope := map[string]string{"user_id": "u1"}

	store.Save(ctx, &Memory{Content: "to forget", Scope: scope})
	all, _ := store.List(ctx, scope, ListOptions{})
	memID := all[0].ID

	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: ForgetToolName}

	args, _ := json.Marshal(map[string]string{"memory_id": memID})
	result, err := exec.Execute(ctx, desc, args)
	if err != nil {
		t.Fatalf("forget: %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["status"] != "forgotten" {
		t.Errorf("status = %q", resp["status"])
	}

	remaining, _ := store.List(ctx, scope, ListOptions{})
	if len(remaining) != 0 {
		t.Errorf("memory should be deleted")
	}
}

func TestMemoryExecutor_ForgetEmptyID(t *testing.T) {
	store := NewInMemoryStore()
	exec := NewExecutor(store, nil)
	desc := &tools.ToolDescriptor{Name: ForgetToolName}

	args, _ := json.Marshal(map[string]string{"memory_id": ""})
	_, err := exec.Execute(context.Background(), desc, args)
	if err == nil {
		t.Error("expected error for empty memory_id")
	}
}

func TestMemoryExecutor_UnknownTool(t *testing.T) {
	store := NewInMemoryStore()
	exec := NewExecutor(store, nil)
	desc := &tools.ToolDescriptor{Name: "memory__unknown"}

	_, err := exec.Execute(context.Background(), desc, json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestMemoryExecutor_NilDescriptor(t *testing.T) {
	store := NewInMemoryStore()
	exec := NewExecutor(store, nil)

	_, err := exec.Execute(context.Background(), nil, json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for nil descriptor")
	}
}

// ---------------------------------------------------------------------------
// Provenance tests
// ---------------------------------------------------------------------------

func TestMemoryExecutor_Remember_SetsProvenance(t *testing.T) {
	store := NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}
	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: RememberToolName}

	args, _ := json.Marshal(map[string]any{
		"content": "My name is Charlie",
	})
	_, err := exec.Execute(context.Background(), desc, args)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	all, _ := store.List(context.Background(), scope, ListOptions{})
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}

	prov, ok := all[0].Metadata[MetaKeyProvenance]
	if !ok {
		t.Fatal("expected provenance in metadata")
	}
	if prov != string(ProvenanceUserRequested) {
		t.Errorf("provenance = %q, want %q", prov, ProvenanceUserRequested)
	}
}

func TestMemoryExecutor_Remember_PreservesUserMetadata(t *testing.T) {
	store := NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}
	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: RememberToolName}

	// User provides their own metadata — provenance should be added alongside it
	args, _ := json.Marshal(map[string]any{
		"content":  "I prefer dark mode",
		"metadata": map[string]any{"source": "settings", "priority": "high"},
	})
	_, err := exec.Execute(context.Background(), desc, args)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	all, _ := store.List(context.Background(), scope, ListOptions{})
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}

	m := all[0]
	// Provenance should be set
	if m.Metadata[MetaKeyProvenance] != string(ProvenanceUserRequested) {
		t.Errorf("provenance = %q, want %q", m.Metadata[MetaKeyProvenance], ProvenanceUserRequested)
	}
	// User metadata should be preserved
	if m.Metadata["source"] != "settings" {
		t.Errorf("source = %q, want 'settings'", m.Metadata["source"])
	}
	if m.Metadata["priority"] != "high" {
		t.Errorf("priority = %q, want 'high'", m.Metadata["priority"])
	}
}

func TestMemoryExecutor_Remember_StashesConsentCategory(t *testing.T) {
	store := NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}
	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: RememberToolName}

	args, _ := json.Marshal(map[string]any{
		"content":  "allergic to peanuts",
		"category": "memory:health",
	})
	if _, err := exec.Execute(context.Background(), desc, args); err != nil {
		t.Fatalf("remember: %v", err)
	}

	all, _ := store.List(context.Background(), scope, ListOptions{})
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}
	if got := all[0].Metadata[MetaKeyConsentCategory]; got != "memory:health" {
		t.Errorf("%s = %q, want %q", MetaKeyConsentCategory, got, "memory:health")
	}
	// Provenance still pinned regardless of category presence.
	if all[0].Metadata[MetaKeyProvenance] != string(ProvenanceUserRequested) {
		t.Errorf("provenance lost when category supplied")
	}
}

func TestMemoryExecutor_Remember_NoCategoryLeavesMetadataAlone(t *testing.T) {
	store := NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}
	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: RememberToolName}

	args, _ := json.Marshal(map[string]any{
		"content": "I prefer dark mode",
	})
	if _, err := exec.Execute(context.Background(), desc, args); err != nil {
		t.Fatalf("remember: %v", err)
	}

	all, _ := store.List(context.Background(), scope, ListOptions{})
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}
	if _, ok := all[0].Metadata[MetaKeyConsentCategory]; ok {
		t.Errorf("expected %s absent when not supplied", MetaKeyConsentCategory)
	}
}

func TestMemoryExecutor_Remember_UserCannotOverrideProvenance(t *testing.T) {
	store := NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}
	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: RememberToolName}

	// LLM tries to set provenance to something else — should be overridden
	args, _ := json.Marshal(map[string]any{
		"content":  "sneaky memory",
		"metadata": map[string]any{"provenance": "operator_curated"},
	})
	_, err := exec.Execute(context.Background(), desc, args)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	all, _ := store.List(context.Background(), scope, ListOptions{})
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}

	// Provenance should be forced to user_requested regardless of what the LLM sent
	if all[0].Metadata[MetaKeyProvenance] != string(ProvenanceUserRequested) {
		t.Errorf("provenance = %q, want %q (should override LLM-provided value)",
			all[0].Metadata[MetaKeyProvenance], ProvenanceUserRequested)
	}
}

func TestMemoryExecutor_Remember_NilMetadataGetsProvenance(t *testing.T) {
	store := NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}
	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: RememberToolName}

	// No metadata provided at all (weak LLMs often omit optional fields)
	args, _ := json.Marshal(map[string]any{
		"content": "remember this",
	})
	_, err := exec.Execute(context.Background(), desc, args)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	all, _ := store.List(context.Background(), scope, ListOptions{})
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}
	if all[0].Metadata == nil {
		t.Fatal("metadata should not be nil — provenance must be set")
	}
	if all[0].Metadata[MetaKeyProvenance] != string(ProvenanceUserRequested) {
		t.Errorf("provenance = %q, want %q", all[0].Metadata[MetaKeyProvenance], ProvenanceUserRequested)
	}
}

func TestMemory_SetProvenance(t *testing.T) {
	m := &Memory{Content: "test"}

	// Nil metadata — should be initialized
	m.SetProvenance(ProvenanceUserRequested)
	if m.Metadata == nil {
		t.Fatal("metadata should not be nil after SetProvenance")
	}
	if m.Metadata[MetaKeyProvenance] != string(ProvenanceUserRequested) {
		t.Errorf("provenance = %q, want %q", m.Metadata[MetaKeyProvenance], ProvenanceUserRequested)
	}

	// Overwrite with different provenance
	m.SetProvenance(ProvenanceAgentExtracted)
	if m.Metadata[MetaKeyProvenance] != string(ProvenanceAgentExtracted) {
		t.Errorf("provenance = %q, want %q", m.Metadata[MetaKeyProvenance], ProvenanceAgentExtracted)
	}

	// Preserves other metadata
	m.Metadata["other_key"] = "other_value"
	m.SetProvenance(ProvenanceSystemGenerated)
	if m.Metadata["other_key"] != "other_value" {
		t.Error("SetProvenance should not clear other metadata")
	}
}

func TestMemory_GetProvenance(t *testing.T) {
	// Nil metadata
	m := &Memory{Content: "test"}
	if got := m.GetProvenance(); got != "" {
		t.Errorf("GetProvenance on nil metadata = %q, want empty", got)
	}

	// Empty metadata
	m.Metadata = map[string]any{}
	if got := m.GetProvenance(); got != "" {
		t.Errorf("GetProvenance on empty metadata = %q, want empty", got)
	}

	// Set provenance
	m.SetProvenance(ProvenanceOperatorCurated)
	if got := m.GetProvenance(); got != ProvenanceOperatorCurated {
		t.Errorf("GetProvenance = %q, want %q", got, ProvenanceOperatorCurated)
	}

	// Wrong type in metadata (shouldn't panic)
	m.Metadata[MetaKeyProvenance] = 42
	if got := m.GetProvenance(); got != "" {
		t.Errorf("GetProvenance with non-string value = %q, want empty", got)
	}
}

func TestProvenanceConstants(t *testing.T) {
	// Verify constant values match expected strings (for JSON serialization stability)
	tests := []struct {
		prov Provenance
		want string
	}{
		{ProvenanceUserRequested, "user_requested"},
		{ProvenanceAgentExtracted, "agent_extracted"},
		{ProvenanceSystemGenerated, "system_generated"},
		{ProvenanceOperatorCurated, "operator_curated"},
	}
	for _, tt := range tests {
		if string(tt.prov) != tt.want {
			t.Errorf("Provenance %v = %q, want %q", tt.prov, string(tt.prov), tt.want)
		}
	}
}

func TestRegisterMemoryTools(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterMemoryTools(registry)

	for _, name := range []string{RecallToolName, RememberToolName, ListToolName, ForgetToolName} {
		tool := registry.Get(name)
		if tool == nil {
			t.Errorf("tool %q not registered", name)
			continue
		}
		if tool.Mode != ExecutorMode {
			t.Errorf("tool %q mode = %q, want %q", name, tool.Mode, ExecutorMode)
		}
		if tool.Namespace != "memory" {
			t.Errorf("tool %q namespace = %q, want 'memory'", name, tool.Namespace)
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q has empty InputSchema", name)
		}
		if len(tool.OutputSchema) == 0 {
			t.Errorf("tool %q has empty OutputSchema", name)
		}
	}
}

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

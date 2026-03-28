package memory

import (
	"context"
	"testing"
)

func TestInMemoryStore_SaveAndRetrieve(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	scope := map[string]string{"user_id": "u1"}

	err := store.Save(ctx, &Memory{
		Type:       "preference",
		Content:    "User prefers dark mode",
		Confidence: 0.9,
		Scope:      scope,
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	results, err := store.Retrieve(ctx, scope, "dark mode", RetrieveOptions{})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "User prefers dark mode" {
		t.Errorf("content = %q", results[0].Content)
	}
	if results[0].ID == "" {
		t.Error("ID should be auto-generated")
	}
}

func TestInMemoryStore_RetrieveWithFilters(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	scope := map[string]string{"user_id": "u1"}

	store.Save(ctx, &Memory{Type: "preference", Content: "likes dark mode", Confidence: 0.9, Scope: scope})
	store.Save(ctx, &Memory{Type: "fact", Content: "dark matter is interesting", Confidence: 0.5, Scope: scope})
	store.Save(ctx, &Memory{Type: "preference", Content: "likes Go language", Confidence: 0.8, Scope: scope})

	// Filter by type
	results, _ := store.Retrieve(ctx, scope, "dark", RetrieveOptions{Types: []string{"preference"}})
	if len(results) != 1 {
		t.Errorf("expected 1 preference match, got %d", len(results))
	}

	// Filter by confidence
	results, _ = store.Retrieve(ctx, scope, "dark", RetrieveOptions{MinConfidence: 0.7})
	if len(results) != 1 {
		t.Errorf("expected 1 high-confidence match, got %d", len(results))
	}

	// Limit
	results, _ = store.Retrieve(ctx, scope, "", RetrieveOptions{Limit: 2})
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit, got %d", len(results))
	}
}

func TestInMemoryStore_ScopeIsolation(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	scope1 := map[string]string{"user_id": "u1"}
	scope2 := map[string]string{"user_id": "u2"}

	store.Save(ctx, &Memory{Content: "user1 memory", Scope: scope1})
	store.Save(ctx, &Memory{Content: "user2 memory", Scope: scope2})

	r1, _ := store.Retrieve(ctx, scope1, "", RetrieveOptions{})
	r2, _ := store.Retrieve(ctx, scope2, "", RetrieveOptions{})

	if len(r1) != 1 || r1[0].Content != "user1 memory" {
		t.Errorf("scope1 should only see user1 memory, got %d results", len(r1))
	}
	if len(r2) != 1 || r2[0].Content != "user2 memory" {
		t.Errorf("scope2 should only see user2 memory, got %d results", len(r2))
	}
}

func TestInMemoryStore_List(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	scope := map[string]string{"user_id": "u1"}

	store.Save(ctx, &Memory{Type: "a", Content: "first", Scope: scope})
	store.Save(ctx, &Memory{Type: "b", Content: "second", Scope: scope})
	store.Save(ctx, &Memory{Type: "a", Content: "third", Scope: scope})

	// List all
	all, _ := store.List(ctx, scope, ListOptions{})
	if len(all) != 3 {
		t.Errorf("expected 3, got %d", len(all))
	}

	// List filtered
	filtered, _ := store.List(ctx, scope, ListOptions{Types: []string{"a"}})
	if len(filtered) != 2 {
		t.Errorf("expected 2 type-a, got %d", len(filtered))
	}
}

func TestInMemoryStore_Delete(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	scope := map[string]string{"user_id": "u1"}

	store.Save(ctx, &Memory{Content: "keep", Scope: scope})
	store.Save(ctx, &Memory{Content: "remove", Scope: scope})

	all, _ := store.List(ctx, scope, ListOptions{})
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}

	removeID := all[1].ID
	store.Delete(ctx, scope, removeID)

	remaining, _ := store.List(ctx, scope, ListOptions{})
	if len(remaining) != 1 {
		t.Errorf("expected 1 after delete, got %d", len(remaining))
	}
}

func TestInMemoryStore_DeleteAll(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	scope := map[string]string{"user_id": "u1"}

	store.Save(ctx, &Memory{Content: "a", Scope: scope})
	store.Save(ctx, &Memory{Content: "b", Scope: scope})

	store.DeleteAll(ctx, scope)

	remaining, _ := store.List(ctx, scope, ListOptions{})
	if len(remaining) != 0 {
		t.Errorf("expected 0 after delete all, got %d", len(remaining))
	}
}

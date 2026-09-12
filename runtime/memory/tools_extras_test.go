package memory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// optsRecordingStore captures the options structs the executor builds so a
// test can assert on passthrough without depending on a store that acts on
// it. InMemoryStore ignores Extras by design, so it cannot observe them.
type optsRecordingStore struct {
	retrieveOpts RetrieveOptions
	listOpts     ListOptions
	retrieveQ    string
}

func (s *optsRecordingStore) Save(context.Context, *Memory) error { return nil }

func (s *optsRecordingStore) Retrieve(
	_ context.Context, _ map[string]string, query string, opts RetrieveOptions,
) ([]*Memory, error) {
	s.retrieveQ = query
	s.retrieveOpts = opts
	return nil, nil
}

func (s *optsRecordingStore) List(
	_ context.Context, _ map[string]string, opts ListOptions,
) ([]*Memory, error) {
	s.listOpts = opts
	return nil, nil
}

func (s *optsRecordingStore) Delete(context.Context, map[string]string, string) error { return nil }

func (s *optsRecordingStore) DeleteAll(context.Context, map[string]string) error { return nil }

// TestMemoryExecutor_Recall_PassesUnknownArgsThroughToOptions is the read-side
// counterpart of TestMemoryExecutor_Remember_PassesUnknownArgsThroughToMetadata.
// A host extends memory__recall's schema with backend-specific fields (Omnia
// adds graph expansion and point-in-time args); the executor's typed decode
// used to drop them silently. See AltairaLabs/PromptKit#1987.
func TestMemoryExecutor_Recall_PassesUnknownArgsThroughToOptions(t *testing.T) {
	store := &optsRecordingStore{}
	exec := NewExecutor(store, map[string]string{"user_id": "u1"})
	desc := &tools.ToolDescriptor{Name: RecallToolName}

	args, _ := json.Marshal(map[string]any{
		"query":          "seat preference",
		"types":          []string{"preference"},
		"limit":          5,
		"min_confidence": 0.7,
		"seed_name":      "craig",
		"relation_types": []string{"works_with", "reports_to"},
		"max_hops":       2,
		"as_of":          "2026-01-01T00:00:00Z",
	})
	if _, err := exec.Execute(context.Background(), desc, args); err != nil {
		t.Fatalf("recall: %v", err)
	}

	got := store.retrieveOpts
	if store.retrieveQ != "seat preference" {
		t.Errorf("query = %q, want %q", store.retrieveQ, "seat preference")
	}
	// Typed fields still decode exactly as before.
	if len(got.Types) != 1 || got.Types[0] != "preference" {
		t.Errorf("Types = %v, want [preference]", got.Types)
	}
	if got.Limit != 5 {
		t.Errorf("Limit = %d, want 5", got.Limit)
	}
	if got.MinConfidence != 0.7 {
		t.Errorf("MinConfidence = %v, want 0.7", got.MinConfidence)
	}

	// Untyped fields reach the store.
	if got.Extras["seed_name"] != "craig" {
		t.Errorf("Extras[seed_name] = %v, want craig", got.Extras["seed_name"])
	}
	if got.Extras["as_of"] != "2026-01-01T00:00:00Z" {
		t.Errorf("Extras[as_of] = %v", got.Extras["as_of"])
	}
	// JSON numbers decode as float64 through the generic map.
	if hops, ok := got.Extras["max_hops"].(float64); !ok || hops != 2 {
		t.Errorf("Extras[max_hops] = %#v, want 2", got.Extras["max_hops"])
	}
	rel, ok := got.Extras["relation_types"].([]any)
	if !ok || len(rel) != 2 || rel[0] != "works_with" {
		t.Errorf("Extras[relation_types] = %#v", got.Extras["relation_types"])
	}

	// Typed keys must not be duplicated into Extras — a store reading
	// Extras must not see a second, untyped copy of limit or query.
	for _, k := range []string{"query", "types", "limit", "min_confidence"} {
		if _, dup := got.Extras[k]; dup {
			t.Errorf("typed key %q leaked into Extras", k)
		}
	}
}

// TestMemoryExecutor_Recall_NoExtrasLeavesOptionsClean proves the default path
// is unchanged: a plain recall passes no Extras, so a store that branches on
// len(opts.Extras) keeps its current behavior.
func TestMemoryExecutor_Recall_NoExtrasLeavesOptionsClean(t *testing.T) {
	store := &optsRecordingStore{}
	exec := NewExecutor(store, nil)
	desc := &tools.ToolDescriptor{Name: RecallToolName}

	args, _ := json.Marshal(map[string]any{"query": "Go", "limit": 3})
	if _, err := exec.Execute(context.Background(), desc, args); err != nil {
		t.Fatalf("recall: %v", err)
	}

	if len(store.retrieveOpts.Extras) != 0 {
		t.Errorf("Extras = %#v, want empty", store.retrieveOpts.Extras)
	}
	if store.retrieveOpts.Limit != 3 {
		t.Errorf("Limit = %d, want 3", store.retrieveOpts.Limit)
	}
}

// TestMemoryExecutor_Recall_MalformedArgsStillError guards the error path the
// switch from json.Unmarshal to DecodeArgsExtras could have swallowed.
func TestMemoryExecutor_Recall_MalformedArgsStillError(t *testing.T) {
	exec := NewExecutor(&optsRecordingStore{}, nil)
	desc := &tools.ToolDescriptor{Name: RecallToolName}

	if _, err := exec.Execute(context.Background(), desc, json.RawMessage(`{"query": 42}`)); err == nil {
		t.Fatal("expected an error for a non-string query")
	}
}

// TestMemoryExecutor_List_PassesUnknownArgsThroughToOptions covers the same
// passthrough for memory__list, whose typed decode had the same gap.
func TestMemoryExecutor_List_PassesUnknownArgsThroughToOptions(t *testing.T) {
	store := &optsRecordingStore{}
	exec := NewExecutor(store, map[string]string{"user_id": "u1"})
	desc := &tools.ToolDescriptor{Name: ListToolName}

	args, _ := json.Marshal(map[string]any{
		"types":     []string{"fact"},
		"limit":     10,
		"offset":    20,
		"as_of":     "2026-01-01T00:00:00Z",
		"namespace": "graph",
	})
	if _, err := exec.Execute(context.Background(), desc, args); err != nil {
		t.Fatalf("list: %v", err)
	}

	got := store.listOpts
	if got.Limit != 10 || got.Offset != 20 {
		t.Errorf("Limit/Offset = %d/%d, want 10/20", got.Limit, got.Offset)
	}
	if got.Extras["as_of"] != "2026-01-01T00:00:00Z" {
		t.Errorf("Extras[as_of] = %v", got.Extras["as_of"])
	}
	if got.Extras["namespace"] != "graph" {
		t.Errorf("Extras[namespace] = %v", got.Extras["namespace"])
	}
	for _, k := range []string{"types", "limit", "offset"} {
		if _, dup := got.Extras[k]; dup {
			t.Errorf("typed key %q leaked into Extras", k)
		}
	}
}

// extrasDeletingStore opts into the passthrough by implementing
// [ExtrasDeleter]; optsRecordingStore deliberately does not, so the two
// together cover both sides of the type assertion in forget.
type extrasDeletingStore struct {
	optsRecordingStore
	deleteOpts DeleteOptions
	deletedID  string
	withOpts   bool
}

func (s *extrasDeletingStore) DeleteWithOptions(
	_ context.Context, _ map[string]string, memoryID string, opts DeleteOptions,
) error {
	s.deletedID = memoryID
	s.deleteOpts = opts
	s.withOpts = true
	return nil
}

// TestMemoryExecutor_Forget_PassesUnknownArgsToExtrasDeleter covers the opt-in
// path: Store.Delete has no options parameter, so a store that wants the
// passthrough implements ExtrasDeleter and the executor prefers it.
func TestMemoryExecutor_Forget_PassesUnknownArgsToExtrasDeleter(t *testing.T) {
	store := &extrasDeletingStore{}
	exec := NewExecutor(store, map[string]string{"user_id": "u1"})
	desc := &tools.ToolDescriptor{Name: ForgetToolName}

	args, _ := json.Marshal(map[string]any{
		"memory_id": "m-1",
		"reason":    "superseded",
		"as_of":     "2026-01-01T00:00:00Z",
	})
	if _, err := exec.Execute(context.Background(), desc, args); err != nil {
		t.Fatalf("forget: %v", err)
	}

	if !store.withOpts {
		t.Fatal("expected DeleteWithOptions to be preferred over Delete")
	}
	if store.deletedID != "m-1" {
		t.Errorf("memory_id = %q, want m-1", store.deletedID)
	}
	if store.deleteOpts.Extras["reason"] != "superseded" {
		t.Errorf("Extras[reason] = %v", store.deleteOpts.Extras["reason"])
	}
	if store.deleteOpts.Extras["as_of"] != "2026-01-01T00:00:00Z" {
		t.Errorf("Extras[as_of] = %v", store.deleteOpts.Extras["as_of"])
	}
	if _, dup := store.deleteOpts.Extras["memory_id"]; dup {
		t.Error("typed key \"memory_id\" leaked into Extras")
	}
}

// TestMemoryExecutor_Forget_PlainStoreStillUsesDelete proves the fallback: a
// store that does not implement ExtrasDeleter is unaffected by the change.
func TestMemoryExecutor_Forget_PlainStoreStillUsesDelete(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	scope := map[string]string{"user_id": "u1"}
	m := &Memory{Content: "temp", Scope: scope, Confidence: 0.9}
	if err := store.Save(ctx, m); err != nil {
		t.Fatalf("save: %v", err)
	}

	exec := NewExecutor(store, scope)
	desc := &tools.ToolDescriptor{Name: ForgetToolName}
	args, _ := json.Marshal(map[string]any{"memory_id": m.ID, "reason": "ignored"})
	if _, err := exec.Execute(ctx, desc, args); err != nil {
		t.Fatalf("forget: %v", err)
	}

	all, _ := store.List(ctx, scope, ListOptions{})
	if len(all) != 0 {
		t.Errorf("expected the memory to be deleted, %d remain", len(all))
	}
}

// TestMemoryExecutor_Forget_MissingIDStillErrors guards the required-field
// check across the switch to DecodeArgsExtras.
func TestMemoryExecutor_Forget_MissingIDStillErrors(t *testing.T) {
	exec := NewExecutor(&extrasDeletingStore{}, nil)
	desc := &tools.ToolDescriptor{Name: ForgetToolName}

	if _, err := exec.Execute(context.Background(), desc, json.RawMessage(`{"reason":"x"}`)); err == nil {
		t.Fatal("expected an error when memory_id is absent")
	}
}

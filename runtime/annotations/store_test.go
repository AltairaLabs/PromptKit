package annotations

import (
	"context"
	"testing"
	"time"
)

func TestNewFileStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer store.Close()
}

func TestFileStore_Add(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	t.Run("adds annotation with generated ID", func(t *testing.T) {
		ann := &Annotation{
			Type:      TypeScore,
			SessionID: "session-1",
			Target:    ForSession(),
			Key:       "quality",
			Value:     NewScoreValue(0.85),
		}

		if err := store.Add(ctx, ann); err != nil {
			t.Fatalf("add: %v", err)
		}

		if ann.ID == "" {
			t.Error("expected ID to be generated")
		}
		if ann.Version != 1 {
			t.Errorf("expected version 1, got %d", ann.Version)
		}
		if ann.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set")
		}
	})

	t.Run("preserves provided ID", func(t *testing.T) {
		ann := &Annotation{
			ID:        "custom-id",
			Type:      TypeLabel,
			SessionID: "session-1",
			Target:    ForSession(),
			Key:       "category",
			Value:     NewLabelValue("support"),
		}

		if err := store.Add(ctx, ann); err != nil {
			t.Fatalf("add: %v", err)
		}

		if ann.ID != "custom-id" {
			t.Errorf("expected ID 'custom-id', got %q", ann.ID)
		}
	})

	t.Run("fails without session ID", func(t *testing.T) {
		ann := &Annotation{
			Type:   TypeScore,
			Target: ForSession(),
			Key:    "quality",
			Value:  NewScoreValue(0.5),
		}

		if err := store.Add(ctx, ann); err == nil {
			t.Error("expected error for missing session ID")
		}
	})
}

func TestFileStore_Query(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	// Add test annotations
	annotations := []*Annotation{
		{
			Type:      TypeScore,
			SessionID: "session-1",
			Target:    ForSession(),
			Key:       "quality",
			Value:     NewScoreValue(0.85),
		},
		{
			Type:      TypeLabel,
			SessionID: "session-1",
			Target:    AtTurn(0),
			Key:       "intent",
			Value:     NewLabelValue("greeting"),
		},
		{
			Type:      TypeFlag,
			SessionID: "session-1",
			Target:    AtMessage(1),
			Key:       "safety",
			Value:     NewFlagValue(true),
		},
		{
			Type:      TypeComment,
			SessionID: "session-1",
			Target:    AtEvent("event-123"),
			Key:       "note",
			Value:     NewCommentValue("Interesting response"),
			CreatedBy: "reviewer-1",
		},
	}

	for _, ann := range annotations {
		if err := store.Add(ctx, ann); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	t.Run("queries all annotations for session", func(t *testing.T) {
		results, err := store.Query(ctx, &Filter{SessionID: "session-1"})
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		if len(results) != 4 {
			t.Errorf("expected 4 annotations, got %d", len(results))
		}
	})

	t.Run("filters by type", func(t *testing.T) {
		results, err := store.Query(ctx, &Filter{
			SessionID: "session-1",
			Types:     []AnnotationType{TypeScore},
		})
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("expected 1 annotation, got %d", len(results))
		}
		if results[0].Type != TypeScore {
			t.Errorf("expected TypeScore, got %v", results[0].Type)
		}
	})

	t.Run("filters by key", func(t *testing.T) {
		results, err := store.Query(ctx, &Filter{
			SessionID: "session-1",
			Keys:      []string{"safety"},
		})
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("expected 1 annotation, got %d", len(results))
		}
	})

	t.Run("filters by target type", func(t *testing.T) {
		results, err := store.Query(ctx, &Filter{
			SessionID:   "session-1",
			TargetTypes: []TargetType{TargetTurn},
		})
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("expected 1 annotation, got %d", len(results))
		}
	})

	t.Run("filters by event ID", func(t *testing.T) {
		results, err := store.Query(ctx, &Filter{
			SessionID: "session-1",
			EventID:   "event-123",
		})
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("expected 1 annotation, got %d", len(results))
		}
	})

	t.Run("filters by creator", func(t *testing.T) {
		results, err := store.Query(ctx, &Filter{
			SessionID: "session-1",
			CreatedBy: "reviewer-1",
		})
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("expected 1 annotation, got %d", len(results))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		results, err := store.Query(ctx, &Filter{
			SessionID: "session-1",
			Limit:     2,
		})
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("expected 2 annotations, got %d", len(results))
		}
	})

	t.Run("returns empty for non-existent session", func(t *testing.T) {
		results, err := store.Query(ctx, &Filter{SessionID: "non-existent"})
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		if len(results) != 0 {
			t.Errorf("expected 0 annotations, got %d", len(results))
		}
	})
}

func TestFileStore_Update(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	// Add original annotation
	original := &Annotation{
		Type:      TypeScore,
		SessionID: "session-1",
		Target:    ForSession(),
		Key:       "quality",
		Value:     NewScoreValue(0.5),
	}
	if err := store.Add(ctx, original); err != nil {
		t.Fatalf("add: %v", err)
	}

	t.Run("creates new version", func(t *testing.T) {
		updated := &Annotation{
			Type:      TypeScore,
			SessionID: "session-1",
			Target:    ForSession(),
			Key:       "quality",
			Value:     NewScoreValue(0.85),
		}

		if err := store.Update(ctx, original.ID, updated); err != nil {
			t.Fatalf("update: %v", err)
		}

		if updated.Version != 2 {
			t.Errorf("expected version 2, got %d", updated.Version)
		}
		if updated.PreviousID != original.ID {
			t.Errorf("expected previous ID %q, got %q", original.ID, updated.PreviousID)
		}
		if updated.ID == original.ID {
			t.Error("expected new ID for updated annotation")
		}
	})
}

func TestFileStore_Delete(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	// Add annotation
	ann := &Annotation{
		Type:      TypeScore,
		SessionID: "session-1",
		Target:    ForSession(),
		Key:       "quality",
		Value:     NewScoreValue(0.5),
	}
	if err := store.Add(ctx, ann); err != nil {
		t.Fatalf("add: %v", err)
	}

	t.Run("soft deletes annotation", func(t *testing.T) {
		if err := store.Delete(ctx, ann.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}

		// Should not be returned in normal query
		results, err := store.Query(ctx, &Filter{SessionID: "session-1"})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 annotations, got %d", len(results))
		}

		// Should be returned with IncludeDeleted
		results, err = store.Query(ctx, &Filter{
			SessionID:      "session-1",
			IncludeDeleted: true,
		})
		if err != nil {
			t.Fatalf("query with deleted: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 annotation, got %d", len(results))
		}
	})
}

func TestFileStore_Get(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	// Add annotation
	ann := &Annotation{
		Type:      TypeScore,
		SessionID: "session-1",
		Target:    ForSession(),
		Key:       "quality",
		Value:     NewScoreValue(0.85),
	}
	if err := store.Add(ctx, ann); err != nil {
		t.Fatalf("add: %v", err)
	}

	t.Run("retrieves annotation by ID", func(t *testing.T) {
		retrieved, err := store.Get(ctx, ann.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}

		if retrieved.ID != ann.ID {
			t.Errorf("expected ID %q, got %q", ann.ID, retrieved.ID)
		}
		if retrieved.Key != "quality" {
			t.Errorf("expected key 'quality', got %q", retrieved.Key)
		}
	})

	t.Run("returns error for non-existent ID", func(t *testing.T) {
		_, err := store.Get(ctx, "non-existent")
		if err == nil {
			t.Error("expected error for non-existent ID")
		}
	})
}

func TestTargetHelpers(t *testing.T) {
	t.Run("ForSession", func(t *testing.T) {
		target := ForSession()
		if target.Type != TargetSession {
			t.Errorf("expected TargetSession, got %v", target.Type)
		}
	})

	t.Run("AtEvent", func(t *testing.T) {
		target := AtEvent("event-123")
		if target.Type != TargetEvent {
			t.Errorf("expected TargetEvent, got %v", target.Type)
		}
		if target.EventID != "event-123" {
			t.Errorf("expected EventID 'event-123', got %q", target.EventID)
		}
	})

	t.Run("AtTurn", func(t *testing.T) {
		target := AtTurn(5)
		if target.Type != TargetTurn {
			t.Errorf("expected TargetTurn, got %v", target.Type)
		}
		if target.TurnIndex != 5 {
			t.Errorf("expected TurnIndex 5, got %d", target.TurnIndex)
		}
	})

	t.Run("InTimeRange", func(t *testing.T) {
		start := time.Now()
		end := start.Add(time.Hour)
		target := InTimeRange(start, end)
		if target.Type != TargetTimeRange {
			t.Errorf("expected TargetTimeRange, got %v", target.Type)
		}
		if !target.StartTime.Equal(start) {
			t.Error("start time mismatch")
		}
		if !target.EndTime.Equal(end) {
			t.Error("end time mismatch")
		}
	})
}

func TestAnnotationValueHelpers(t *testing.T) {
	t.Run("NewScoreValue", func(t *testing.T) {
		val := NewScoreValue(0.85)
		if val.Score == nil || *val.Score != 0.85 {
			t.Error("score value mismatch")
		}
	})

	t.Run("NewLabelValue", func(t *testing.T) {
		val := NewLabelValue("category")
		if val.Label != "category" {
			t.Error("label value mismatch")
		}
	})

	t.Run("NewLabelsValue", func(t *testing.T) {
		val := NewLabelsValue("a", "b", "c")
		if len(val.Labels) != 3 {
			t.Error("labels count mismatch")
		}
	})

	t.Run("NewCommentValue", func(t *testing.T) {
		val := NewCommentValue("test comment")
		if val.Text != "test comment" {
			t.Error("comment value mismatch")
		}
	})

	t.Run("NewFlagValue", func(t *testing.T) {
		val := NewFlagValue(true)
		if val.Flag == nil || !*val.Flag {
			t.Error("flag value mismatch")
		}
	})

	t.Run("NewAssertionValue", func(t *testing.T) {
		val := NewAssertionValue(true, "test passed")
		if val.Passed == nil || !*val.Passed {
			t.Error("passed value mismatch")
		}
		if val.Message != "test passed" {
			t.Error("message mismatch")
		}
	})

	t.Run("NewMetricValue", func(t *testing.T) {
		val := NewMetricValue(42.5, "ms")
		if val.Score == nil || *val.Score != 42.5 {
			t.Error("metric value mismatch")
		}
		if val.Unit != "ms" {
			t.Error("unit mismatch")
		}
	})
}

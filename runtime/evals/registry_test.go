package evals

import (
	"context"
	"sync"
	"testing"
)

// stubHandler is a minimal EvalTypeHandler for testing.
type stubHandler struct {
	typeName string
}

func (s *stubHandler) Type() string { return s.typeName }

func (s *stubHandler) Eval(
	_ context.Context, _ *EvalContext, _ map[string]any,
) (*EvalResult, error) {
	return &EvalResult{EvalID: s.typeName, Passed: true}, nil
}

func TestNewEmptyEvalTypeRegistry(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	if len(r.Types()) != 0 {
		t.Errorf("empty registry should have 0 types, got %d", len(r.Types()))
	}
}

func TestNewEvalTypeRegistry(t *testing.T) {
	r := NewEvalTypeRegistry()
	// Currently no built-in handlers, but should not panic
	if r == nil {
		t.Fatal("NewEvalTypeRegistry returned nil")
	}
}

func TestRegisterAndGet(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	h := &stubHandler{typeName: "contains"}
	r.Register(h)

	got, err := r.Get("contains")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Type() != "contains" {
		t.Errorf("got type %q, want %q", got.Type(), "contains")
	}
}

func TestGetUnknownType(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("Get should return error for unknown type")
	}
}

func TestHas(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	r.Register(&stubHandler{typeName: "regex"})

	if !r.Has("regex") {
		t.Error("Has should return true for registered type")
	}
	if r.Has("unknown") {
		t.Error("Has should return false for unregistered type")
	}
}

func TestTypes(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	r.Register(&stubHandler{typeName: "contains"})
	r.Register(&stubHandler{typeName: "regex"})
	r.Register(&stubHandler{typeName: "json_valid"})

	types := r.Types()
	want := []string{"contains", "json_valid", "regex"}
	if len(types) != len(want) {
		t.Fatalf("got %d types, want %d", len(types), len(want))
	}
	for i, w := range want {
		if types[i] != w {
			t.Errorf("types[%d] = %q, want %q", i, types[i], w)
		}
	}
}

func TestRegisterReplaces(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	r.Register(&stubHandler{typeName: "contains"})

	// Replace with a new handler of the same type
	replacement := &stubHandler{typeName: "contains"}
	r.Register(replacement)

	got, err := r.Get("contains")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != replacement {
		t.Error("Register should replace existing handler")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	var wg sync.WaitGroup

	// Concurrent registrations
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			h := &stubHandler{typeName: "type_" + string(rune('a'+n))}
			r.Register(h)
		}(i)
	}

	// Concurrent reads
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Has("type_a")
			r.Types()
			_, _ = r.Get("type_a")
		}()
	}

	wg.Wait()

	// Should have all 10 types registered
	if len(r.Types()) != 10 {
		t.Errorf("got %d types, want 10", len(r.Types()))
	}
}

func TestHandlerEval(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	r.Register(&stubHandler{typeName: "test"})

	h, err := r.Get("test")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	result, err := h.Eval(context.Background(), &EvalContext{}, nil)
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true")
	}
	if result.EvalID != "test" {
		t.Errorf("got EvalID=%q, want %q", result.EvalID, "test")
	}
}

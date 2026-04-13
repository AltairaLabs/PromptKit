package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubSandbox struct{ name string }

func (s *stubSandbox) Name() string { return s.name }
func (s *stubSandbox) Spawn(_ context.Context, _ Request) (Response, error) {
	return Response{}, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	factory := func(name string, _ map[string]any) (Sandbox, error) {
		return &stubSandbox{name: name}, nil
	}
	if err := r.Register("stub", factory); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := r.Lookup("stub")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	sb, err := got("my_stub", nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if sb.Name() != "my_stub" {
		t.Errorf("Name = %q, want %q", sb.Name(), "my_stub")
	}
}

func TestRegistry_DuplicateRegisterErrors(t *testing.T) {
	r := NewRegistry()
	f := func(_ string, _ map[string]any) (Sandbox, error) { return nil, nil }
	if err := r.Register("once", f); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register("once", f)
	if err == nil {
		t.Fatal("second Register should fail")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("expected duplicate-registration error, got: %v", err)
	}
}

func TestRegistry_ReplaceOverwritesSilently(t *testing.T) {
	r := NewRegistry()
	one := func(_ string, _ map[string]any) (Sandbox, error) { return &stubSandbox{name: "one"}, nil }
	two := func(_ string, _ map[string]any) (Sandbox, error) { return &stubSandbox{name: "two"}, nil }
	_ = r.Register("mode", one)
	if err := r.Replace("mode", two); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	f, err := r.Lookup("mode")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	sb, _ := f("", nil)
	if sb.Name() != "two" {
		t.Errorf("Replace should have overwritten, got name=%q", sb.Name())
	}
}

func TestRegistry_RejectsEmptyName(t *testing.T) {
	r := NewRegistry()
	f := func(_ string, _ map[string]any) (Sandbox, error) { return nil, nil }
	if err := r.Register("", f); err == nil {
		t.Error("Register with empty name should fail")
	}
	if err := r.Replace("", f); err == nil {
		t.Error("Replace with empty name should fail")
	}
}

func TestRegistry_RejectsNilFactory(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("nil_fact", nil); err == nil {
		t.Error("Register with nil factory should fail")
	}
	if err := r.Replace("nil_fact", nil); err == nil {
		t.Error("Replace with nil factory should fail")
	}
}

func TestRegistry_LookupUnknownListsKnownModes(t *testing.T) {
	r := NewRegistry()
	_ = r.Register("alpha", func(_ string, _ map[string]any) (Sandbox, error) { return nil, nil })
	_ = r.Register("beta", func(_ string, _ map[string]any) (Sandbox, error) { return nil, nil })

	_, err := r.Lookup("missing")
	if err == nil {
		t.Fatal("Lookup of unknown mode should fail")
	}
	if !strings.Contains(err.Error(), "missing") ||
		!strings.Contains(err.Error(), "alpha") ||
		!strings.Contains(err.Error(), "beta") {
		t.Errorf("error should mention requested and known modes, got: %v", err)
	}
}

// TestRegistry_FactoryError is a sanity check that errors from the
// factory function itself propagate cleanly to the caller.
func TestRegistry_FactoryError(t *testing.T) {
	r := NewRegistry()
	sentinel := errors.New("boom")
	_ = r.Register("bad", func(_ string, _ map[string]any) (Sandbox, error) {
		return nil, sentinel
	})
	f, err := r.Lookup("bad")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if _, err := f("x", nil); !errors.Is(err, sentinel) {
		t.Errorf("factory error should propagate, got %v", err)
	}
}

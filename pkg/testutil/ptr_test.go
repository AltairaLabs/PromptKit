package testutil

import "testing"

func TestPtr(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		p := Ptr("hello")
		if p == nil {
			t.Fatal("expected non-nil pointer")
		}
		if *p != "hello" {
			t.Fatalf("expected %q, got %q", "hello", *p)
		}
	})

	t.Run("int", func(t *testing.T) {
		p := Ptr(42)
		if p == nil {
			t.Fatal("expected non-nil pointer")
		}
		if *p != 42 {
			t.Fatalf("expected %d, got %d", 42, *p)
		}
	})

	t.Run("bool", func(t *testing.T) {
		p := Ptr(true)
		if p == nil {
			t.Fatal("expected non-nil pointer")
		}
		if *p != true {
			t.Fatal("expected true")
		}
	})

	t.Run("float32", func(t *testing.T) {
		p := Ptr(float32(3.14))
		if p == nil {
			t.Fatal("expected non-nil pointer")
		}
		if *p != float32(3.14) {
			t.Fatalf("expected %f, got %f", float32(3.14), *p)
		}
	})

	t.Run("float64", func(t *testing.T) {
		p := Ptr(1.618)
		if p == nil {
			t.Fatal("expected non-nil pointer")
		}
		if *p != 1.618 {
			t.Fatalf("expected %f, got %f", 1.618, *p)
		}
	})

	t.Run("struct", func(t *testing.T) {
		type S struct{ X int }
		p := Ptr(S{X: 7})
		if p == nil {
			t.Fatal("expected non-nil pointer")
		}
		if p.X != 7 {
			t.Fatalf("expected X=7, got X=%d", p.X)
		}
	})

	t.Run("returns distinct pointers", func(t *testing.T) {
		a := Ptr(1)
		b := Ptr(1)
		if a == b {
			t.Fatal("expected distinct pointers for separate calls")
		}
	})
}

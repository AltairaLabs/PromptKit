package testutil

import "testing"

func TestPtr(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		p := Ptr("hello")
		if p == nil || *p != "hello" {
			t.Fatal("unexpected result")
		}
	})

	t.Run("int", func(t *testing.T) {
		p := Ptr(42)
		if p == nil || *p != 42 {
			t.Fatal("unexpected result")
		}
	})

	t.Run("distinct pointers", func(t *testing.T) {
		a := Ptr(1)
		b := Ptr(1)
		if a == b {
			t.Fatal("expected distinct pointers")
		}
	})
}

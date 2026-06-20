package safe

import (
	"errors"
	"testing"
)

func TestRun_RecoversPanicAndCallsOnPanic(t *testing.T) {
	var got any
	// Must not propagate the panic out of Run.
	Run("test", func() { panic("boom") }, func(r any) { got = r })
	if got != "boom" {
		t.Fatalf("onPanic should receive the recovered value, got %v", got)
	}
}

func TestRun_NoPanicRunsFn(t *testing.T) {
	ran := false
	Run("test", func() { ran = true }, nil)
	if !ran {
		t.Fatal("fn should have run")
	}
}

func TestRun_NilOnPanicStillRecovers(t *testing.T) {
	Run("test", func() { panic(errors.New("x")) }, nil) // reaching here == recovered
}

func TestRecover_DeferredRecovers(t *testing.T) {
	func() {
		defer Recover("test")
		panic("boom")
	}() // reaching here == recovered
}

func TestGo_RecoversInGoroutine(t *testing.T) {
	done := make(chan any, 1)
	Go("test", func() { panic("boom") }, func(r any) { done <- r })
	if r := <-done; r != "boom" {
		t.Fatalf("got %v", r)
	}
}

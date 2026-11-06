package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// admitCounter is an admission gate that counts its calls and refuses with
// refuse when set.
type admitCounter struct {
	calls  int
	refuse error
}

func (a *admitCounter) admit() error {
	a.calls++
	return a.refuse
}

func newAdmittedTestRegistry(t *testing.T, opts ...RegistryOption) *Registry {
	t.Helper()
	r := NewRegistry(opts...)
	if err := r.Register(&ToolDescriptor{
		Name: "echo", Description: "echo", Mode: "mock", MockResult: json.RawMessage(`"ok"`),
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

// A call that fails one of the registry's own checks never reaches admit.
func TestExecuteAsyncAdmitted_ChecksRunBeforeAdmit(t *testing.T) {
	ctx := context.Background()
	r := newAdmittedTestRegistry(t, WithRateLimit(1))
	gate := &admitCounter{}

	if _, err := r.ExecuteAsyncAdmitted(ctx, "nope", json.RawMessage(`{"id":"a"}`), gate.admit); err == nil {
		t.Fatal("an unknown tool is an error")
	}
	if _, err := r.ExecuteAsyncAdmitted(ctx, "echo", json.RawMessage(`{}`), gate.admit); err == nil {
		t.Fatal("invalid arguments are an error")
	}
	if gate.calls != 0 {
		t.Fatalf("admit was called %d times for calls that never ran", gate.calls)
	}

	res, err := r.ExecuteAsyncAdmitted(ctx, "echo", json.RawMessage(`{"id":"a"}`), gate.admit)
	if err != nil || res.Status != ToolStatusComplete || gate.calls != 1 {
		t.Fatalf("a valid call is admitted and runs: res=%+v err=%v admits=%d", res, err, gate.calls)
	}
	res, err = r.ExecuteAsyncAdmitted(ctx, "echo", json.RawMessage(`{"id":"b"}`), gate.admit)
	if err != nil || res.Status != ToolStatusFailed || gate.calls != 1 {
		t.Fatalf("the registry's limit refuses before admit: res=%+v err=%v admits=%d", res, err, gate.calls)
	}
}

// A refusal from admit is returned unchanged, the executor does not run, and
// the registry's own limit does not count the call.
func TestExecuteAsyncAdmitted_RefusalIsNotCounted(t *testing.T) {
	ctx := context.Background()
	r := newAdmittedTestRegistry(t, WithRateLimit(1))
	refusal := errors.New("over the caller's limit")

	res, err := r.ExecuteAsyncAdmitted(ctx, "echo", json.RawMessage(`{"id":"a"}`), (&admitCounter{refuse: refusal}).admit)
	if !errors.Is(err, refusal) || res != nil {
		t.Fatalf("got res=%+v err=%v, want the admit error", res, err)
	}
	res, err = r.ExecuteAsync(ctx, "echo", json.RawMessage(`{"id":"b"}`))
	if err != nil || res.Status != ToolStatusComplete {
		t.Fatalf("the refused call took the registry's one slot: res=%+v err=%v", res, err)
	}
}

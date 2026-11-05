package hooks

import (
	"context"
	"errors"
	"testing"
)

// rewritingHook is a tool hook that also rewrites results.
type rewritingHook struct {
	stubToolHook
	fn func(req ToolRequest, content string) (string, error)
}

func (h *rewritingHook) RewriteResult(_ context.Context, req ToolRequest, content string) (string, error) {
	return h.fn(req, content)
}

func TestRunToolResultRewriters_ChainsInOrder(t *testing.T) {
	r := NewRegistry(
		WithToolHook(&stubToolHook{name: "plain"}),
		WithToolHook(&rewritingHook{fn: func(_ ToolRequest, c string) (string, error) { return c + " A", nil }}),
		WithToolHook(&rewritingHook{fn: func(req ToolRequest, c string) (string, error) { return c + " " + req.Name, nil }}),
	)
	if !r.HasToolResultRewriters() {
		t.Fatal("rewriters registered through WithToolHook should be found")
	}
	got, err := r.RunToolResultRewriters(context.Background(), ToolRequest{Name: "lookup"}, "result")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "result A lookup" {
		t.Fatalf("got %q, want each rewriter on the previous one's output", got)
	}
}

func TestRunToolResultRewriters_ErrorStopsTheChain(t *testing.T) {
	boom := errors.New("boom")
	called := false
	r := NewRegistry(
		WithToolHook(&rewritingHook{fn: func(_ ToolRequest, _ string) (string, error) { return "", boom }}),
		WithToolHook(&rewritingHook{fn: func(_ ToolRequest, c string) (string, error) { called = true; return c, nil }}),
	)
	if _, err := r.RunToolResultRewriters(context.Background(), ToolRequest{Name: "x"}, "in"); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the rewriter's error", err)
	}
	if called {
		t.Fatal("a failed rewrite should stop the chain")
	}
}

func TestRunToolResultRewriters_NoneOrNil(t *testing.T) {
	var nilReg *Registry
	if nilReg.HasToolResultRewriters() {
		t.Fatal("a nil registry has no rewriters")
	}
	got, err := nilReg.RunToolResultRewriters(context.Background(), ToolRequest{}, "same")
	if err != nil || got != "same" {
		t.Fatalf("nil registry: got %q, %v", got, err)
	}
	r := NewRegistry(WithToolHook(&stubToolHook{name: "plain"}))
	if r.HasToolResultRewriters() {
		t.Fatal("a plain tool hook is not a rewriter")
	}
}

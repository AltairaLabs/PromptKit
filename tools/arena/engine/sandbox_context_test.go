package engine

import (
	"context"
	"testing"
)

func TestSandboxContainerIDsContext(t *testing.T) {
	ctx := context.Background()
	if got := sandboxContainerIDsFromContext(ctx); got != nil {
		t.Fatalf("empty context should carry no IDs, got %v", got)
	}

	// nil/empty ids is a no-op (context unchanged).
	if got := sandboxContainerIDsFromContext(withSandboxContainerIDs(ctx, nil)); got != nil {
		t.Fatalf("nil ids should be a no-op, got %v", got)
	}

	ids := map[string]string{"sandbox": "abc123"}
	got := sandboxContainerIDsFromContext(withSandboxContainerIDs(ctx, ids))
	if got["sandbox"] != "abc123" {
		t.Fatalf("round-trip failed: got %v", got)
	}
}

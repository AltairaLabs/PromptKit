package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// stubEntry is a minimal toolCallEntry implementation for testing matchResult.
type stubEntry struct {
	id       string
	name     string
	resolved bool
	result   *types.MessageToolResult
}

func (s *stubEntry) callID() string                              { return s.id }
func (s *stubEntry) callName() string                            { return s.name }
func (s *stubEntry) isResolved() bool                            { return s.resolved }
func (s *stubEntry) applyResult(r *types.MessageToolResult)      { s.result = r; s.resolved = true }

func TestMatchResult_ByID(t *testing.T) {
	a := &stubEntry{id: "tc1", name: "search"}
	b := &stubEntry{id: "tc2", name: "lookup"}
	entries := []toolCallEntry{a, b}

	result := &types.MessageToolResult{ID: "tc2", Name: "lookup", Content: "found"}
	matchResult(entries, result)

	if a.resolved {
		t.Error("entry a should not be resolved")
	}
	if !b.resolved || b.result.Content != "found" {
		t.Errorf("entry b should be resolved with content 'found', got resolved=%v", b.resolved)
	}
}

func TestMatchResult_ByNameFallback(t *testing.T) {
	a := &stubEntry{id: "", name: "search"}
	b := &stubEntry{id: "", name: "lookup"}
	entries := []toolCallEntry{a, b}

	result := &types.MessageToolResult{ID: "", Name: "lookup", Content: "result"}
	matchResult(entries, result)

	if a.resolved {
		t.Error("entry a should not be resolved")
	}
	if !b.resolved || b.result.Content != "result" {
		t.Errorf("entry b should be resolved with content 'result', got resolved=%v", b.resolved)
	}
}

func TestMatchResult_IDTakesPrecedenceOverName(t *testing.T) {
	a := &stubEntry{id: "tc1", name: "search"}
	b := &stubEntry{id: "tc2", name: "search"}
	entries := []toolCallEntry{a, b}

	// ID matches b, name would match a first.
	result := &types.MessageToolResult{ID: "tc2", Name: "search", Content: "matched-by-id"}
	matchResult(entries, result)

	if a.resolved {
		t.Error("entry a should not be resolved (ID match should win)")
	}
	if !b.resolved || b.result.Content != "matched-by-id" {
		t.Error("entry b should be resolved via ID match")
	}
}

func TestMatchResult_SkipsAlreadyResolved(t *testing.T) {
	a := &stubEntry{id: "tc1", name: "search", resolved: true}
	b := &stubEntry{id: "tc2", name: "search"}
	entries := []toolCallEntry{a, b}

	result := &types.MessageToolResult{ID: "", Name: "search", Content: "second"}
	matchResult(entries, result)

	if b.result == nil || b.result.Content != "second" {
		t.Error("should match second unresolved entry with same name")
	}
}

func TestMatchResult_NoMatch(t *testing.T) {
	a := &stubEntry{id: "tc1", name: "search"}
	entries := []toolCallEntry{a}

	result := &types.MessageToolResult{ID: "tc99", Name: "unknown", Content: "orphan"}
	matchResult(entries, result)

	if a.resolved {
		t.Error("entry a should not be resolved for non-matching result")
	}
}

func TestMatchResult_EmptyEntries(t *testing.T) {
	// Should not panic on empty entries.
	result := &types.MessageToolResult{ID: "tc1", Name: "search", Content: "x"}
	matchResult(nil, result)
	matchResult([]toolCallEntry{}, result)
}

func TestMatchResult_IDMatchSkipsResolvedByID(t *testing.T) {
	a := &stubEntry{id: "tc1", name: "search", resolved: true}
	b := &stubEntry{id: "tc1", name: "search"}
	entries := []toolCallEntry{a, b}

	result := &types.MessageToolResult{ID: "tc1", Name: "search", Content: "dup-id"}
	matchResult(entries, result)

	// a is already resolved, so b should get the match even though both have same ID.
	if !b.resolved || b.result.Content != "dup-id" {
		t.Error("should match second entry with same ID when first is resolved")
	}
}

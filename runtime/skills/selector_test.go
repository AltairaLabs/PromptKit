package skills

import (
	"context"
	"slices"
	"testing"
)

func testSkills() []SkillMetadata {
	return []SkillMetadata{
		{Name: "billing-lookup", Description: "Look up billing info"},
		{Name: "refund-processor", Description: "Process refunds"},
		{Name: "pci-compliance", Description: "PCI rules"},
		{Name: "knowledge-base", Description: "Search KB articles"},
		{Name: "escalation", Description: "Escalate to human"},
	}
}

func taggedSkills() []SkillMetadata {
	return []SkillMetadata{
		{
			Name:        "billing-lookup",
			Description: "Look up billing info",
			Metadata:    map[string]string{"tags": "billing,finance"},
		},
		{
			Name:        "refund-processor",
			Description: "Process refunds",
			Metadata:    map[string]string{"tags": "billing,compliance"},
		},
		{
			Name:        "pci-compliance",
			Description: "PCI rules",
			Metadata:    map[string]string{"tags": "compliance,security"},
		},
		{
			Name:        "knowledge-base",
			Description: "Search KB articles",
			// No tags key in metadata
			Metadata: map[string]string{"version": "2.0"},
		},
		{
			Name:        "escalation",
			Description: "Escalate to human",
			// No metadata at all
		},
	}
}

// --- ModelDrivenSelector tests ---

func TestModelDrivenSelector_ReturnsAll(t *testing.T) {
	sel := NewModelDrivenSelector()
	available := testSkills()

	names, err := sel.Select(context.Background(), "help me with billing", available)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 5 {
		t.Fatalf("expected 5 names, got %d", len(names))
	}

	expected := []string{
		"billing-lookup", "refund-processor", "pci-compliance",
		"knowledge-base", "escalation",
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("index %d: expected %q, got %q", i, expected[i], name)
		}
	}
}

func TestModelDrivenSelector_EmptyInput(t *testing.T) {
	sel := NewModelDrivenSelector()

	names, err := sel.Select(context.Background(), "anything", []SkillMetadata{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if names == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(names) != 0 {
		t.Fatalf("expected 0 names, got %d", len(names))
	}
}

func TestModelDrivenSelector_CancelledContext(t *testing.T) {
	sel := NewModelDrivenSelector()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	available := testSkills()
	names, err := sel.Select(ctx, "query", available)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 5 {
		t.Fatalf("expected 5 names even with cancelled context, got %d", len(names))
	}
}

// --- TagSelector tests ---

func TestTagSelector_MatchesSingleTag(t *testing.T) {
	sel := NewTagSelector([]string{"billing"})
	available := taggedSkills()

	names, err := sel.Select(context.Background(), "query", available)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(names), names)
	}
	if !slices.Contains(names, "billing-lookup") {
		t.Error("expected billing-lookup in results")
	}
	if !slices.Contains(names, "refund-processor") {
		t.Error("expected refund-processor in results")
	}
}

func TestTagSelector_MatchesMultipleTagsOR(t *testing.T) {
	sel := NewTagSelector([]string{"finance", "security"})
	available := taggedSkills()

	names, err := sel.Select(context.Background(), "query", available)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(names), names)
	}
	if !slices.Contains(names, "billing-lookup") {
		t.Error("expected billing-lookup in results")
	}
	if !slices.Contains(names, "pci-compliance") {
		t.Error("expected pci-compliance in results")
	}
}

func TestTagSelector_NoMatches(t *testing.T) {
	sel := NewTagSelector([]string{"nonexistent"})
	available := taggedSkills()

	names, err := sel.Select(context.Background(), "query", available)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if names == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(names) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(names))
	}
}

func TestTagSelector_SkillsWithoutTagsExcluded(t *testing.T) {
	sel := NewTagSelector([]string{"billing"})
	available := taggedSkills()

	names, err := sel.Select(context.Background(), "query", available)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// knowledge-base has metadata but no "tags" key — should be excluded
	if slices.Contains(names, "knowledge-base") {
		t.Error("knowledge-base should be excluded (no tags key)")
	}
	// escalation has no metadata at all — should be excluded
	if slices.Contains(names, "escalation") {
		t.Error("escalation should be excluded (no metadata)")
	}
}

func TestTagSelector_EmptyTags(t *testing.T) {
	sel := NewTagSelector([]string{})
	available := taggedSkills()

	names, err := sel.Select(context.Background(), "query", available)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if names == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(names) != 0 {
		t.Fatalf("expected 0 matches with empty tags, got %d", len(names))
	}
}

func TestNewTagSelector_Deduplicates(t *testing.T) {
	sel := NewTagSelector([]string{"billing", "billing", "billing"})

	if len(sel.tags) != 1 {
		t.Fatalf("expected 1 unique tag, got %d", len(sel.tags))
	}
	if !sel.tags["billing"] {
		t.Error("expected 'billing' tag to be present")
	}
}

// --- Interface compliance ---

func TestInterfaceCompliance(t *testing.T) {
	var _ SkillSelector = (*ModelDrivenSelector)(nil)
	var _ SkillSelector = (*TagSelector)(nil)
	var _ SkillSelector = (*EmbeddingSelector)(nil)
}

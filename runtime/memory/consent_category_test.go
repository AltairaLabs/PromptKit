package memory

import (
	"strings"
	"testing"
)

func TestKnownCategories_StableOrder(t *testing.T) {
	cats := KnownCategories()
	if len(cats) != 6 {
		t.Fatalf("expected 6 known categories, got %d", len(cats))
	}
	want := []ConsentCategory{
		CategoryIdentity, CategoryPreferences, CategoryContext,
		CategoryLocation, CategoryHealth, CategoryHistory,
	}
	for i, c := range want {
		if cats[i] != c {
			t.Errorf("cats[%d] = %q, want %q", i, cats[i], c)
		}
	}
}

func TestIsKnownCategory(t *testing.T) {
	cases := map[string]bool{
		"memory:identity":    true,
		"memory:preferences": true,
		"memory:context":     true,
		"memory:location":    true,
		"memory:health":      true,
		"memory:history":     true,
		"memory:other":       false, // not in the canonical taxonomy
		"identity":           false, // missing prefix
		"":                   false,
		"MEMORY:HEALTH":      false, // case-sensitive
	}
	for in, want := range cases {
		if got := IsKnownCategory(in); got != want {
			t.Errorf("IsKnownCategory(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCategoryRubric_MentionsAllKnownCategories(t *testing.T) {
	// The rubric is what extractor prompts splice in; if a new category
	// is added to the taxonomy, the rubric MUST mention it or extractors
	// won't know to emit it. This test pins that contract.
	for _, c := range KnownCategories() {
		if !strings.Contains(CategoryRubric, string(c)) {
			t.Errorf("CategoryRubric does not mention category %q", c)
		}
	}
}

func TestMemory_SetConsentCategory(t *testing.T) {
	m := &Memory{}
	m.SetConsentCategory(CategoryHealth)
	if got := m.Metadata[MetaKeyConsentCategory]; got != "memory:health" {
		t.Errorf("expected metadata[%s]=memory:health, got %v", MetaKeyConsentCategory, got)
	}
	if got := m.GetConsentCategory(); got != CategoryHealth {
		t.Errorf("GetConsentCategory = %q, want %q", got, CategoryHealth)
	}
}

func TestMemory_SetConsentCategory_EmptyIsNoop(t *testing.T) {
	m := &Memory{Metadata: map[string]any{"existing": "value"}}
	m.SetConsentCategory("")
	if _, ok := m.Metadata[MetaKeyConsentCategory]; ok {
		t.Error("empty category should not write to metadata")
	}
	if m.Metadata["existing"] != "value" {
		t.Error("existing metadata clobbered")
	}
}

func TestMemory_SetConsentCategory_WhitespaceIsNoop(t *testing.T) {
	m := &Memory{}
	m.SetConsentCategory("   ")
	if _, ok := m.Metadata[MetaKeyConsentCategory]; ok {
		t.Error("whitespace-only category should not write to metadata")
	}
}

func TestMemory_SetConsentCategory_AcceptsUnknownValues(t *testing.T) {
	// Per the doc contract: PromptKit does NOT validate, so a consumer
	// extending the taxonomy ad-hoc still gets their value stored.
	m := &Memory{}
	m.SetConsentCategory("memory:custom_thing")
	if m.Metadata[MetaKeyConsentCategory] != "memory:custom_thing" {
		t.Error("unknown category should be stored verbatim")
	}
}

func TestMemory_GetConsentCategory_UnsetReturnsEmpty(t *testing.T) {
	m := &Memory{}
	if got := m.GetConsentCategory(); got != "" {
		t.Errorf("expected empty string for unset category, got %q", got)
	}
	m.Metadata = map[string]any{"other": "value"}
	if got := m.GetConsentCategory(); got != "" {
		t.Errorf("expected empty string when key missing, got %q", got)
	}
}

package memory

import "strings"

// ConsentCategory is the well-known taxonomy used by consent-aware consumers
// (e.g. Omnia) to apply per-category retention, opt-outs, and PII rules at
// memory-write time. Values are stored in [Memory.Metadata] under the key
// [MetaKeyConsentCategory] regardless of which path produced the memory:
// the explicit `memory__remember` tool (LLM-supplied category arg) or an
// extractor stage that classifies and tags during write.
//
// PromptKit defines the vocabulary and helpers; semantics (retention,
// access control, redaction) are owned by the consumer.
type ConsentCategory string

// Known consent categories. The string values are the wire format consumers
// match against — do not change them without coordinating with downstream
// platforms.
const (
	CategoryIdentity    ConsentCategory = "memory:identity"    // Names, IDs, pronouns, contact details.
	CategoryPreferences ConsentCategory = "memory:preferences" // Likes, dislikes, settings, configuration.
	CategoryContext     ConsentCategory = "memory:context"     // Work / project context, domain knowledge, current task.
	CategoryLocation    ConsentCategory = "memory:location"    // Geographic info, addresses, IP.
	CategoryHealth      ConsentCategory = "memory:health"      // Health, dietary, medical, accessibility info.
	CategoryHistory     ConsentCategory = "memory:history"     // Past conversations, decisions, what was said before.
)

// KnownCategories returns the canonical set of categories in a stable order.
// Useful for filling option lists, validation tables, or rubric expansion.
func KnownCategories() []ConsentCategory {
	return []ConsentCategory{
		CategoryIdentity,
		CategoryPreferences,
		CategoryContext,
		CategoryLocation,
		CategoryHealth,
		CategoryHistory,
	}
}

// IsKnownCategory reports whether s exactly matches one of the canonical
// category strings. Unknown values are not rejected by the storage layer —
// this helper exists for consumer validation only.
func IsKnownCategory(s string) bool {
	for _, c := range KnownCategories() {
		if string(c) == s {
			return true
		}
	}
	return false
}

// CategoryRubric is a prompt fragment that consumer extractors splice into
// their extraction prompt so the extracting LLM tags each memory with one
// of the canonical categories. PromptKit owns the rubric so all consumers
// see the same vocabulary and downstream platforms can apply uniform rules.
//
// Usage:
//
//	prompt := basePrompt + "\n" + memory.CategoryRubric
const CategoryRubric = `For each extracted memory, choose ONE consent category:
  memory:identity     names, IDs, pronouns, contact details
  memory:preferences  likes, dislikes, settings, configuration
  memory:context      work/project context, domain knowledge, current task
  memory:location     geographic info, addresses, IP
  memory:health       health, dietary, medical, accessibility info
  memory:history      past conversations, decisions, what was said before
If none of the above fits, omit the category entirely so the platform's
fallback classifier handles it.`

// SetConsentCategory writes a category onto m.Metadata[MetaKeyConsentCategory],
// initializing the map when nil. Empty input is a no-op so callers can pass
// LLM output unconditionally without checking first. Unknown (non-canonical)
// values are accepted as-is — see [IsKnownCategory] for validation.
func (m *Memory) SetConsentCategory(c ConsentCategory) {
	v := strings.TrimSpace(string(c))
	if v == "" {
		return
	}
	if m.Metadata == nil {
		m.Metadata = map[string]any{}
	}
	m.Metadata[MetaKeyConsentCategory] = v
}

// GetConsentCategory returns the consent category previously written via
// [Memory.SetConsentCategory] or the `memory__remember` tool, or empty
// string when unset.
func (m *Memory) GetConsentCategory() ConsentCategory {
	if m.Metadata == nil {
		return ""
	}
	if v, ok := m.Metadata[MetaKeyConsentCategory].(string); ok {
		return ConsentCategory(v)
	}
	return ""
}

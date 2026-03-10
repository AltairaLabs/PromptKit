package evals

import "testing"

func TestNormalizeParams_KnownAlias(t *testing.T) {
	params := map[string]any{"words": []string{"bad", "evil"}}
	normalized := NormalizeParams("content_excludes", params)

	if _, ok := normalized["patterns"]; !ok {
		t.Fatal("expected 'words' to be remapped to 'patterns'")
	}
	if _, ok := normalized["words"]; ok {
		t.Fatal("expected 'words' to be removed after remapping")
	}
}

func TestNormalizeParams_UnknownEvalType(t *testing.T) {
	params := map[string]any{"foo": "bar"}
	normalized := NormalizeParams("unknown_type", params)

	// Should return the original map unchanged
	if normalized["foo"] != "bar" {
		t.Fatal("expected params to pass through for unknown eval type")
	}
}

func TestNormalizeParams_NoOverwriteExistingCanonical(t *testing.T) {
	params := map[string]any{
		"words":    []string{"legacy"},
		"patterns": []string{"canonical"},
	}
	normalized := NormalizeParams("content_excludes", params)

	// Should keep the canonical value, not overwrite with the alias
	patterns, ok := normalized["patterns"].([]string)
	if !ok {
		t.Fatal("expected 'patterns' to remain")
	}
	if len(patterns) != 1 || patterns[0] != "canonical" {
		t.Fatalf("expected canonical value preserved, got %v", patterns)
	}
	// The alias key should also be present since canonical exists
	if _, ok := normalized["words"]; !ok {
		t.Fatal("expected 'words' to remain when canonical key already exists")
	}
}

func TestNormalizeParams_MaxLengthAlias(t *testing.T) {
	params := map[string]any{"max_characters": 100}
	normalized := NormalizeParams("max_length", params)

	if _, ok := normalized["max"]; !ok {
		t.Fatal("expected 'max_characters' to be remapped to 'max'")
	}
}

func TestNormalizeParams_MinLengthAlias(t *testing.T) {
	params := map[string]any{"min_chars": 10}
	normalized := NormalizeParams("min_length", params)

	if _, ok := normalized["min"]; !ok {
		t.Fatal("expected 'min_chars' to be remapped to 'min'")
	}
}

func TestNormalizeParams_SentenceCountAlias(t *testing.T) {
	params := map[string]any{"max_sentences": 5}
	normalized := NormalizeParams("sentence_count", params)

	if _, ok := normalized["max"]; !ok {
		t.Fatal("expected 'max_sentences' to be remapped to 'max'")
	}
}

func TestNormalizeParams_FieldPresenceAlias(t *testing.T) {
	params := map[string]any{"required_fields": []string{"name", "email"}}
	normalized := NormalizeParams("field_presence", params)

	if _, ok := normalized["fields"]; !ok {
		t.Fatal("expected 'required_fields' to be remapped to 'fields'")
	}
}

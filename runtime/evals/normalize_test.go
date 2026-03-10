package evals

import (
	"testing"
)

func TestNormalizeParams(t *testing.T) {
	// content_excludes: "words" -> "patterns"
	params := NormalizeParams("content_excludes", map[string]any{
		"words": []string{"bad"},
	})
	if _, ok := params["patterns"]; !ok {
		t.Error("expected 'words' to be renamed to 'patterns'")
	}
	if _, ok := params["words"]; ok {
		t.Error("expected 'words' key to be removed after normalization")
	}
}

func TestNormalizeParams_NoAlias(t *testing.T) {
	input := map[string]any{"foo": "bar"}
	result := NormalizeParams("unknown_type", input)
	if result["foo"] != "bar" {
		t.Error("expected unknown types to pass through unchanged")
	}
}

func TestNormalizeParams_CanonicalAlreadyPresent(t *testing.T) {
	// When canonical key already exists, don't overwrite it
	params := NormalizeParams("content_excludes", map[string]any{
		"words":    []string{"old"},
		"patterns": []string{"new"},
	})
	if v, ok := params["patterns"].([]string); !ok || v[0] != "new" {
		t.Error("expected canonical key to take precedence")
	}
}

func TestApplyDefaults_BannedWords(t *testing.T) {
	// banned_words should get match_mode=word_boundary by default
	params := ApplyDefaults("banned_words", map[string]any{
		"words": []string{"bad"},
	})
	if params["match_mode"] != "word_boundary" {
		t.Errorf(
			"expected match_mode=word_boundary, got %v",
			params["match_mode"],
		)
	}
	// original params preserved
	if params["words"] == nil {
		t.Error("expected original params to be preserved")
	}
}

func TestApplyDefaults_UserOverride(t *testing.T) {
	// User-provided match_mode should override default
	params := ApplyDefaults("banned_words", map[string]any{
		"words":      []string{"bad"},
		"match_mode": "substring",
	})
	if params["match_mode"] != "substring" {
		t.Errorf(
			"expected user override match_mode=substring, got %v",
			params["match_mode"],
		)
	}
}

func TestApplyDefaults_NoDefaults(t *testing.T) {
	input := map[string]any{"foo": "bar"}
	result := ApplyDefaults("unknown_type", input)
	if result["foo"] != "bar" {
		t.Error("expected params to pass through when no defaults exist")
	}
}

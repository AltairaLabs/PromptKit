package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"judge", "prompt", 6},
		{"judge", "judges", 1},
		{"prompt", "promp", 1},
		{"anthrop", "anthropic", 2},
	}
	for _, tc := range cases {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			assert.Equal(t, tc.want, levenshtein(tc.a, tc.b))
			assert.Equal(t, tc.want, levenshtein(tc.b, tc.a), "should be symmetric")
		})
	}
}

func TestNearestMatches(t *testing.T) {
	t.Run("returns close match sorted by distance then name", func(t *testing.T) {
		got := nearestMatches("promp", []string{"prompt", "prompts", "model"})
		assert.Equal(t, []string{"prompt", "prompts"}, got)
	})

	t.Run("returns single best match", func(t *testing.T) {
		got := nearestMatches("anthrop", []string{"openai", "anthropic", "mock"})
		assert.Equal(t, []string{"anthropic"}, got)
	})

	t.Run("no candidates within distance 2", func(t *testing.T) {
		got := nearestMatches("judge", []string{"prompt", "model", "temperature"})
		assert.Empty(t, got)
	})

	t.Run("short target with unrelated candidates returns empty", func(t *testing.T) {
		got := nearestMatches("xyz", []string{"abc", "def", "ghi"})
		assert.Empty(t, got)
	})

	t.Run("short target with close match still returns it", func(t *testing.T) {
		got := nearestMatches("abc", []string{"abd"})
		assert.Equal(t, []string{"abd"}, got)
	})

	t.Run("empty target returns empty", func(t *testing.T) {
		assert.Empty(t, nearestMatches("", []string{"a", "b"}))
	})

	t.Run("empty candidates returns empty", func(t *testing.T) {
		assert.Empty(t, nearestMatches("foo", nil))
	})
}

func parseSchema(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

func TestLookupProperties(t *testing.T) {
	t.Run("direct properties on root", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{"a":{"type":"string"},"b":{"type":"number"}}
		}`)
		got := lookupProperties(schema, "(root)")
		assert.ElementsMatch(t, []string{"a", "b"}, got)
	})

	t.Run("nested direct properties", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{
				"spec":{"type":"object","properties":{"x":{},"y":{}}}
			}
		}`)
		got := lookupProperties(schema, "spec")
		assert.ElementsMatch(t, []string{"x", "y"}, got)
	})

	t.Run("ref to definitions", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{"spec":{"$ref":"#/definitions/Spec"}},
			"definitions":{"Spec":{"type":"object","properties":{"p":{},"q":{}}}}
		}`)
		got := lookupProperties(schema, "spec")
		assert.ElementsMatch(t, []string{"p", "q"}, got)
	})

	t.Run("ref to $defs", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{"spec":{"$ref":"#/$defs/Spec"}},
			"$defs":{"Spec":{"type":"object","properties":{"m":{},"n":{}}}}
		}`)
		got := lookupProperties(schema, "spec")
		assert.ElementsMatch(t, []string{"m", "n"}, got)
	})

	t.Run("two-level path through ref", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{"spec":{"$ref":"#/definitions/Spec"}},
			"definitions":{
				"Spec":{"type":"object","properties":{"judge_defaults":{"type":"object","properties":{"prompt":{}}}}}
			}
		}`)
		got := lookupProperties(schema, "spec.judge_defaults")
		assert.ElementsMatch(t, []string{"prompt"}, got)
	})

	t.Run("oneOf returns nil", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{
				"spec":{"oneOf":[
					{"properties":{"a":{}}},
					{"properties":{"b":{}}}
				]}
			}
		}`)
		got := lookupProperties(schema, "spec")
		assert.Nil(t, got)
	})

	t.Run("missing path returns nil", func(t *testing.T) {
		schema := parseSchema(t, `{"type":"object","properties":{"a":{}}}`)
		got := lookupProperties(schema, "nonexistent.deep.path")
		assert.Nil(t, got)
	})

	t.Run("unresolvable ref returns nil", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{"spec":{"$ref":"#/definitions/Missing"}}
		}`)
		got := lookupProperties(schema, "spec")
		assert.Nil(t, got)
	})
}

func TestLookupEnumValues(t *testing.T) {
	t.Run("direct enum on root property", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{
				"provider":{"type":"string","enum":["openai","anthropic","mock"]}
			}
		}`)
		got := lookupEnumValues(schema, "provider")
		assert.Equal(t, []string{"openai", "anthropic", "mock"}, got)
	})

	t.Run("enum behind ref", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{"spec":{"$ref":"#/definitions/Spec"}},
			"definitions":{
				"Spec":{"type":"object","properties":{
					"mode":{"type":"string","enum":["fast","slow"]}
				}}
			}
		}`)
		got := lookupEnumValues(schema, "spec.mode")
		assert.Equal(t, []string{"fast", "slow"}, got)
	})

	t.Run("numeric enum coerced to strings", func(t *testing.T) {
		schema := parseSchema(t, `{
			"type":"object",
			"properties":{"version":{"enum":[1,2,3]}}
		}`)
		got := lookupEnumValues(schema, "version")
		assert.Equal(t, []string{"1", "2", "3"}, got)
	})

	t.Run("missing field returns nil", func(t *testing.T) {
		schema := parseSchema(t, `{"type":"object","properties":{"a":{}}}`)
		assert.Nil(t, lookupEnumValues(schema, "nonexistent"))
	})

	t.Run("field without enum returns nil", func(t *testing.T) {
		schema := parseSchema(t, `{"type":"object","properties":{"a":{"type":"string"}}}`)
		assert.Nil(t, lookupEnumValues(schema, "a"))
	})
}

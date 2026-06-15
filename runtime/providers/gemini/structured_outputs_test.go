package gemini

import (
	"encoding/json"
	"testing"
)

// TestSanitizeGeminiSchema verifies that JSON Schema keywords Gemini's
// responseSchema rejects ($schema, additionalProperties, $defs, …) are stripped
// recursively while supported keywords are preserved.
func TestSanitizeGeminiSchema(t *testing.T) {
	in := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "x",
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"type": {"type": "string", "enum": ["a", "b"]},
			"items": {
				"type": "array",
				"items": {"type": "object", "additionalProperties": false, "properties": {"k": {"type": "string"}}}
			}
		},
		"required": ["type"],
		"$defs": {"unused": {"type": "string"}}
	}`)
	var schema interface{}
	if err := json.Unmarshal(in, &schema); err != nil {
		t.Fatal(err)
	}

	got := sanitizeGeminiSchema(schema).(map[string]interface{})

	for _, banned := range []string{"$schema", "$id", "additionalProperties", "$defs"} {
		if _, present := got[banned]; present {
			t.Errorf("top-level: %q should be stripped", banned)
		}
	}
	// Supported keywords preserved.
	if got["type"] != "object" {
		t.Errorf("type should be preserved, got %v", got["type"])
	}
	if _, ok := got["required"]; !ok {
		t.Error("required should be preserved")
	}
	// Nested object under properties.items.items must also be sanitized.
	props := got["properties"].(map[string]interface{})
	items := props["items"].(map[string]interface{})
	nested := items["items"].(map[string]interface{})
	if _, present := nested["additionalProperties"]; present {
		t.Error("nested additionalProperties should be stripped")
	}
	if _, ok := nested["properties"]; !ok {
		t.Error("nested properties should be preserved")
	}
}

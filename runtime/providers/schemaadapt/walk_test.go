package schemaadapt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWalk_VisitsEveryNestedSchema is the test the hand-rolled recursions in
// the adapters failed: an object nested inside an array's items, under $defs,
// or inside an anyOf branch is still a schema node and still has to be
// visited. #2055 was exactly that miss, one level down.
func TestWalk_VisitsEveryNestedSchema(t *testing.T) {
	raw := `{
	  "type": "object",
	  "properties": {
	    "flat": {"type": "string"},
	    "list": {"type": "array", "items": {"type": "object", "properties": {"deep": {"type": "string"}}}},
	    "tuple": {"type": "array", "prefixItems": [{"type": "object", "properties": {"t": {"type": "string"}}}]},
	    "choice": {"anyOf": [{"type": "object", "properties": {"a": {"type": "string"}}}, {"type": "null"}]},
	    "ref": {"$ref": "#/$defs/named"}
	  },
	  "$defs": {"named": {"type": "object", "properties": {"n": {"type": "string"}}}}
	}`

	var doc any
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))

	var objectNodes int
	Walk(doc, func(node map[string]any) {
		if IsObjectNode(node) {
			objectNodes++
		}
	})

	// root, list.items, tuple.prefixItems[0], choice.anyOf[0], $defs.named
	assert.Equal(t, 5, objectNodes,
		"every object node must be visited, including nested ones")
}

// TestWalk_DoesNotTreatUserPropertiesAsKeywords guards the reason this walks
// schema structure instead of every map: "properties", "items" and
// "additionalProperties" are also legal property names, and a blunt walk
// rewrites the caller's data model.
func TestWalk_DoesNotTreatUserPropertiesAsKeywords(t *testing.T) {
	raw := `{
	  "type": "object",
	  "properties": {
	    "additionalProperties": {"type": "string"},
	    "items": {"type": "string"},
	    "properties": {"type": "string"}
	  }
	}`

	var doc any
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))

	var visited int
	Walk(doc, func(map[string]any) { visited++ })

	// The root plus the three property schemas — and nothing more. A blunt
	// walk would descend into the string-typed "items" value as if it were a
	// keyword, which is harmless here but is the same mistake that lets a
	// sanitizer delete a property called "additionalProperties".
	assert.Equal(t, 4, visited)
}

func TestRewrite_LeavesUnparseableSchemaAlone(t *testing.T) {
	for name, raw := range map[string]string{
		"not json": `{"type": `,
		"an array": `[{"type":"object"}]`,
		"a string": `"nope"`,
	} {
		t.Run(name, func(t *testing.T) {
			in := json.RawMessage(raw)
			out := Rewrite(in, func(node map[string]any) {
				node["touched"] = true
			})
			assert.Equal(t, string(in), string(out),
				"%s: an input this package cannot parse is the API's error to "+
					"report, not something to rewrite or swallow", name)
		})
	}
}

func TestRewrite_AppliesTheVisitorAndReEncodes(t *testing.T) {
	out := Rewrite(json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
		func(node map[string]any) {
			if IsObjectNode(node) {
				node["additionalProperties"] = false
			}
		})

	assert.JSONEq(t,
		`{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false}`,
		string(out))
}

func TestIsObjectNode(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"explicit type", `{"type":"object"}`, true},
		{"properties without a type", `{"properties":{"a":{"type":"string"}}}`, true},
		{"nullable object union", `{"type":["object","null"]}`, true},
		{"a string", `{"type":"string"}`, false},
		{"an array", `{"type":"array","items":{"type":"string"}}`, false},
		{"a bare $ref", `{"$ref":"#/$defs/x"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var node map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &node))
			assert.Equal(t, tc.want, IsObjectNode(node))
		})
	}
}

func TestRewrite_EmptyInput(t *testing.T) {
	assert.Empty(t, Rewrite(nil, func(map[string]any) {}))
}

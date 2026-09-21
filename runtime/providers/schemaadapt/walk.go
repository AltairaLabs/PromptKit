// Package schemaadapt walks a JSON Schema so provider adapters can rewrite it
// into the dialect their API accepts.
//
// Every provider that takes a JSON Schema takes a different one. The rules are
// not merely different in degree — they contradict:
//
//	Anthropic  REQUIRES additionalProperties: false on every object node
//	Gemini     REJECTS  the additionalProperties keyword outright
//	OpenAI     REQUIRES additionalProperties: false AND every property in required
//
// So there is no schema a pack author can write that is portable, and the
// adapter — the only layer that knows which vendor it is talking to — has to
// do the rewriting. Each adapter supplies its own rules; this package supplies
// the traversal they all need, because the traversal is where the bugs are:
// issue #2055 was a schema that satisfied the rule at the top level and broke
// on an object nested inside an array's items.
package schemaadapt

import "encoding/json"

// typeObject is the JSON Schema type name for an object node.
const typeObject = "object"

// schemaBearingKeys are keywords whose value is itself a schema.
var schemaBearingKeys = []string{
	"items", "additionalItems", "contains", "additionalProperties",
	"propertyNames", "not", "if", "then", "else", "unevaluatedItems",
	"unevaluatedProperties",
}

// schemaMapKeys are keywords whose value is an object whose VALUES are schemas.
var schemaMapKeys = []string{
	"properties", "patternProperties", "$defs", "definitions",
	"dependentSchemas",
}

// schemaListKeys are keywords whose value is an array of schemas.
var schemaListKeys = []string{"anyOf", "allOf", "oneOf", "prefixItems"}

// Walk visits every schema node in a decoded JSON Schema document, parents
// before children, and calls visit on each.
//
// Traversal is structural: it descends only through keywords whose values are
// schemas. A blunt walk of every map in the document is tempting and wrong —
// it would treat a user property literally named "properties" or
// "additionalProperties" as a schema keyword and rewrite the caller's data
// model. $ref is a string and is not followed, which also means a recursive
// schema cannot send this into a loop.
func Walk(node any, visit func(map[string]any)) {
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	visit(obj)

	walkChildSchemas(obj, visit)
	walkChildSchemaMaps(obj, visit)
	walkChildSchemaLists(obj, visit)
}

// walkChildSchemas descends through keywords whose value is one schema.
func walkChildSchemas(obj map[string]any, visit func(map[string]any)) {
	for _, k := range schemaBearingKeys {
		if child, ok := obj[k]; ok {
			Walk(child, visit)
		}
	}
}

// walkChildSchemaMaps descends through keywords whose value is an object whose
// values are schemas.
func walkChildSchemaMaps(obj map[string]any, visit func(map[string]any)) {
	for _, k := range schemaMapKeys {
		children, ok := obj[k].(map[string]any)
		if !ok {
			continue
		}
		for _, child := range children {
			Walk(child, visit)
		}
	}
}

// walkChildSchemaLists descends through keywords whose value is an array of
// schemas.
func walkChildSchemaLists(obj map[string]any, visit func(map[string]any)) {
	for _, k := range schemaListKeys {
		children, ok := obj[k].([]any)
		if !ok {
			continue
		}
		for _, child := range children {
			Walk(child, visit)
		}
	}
}

// Rewrite decodes raw, applies visit to every schema node, and re-encodes.
//
// A schema that does not decode is returned unchanged: the adapters call this
// on the way to the wire, and a malformed schema is the API's error to report,
// with its own message, not something to swallow here.
func Rewrite(raw json.RawMessage, visit func(map[string]any)) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw
	}
	if _, ok := doc.(map[string]any); !ok {
		return raw
	}
	Walk(doc, visit)

	out, err := json.Marshal(doc)
	if err != nil {
		return raw
	}
	return out
}

// IsObjectNode reports whether a schema node describes a JSON object, and so
// is subject to the object rules the providers enforce.
//
// Both spellings count: "type": "object", and a node that declares properties
// without a type (common in hand-written schemas, and the providers treat it
// as an object).
func IsObjectNode(node map[string]any) bool {
	if _, ok := node["properties"]; ok {
		return true
	}
	switch t := node["type"].(type) {
	case string:
		return t == typeObject
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && s == typeObject {
				return true
			}
		}
	}
	return false
}

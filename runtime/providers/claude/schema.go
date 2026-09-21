package claude

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/schemaadapt"
)

// Anthropic's constrained-decoding grammar accepts a subset of JSON Schema,
// and it enforces that subset by rejecting the whole request. A caller cannot
// reasonably write to it: the same schema has to work on Gemini, which rejects
// the one keyword Anthropic demands. So the adapter rewrites it here, on the
// way out (#2055).
//
// The two lists below are MEASURED, not read off the documentation — the docs
// say string constraints are unsupported and minLength/maxLength/pattern are
// in fact accepted, while maxItems is rejected and minItems is not. Each entry
// was probed individually against api.anthropic.com on 2026-09-21, and
// TestClaude_SchemaAdaptation_UnsupportedKeywords_Live re-probes them, so the
// lists fail loudly rather than rot: if Anthropic starts accepting one, the
// live test says so and the entry comes out.

// unsupportedKeyword is one measured rejection. describable marks the ones
// worth restating in prose when they are removed — a range or a bound is
// guidance the model can still act on, while patternProperties is not.
type unsupportedKeyword struct {
	name        string
	describable bool
}

// unsupportedKeywordList is rejected with
// "For '<type>' type, property '<name>' is not supported".
//
// Stripping loosens the schema, which is the same trade the Anthropic Python
// and TypeScript SDKs make (they strip and validate client-side). PromptKit
// keeps the enforcement too: tool arguments are still validated against the
// caller's ORIGINAL descriptor by tools.SchemaValidator.ValidateArgs — this
// rewrite is for the wire only and the descriptor is untouched.
//
// Order is fixed so the appended constraint sentence is deterministic: an
// unstable description would change the request bytes every call and defeat
// prompt caching.
var unsupportedKeywordList = []unsupportedKeyword{
	// numbers
	{"minimum", true},
	{"maximum", true},
	{"exclusiveMinimum", true},
	{"exclusiveMaximum", true},
	{"multipleOf", true},
	// arrays ("minItems" IS supported; "maxItems" is not)
	{"maxItems", true},
	{"uniqueItems", true},
	{"contains", false},
	{"prefixItems", false},
	// objects
	{"minProperties", true},
	{"maxProperties", true},
	{"patternProperties", false},
	{"propertyNames", false},
	{"dependentRequired", false},
	// combinators — "not" is rejected as a keyword ("Schema keyword 'not' is
	// not supported"); "anyOf" and "allOf" are accepted.
	{"not", false},
}

// unsupportedKeywords indexes the list above for lookup.
var unsupportedKeywords = func() map[string]bool {
	m := make(map[string]bool, len(unsupportedKeywordList))
	for _, k := range unsupportedKeywordList {
		m[k.name] = true
	}
	return m
}()

// adaptSchemaForClaude rewrites a caller's JSON Schema into the dialect
// Anthropic's structured outputs and strict tool use accept:
//
//   - every object node gets additionalProperties: false (the API rejects both
//     its absence and an explicit true);
//   - unsupported keywords are dropped, and the constraint they expressed is
//     appended to the node's description so the model still sees it;
//   - oneOf becomes anyOf, which is accepted and means the same thing for
//     generation — dropping it outright would leave the node unconstrained.
//
// required is left exactly as written. Anthropic, unlike OpenAI strict mode,
// does not require every property to be required (verified live), so there is
// no reason to make a caller's optional field mandatory.
func adaptSchemaForClaude(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}

	var changes []string
	out := schemaadapt.Rewrite(raw, func(node map[string]any) {
		if schemaadapt.IsObjectNode(node) {
			if v, ok := node["additionalProperties"]; !ok || v != false {
				node["additionalProperties"] = false
				changes = append(changes, "additionalProperties")
			}
		}

		if branches, ok := node["oneOf"]; ok {
			if _, taken := node["anyOf"]; !taken {
				node["anyOf"] = branches
			}
			delete(node, "oneOf")
			changes = append(changes, "oneOf->anyOf")
		}

		stripped := stripUnsupported(node)
		changes = append(changes, stripped...)
	})

	if len(changes) == 0 {
		// Nothing needed changing, so hand back the caller's exact bytes.
		// Re-encoding would reorder keys for no reason, and the request body
		// is a prompt-cache key.
		return raw
	}
	logger.Debug("adapted a caller schema to Anthropic's accepted subset",
		"changes", strings.Join(dedupe(changes), ","))
	return out
}

// typeBearingKeywords are the keywords that give a node a concrete type.
// Anthropic rejects a node left without one — "Empty schema ({}) that accepts
// any JSON value is not supported. Please specify a concrete type."
var typeBearingKeywords = map[string]bool{
	"type": true, "enum": true, "const": true,
	"anyOf": true, "allOf": true, "$ref": true,
}

// stripUnsupported removes the keywords Anthropic rejects from one node and
// returns their names. Constraints that carried meaning are appended to the
// node's description first.
//
// A node that would be left with no type at all is NOT stripped. The only
// keyword this arises for is "not" — "anything except X" has no equivalent in
// the accepted subset, so there is nothing to rewrite it into. Stripping it
// anyway trades a 400 that names the keyword for a 400 that says "Empty
// schema", which is the same failure with the cause removed.
func stripUnsupported(node map[string]any) []string {
	var removed []string
	var described []string

	for _, k := range unsupportedKeywordList {
		if v, ok := node[k.name]; ok && k.describable {
			described = append(described, fmt.Sprintf("%s %v", k.name, v))
		}
	}

	for k := range node {
		if unsupportedKeywords[k] {
			removed = append(removed, k)
		}
	}
	if len(removed) > 0 && !retainsTypeAfterStrip(node, removed) {
		logger.Warn("keeping a schema keyword Anthropic rejects: removing it "+
			"would leave the node with no type, and there is no equivalent to "+
			"rewrite it into; the API will reject this request and name the keyword",
			"keywords", strings.Join(removed, ","))
		return nil
	}
	for _, k := range removed {
		delete(node, k)
	}

	if len(described) > 0 {
		appendConstraintNote(node, described)
	}
	sort.Strings(removed)
	return removed
}

// appendConstraintNote records a stripped constraint in the node's description.
// The model reads the description, so the guidance survives even though the
// keyword cannot be sent; enforcement, where it exists, is client-side.
func appendConstraintNote(node map[string]any, described []string) {
	note := "Constraints: " + strings.Join(described, ", ") + "."
	existing, _ := node["description"].(string)
	switch {
	case existing == "":
		node["description"] = note
	case strings.Contains(existing, note):
		// Already annotated — adapting an adapted schema must be a no-op, or
		// a retried request would grow a new sentence every attempt.
	default:
		node["description"] = existing + " " + note
	}
}

// retainsTypeAfterStrip reports whether the node would still declare a
// concrete type once the named keywords are removed.
func retainsTypeAfterStrip(node map[string]any, removing []string) bool {
	doomed := make(map[string]bool, len(removing))
	for _, k := range removing {
		doomed[k] = true
	}
	for k := range node {
		if typeBearingKeywords[k] && !doomed[k] {
			return true
		}
	}
	return false
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

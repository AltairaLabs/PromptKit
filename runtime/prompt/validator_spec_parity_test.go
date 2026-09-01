package prompt_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt/schema"
)

// deliberateOmission records a schema property that a compiled-pack struct
// intentionally does not carry, and why.
//
// The embedded schema is a verbatim mirror of the published PromptPack release
// and must never be edited to accommodate the runtime (see the header comment on
// runtime/prompt/schema/schema.go). So where the runtime deliberately declines
// to carry a spec property, that decision is recorded HERE, in Go, where a
// reviewer can see it — not by quietly deleting the property from the schema,
// which is how the copy drifted 143 leaves from the spec between 2026-06 and
// 2026-08 while still claiming to be v1.5.0.
//
// Adding an entry is a design decision, not a way to silence a failing test.
type deliberateOmission struct {
	property string
	reason   string
}

// assertStructMatchesSchemaDef pins a compiled-pack struct to a named $def in
// the embedded PromptPack schema: every schema property must exist on the
// struct, the struct may carry no field the schema doesn't define (mirroring
// additionalProperties:false), and required fields must not have omitempty (so a
// round-trip preserves the spec's required set).
//
// Properties listed in omissions are exempt from the first rule. An omission
// whose property IS present on the struct is itself an error, so the list cannot
// go stale silently.
//
// These guards live in runtime because the runtime is the source of truth for
// the pack format — Arena and packc build/test packs through the runtime, so the
// spec-parity guarantee must be runtime-owned, not stranded in the SDK.
// resolveSchemaRef walks a slash-separated path into the embedded schema and
// follows a $ref if the destination is one.
//
// Not every part of the pack format is a $def. metadata, compilation and
// template_engine are inline objects under the root's properties, which is
// exactly where governance landed in v1.6.0 — so a helper that could only
// address $defs could not have pinned the struct that dropped it.
func resolveSchemaRef(t *testing.T, root map[string]any, ref string) map[string]any {
	t.Helper()

	node := root
	for _, seg := range strings.Split(ref, "/") {
		next, ok := node[seg].(map[string]any)
		require.Truef(t, ok, "embedded schema has no %q (resolving %q)", seg, ref)
		node = next
	}
	if target, ok := node["$ref"].(string); ok {
		return resolveSchemaRef(t, root, strings.TrimPrefix(target, "#/"))
	}
	return node
}

// schemaProperties reads the properties, required set and openness of a schema
// node, resolving a union if the node is one.
//
// A oneOf/anyOf def has no properties of its own — SkillSource is a bare string,
// a SkillPathSource or an InlineSkill. Go has no sum type, so the runtime
// represents such a def as one flattened struct carrying every branch's fields,
// and the check has to compare against the same flattening: the union of the
// branches' properties. Only a property required by EVERY branch is required
// overall, because a document satisfying one branch need not carry the others'.
// Non-object branches (the bare string form) contribute nothing.
func schemaProperties(
	t *testing.T, root, def map[string]any, defName string,
) (expected, required map[string]bool, addlProps bool) {
	t.Helper()

	readInto := func(node map[string]any) (map[string]bool, map[string]bool, bool) {
		props, _ := node["properties"].(map[string]any)
		exp := make(map[string]bool, len(props))
		for name := range props {
			exp[name] = true
		}
		req := map[string]bool{}
		if reqList, ok := node["required"].([]any); ok {
			for _, r := range reqList {
				if name, ok := r.(string); ok {
					req[name] = true
				}
			}
		}
		open := true
		if v, ok := node["additionalProperties"].(bool); ok {
			open = v
		}
		return exp, req, open
	}

	if _, hasProps := def["properties"]; hasProps {
		return readInto(def)
	}

	var branches []any
	for _, key := range []string{"oneOf", "anyOf"} {
		if list, ok := def[key].([]any); ok {
			branches = list
			break
		}
	}
	require.NotEmptyf(t, branches, "%s must have properties or a oneOf/anyOf", defName)

	expected = map[string]bool{}
	var requiredInAll map[string]bool
	objectBranches := 0

	for _, raw := range branches {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if target, isRef := node["$ref"].(string); isRef {
			node = resolveSchemaRef(t, root, strings.TrimPrefix(target, "#/"))
		}
		if _, hasProps := node["properties"]; !hasProps {
			continue // a scalar branch, e.g. the bare-string form
		}
		objectBranches++

		exp, req, open := readInto(node)
		for name := range exp {
			expected[name] = true
		}
		if open {
			addlProps = true
		}
		if requiredInAll == nil {
			requiredInAll = req
			continue
		}
		for name := range requiredInAll {
			if !req[name] {
				delete(requiredInAll, name)
			}
		}
	}

	require.NotZerof(t, objectBranches, "%s has no object branch to compare against", defName)
	return expected, requiredInAll, addlProps
}

func assertStructMatchesSchemaDef(
	t *testing.T, structType reflect.Type, defName string, omissions ...deliberateOmission,
) {
	t.Helper()
	assertStructMatchesSchemaAt(t, structType, "$defs/"+defName, omissions...)
}

// assertStructMatchesSchemaAt is assertStructMatchesSchemaDef against an
// arbitrary location, given as a slash path from the schema root
// ("$defs/Tool", "properties/metadata").
func assertStructMatchesSchemaAt(
	t *testing.T, structType reflect.Type, ref string, omissions ...deliberateOmission,
) {
	t.Helper()

	raw := schema.GetEmbeddedSchema()
	require.NotEmpty(t, raw, "embedded promptpack schema must load")

	var root map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &root))

	def := resolveSchemaRef(t, root, ref)
	defName := ref

	expected, required, addlProps := schemaProperties(t, root, def, defName)

	actual := make(map[string]bool, structType.NumField())
	omitEmpty := make(map[string]bool, structType.NumField())
	for i := 0; i < structType.NumField(); i++ {
		tag := structType.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		actual[name] = true
		for _, opt := range parts[1:] {
			if opt == "omitempty" {
				omitEmpty[name] = true
			}
		}
	}

	omitted := make(map[string]string, len(omissions))
	for _, o := range omissions {
		omitted[o.property] = o.reason
	}
	for name, reason := range omitted {
		if !expected[name] {
			t.Errorf("stale omission: prompt.%s declares it omits %s property %q, "+
				"but the schema no longer defines it — drop the entry (reason was: %s)",
				structType.Name(), defName, name, reason)
		}
		if actual[name] {
			t.Errorf("stale omission: prompt.%s declares it omits %s property %q, "+
				"but the struct carries it — drop the entry (reason was: %s)",
				structType.Name(), defName, name, reason)
		}
	}

	for name := range expected {
		if _, deliberate := omitted[name]; deliberate {
			continue
		}
		if !actual[name] {
			t.Errorf("promptpack %s property %q is missing from prompt.%s", defName, name, structType.Name())
		}
	}
	if !addlProps {
		for name := range actual {
			if !expected[name] {
				t.Errorf("prompt.%s has JSON field %q not in the promptpack spec "+
					"(additionalProperties:false)", structType.Name(), name)
			}
		}
	}
	for name := range required {
		if omitEmpty[name] {
			t.Errorf("promptpack required field %q has omitempty on prompt.%s — "+
				"required fields must always serialize", name, structType.Name())
		}
	}
}

// The per-type cases that used to live here — Validator, Variable and
// PackPrompt — are now entries in packStructPins in pack_struct_coverage_test.go,
// checked by TestPinnedPackStructsMatchTheSpec over exactly the same schema defs
// and the same omissions. They moved rather than being dropped: a standalone
// case only covers the type someone remembered to write it for, and the pins
// list is enumerable, so a struct with no entry now fails instead of being
// quietly unwatched. That gap is what lost metadata.governance in v1.6.0.
//
// PackTool, ModelTestResultRef and ModelOverride had cases here until they
// became aliases for their generated types. A parity test on an alias is
// tautological — it compares the generated type to the schema it was generated
// from — so those were removed rather than left as reassuring noise. The
// generator's own coverage check and `make packspec-check` cover them now, and
// more strictly: they fail on any schema construct that is unaccounted for, not
// just on a tag mismatch.

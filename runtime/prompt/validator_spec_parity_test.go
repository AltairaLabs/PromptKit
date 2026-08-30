package prompt_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/prompt/schema"
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
func assertStructMatchesSchemaDef(
	t *testing.T, structType reflect.Type, defName string, omissions ...deliberateOmission,
) {
	t.Helper()

	raw := schema.GetEmbeddedSchema()
	require.NotEmpty(t, raw, "embedded promptpack schema must load")

	var root map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &root))

	defs, ok := root["$defs"].(map[string]any)
	require.True(t, ok, "embedded schema must have $defs")

	def, ok := defs[defName].(map[string]any)
	require.True(t, ok, "embedded schema must define $defs/"+defName)

	props, ok := def["properties"].(map[string]any)
	require.True(t, ok, defName+" must have properties")

	expected := make(map[string]bool, len(props))
	for name := range props {
		expected[name] = true
	}

	required := map[string]bool{}
	if reqList, ok := def["required"].([]any); ok {
		for _, r := range reqList {
			if name, ok := r.(string); ok {
				required[name] = true
			}
		}
	}

	addlProps := true
	if v, ok := def["additionalProperties"].(bool); ok {
		addlProps = v
	}

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

// TestValidatorStructMatchesPromptPackSpec pins prompt.Validator (the compiled
// validator; no top-level Message — that lives on the authoring ValidatorConfig).
func TestValidatorStructMatchesPromptPackSpec(t *testing.T) {
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.Validator{}), "Validator",
		deliberateOmission{
			property: "message",
			reason: "message is an authoring-time field on prompt.ValidatorConfig; " +
				"foldValidatorMessages folds it into params at compile, so the compiled " +
				"validator never carries it",
		},
	)
}

// TestVariableStructMatchesPromptPackSpec pins prompt.Variable (the compiled
// variable; no Binding — variable binding is a runtime concern on the authoring
// VariableMetadata, not part of the portable pack).
func TestVariableStructMatchesPromptPackSpec(t *testing.T) {
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.Variable{}), "Variable",
		deliberateOmission{
			property: "binding",
			reason: "variable binding (auto-populate from project/provider/workspace/secret/" +
				"configmap) is a runtime concern on the authoring prompt.VariableMetadata; " +
				"compileVariables drops it, so it is not part of the portable pack",
		},
	)
}

// TestPromptStructMatchesPromptPackSpec pins prompt.PackPrompt to $defs/Prompt.
func TestPromptStructMatchesPromptPackSpec(t *testing.T) {
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.PackPrompt{}), "Prompt")
}

// TestToolStructMatchesPromptPackSpec pins prompt.PackTool to $defs/Tool.
func TestToolStructMatchesPromptPackSpec(t *testing.T) {
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.PackTool{}), "Tool")
}

// TestTestedModelStructMatchesPromptPackSpec pins prompt.ModelTestResultRef to
// $defs/TestedModel. The name does not match the def — one of several such
// mismatches across the pack types.
func TestTestedModelStructMatchesPromptPackSpec(t *testing.T) {
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.ModelTestResultRef{}), "TestedModel")
}

// TestModelOverrideStructMatchesPromptPackSpec pins prompt.ModelOverride to
// $defs/ModelOverride.
func TestModelOverrideStructMatchesPromptPackSpec(t *testing.T) {
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.ModelOverride{}), "ModelOverride",
		deliberateOmission{
			property: "system_template_prefix",
			reason: "nothing in the runtime assembles a per-model template prefix; adding " +
				"the field would be vocabulary with a consumer and no producer",
		},
		deliberateOmission{
			property: "parameters",
			reason: "per-model parameter overrides are not applied by the runtime — " +
				"parameters are resolved at the prompt or provider level",
		},
	)
}

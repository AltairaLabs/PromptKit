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

// assertStructMatchesSchemaDef pins a compiled-pack struct to a named $def in
// the embedded PromptPack schema: every schema property must exist on the
// struct, the struct may carry no field the schema doesn't define (mirroring
// additionalProperties:false), and required fields must not have omitempty (so a
// round-trip preserves the spec's required set).
//
// These guards live in runtime because the runtime is the source of truth for
// the pack format — Arena and packc build/test packs through the runtime, so the
// spec-parity guarantee must be runtime-owned, not stranded in the SDK.
func assertStructMatchesSchemaDef(t *testing.T, structType reflect.Type, defName string) {
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

	for name := range expected {
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
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.Validator{}), "Validator")
}

// TestVariableStructMatchesPromptPackSpec pins prompt.Variable (the compiled
// variable; no Binding — variable binding is a runtime concern on the authoring
// VariableMetadata, not part of the portable pack).
func TestVariableStructMatchesPromptPackSpec(t *testing.T) {
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.Variable{}), "Variable")
}

// TestPromptStructMatchesPromptPackSpec pins prompt.PackPrompt to $defs/Prompt.
func TestPromptStructMatchesPromptPackSpec(t *testing.T) {
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.PackPrompt{}), "Prompt")
}

// TestToolStructMatchesPromptPackSpec pins prompt.PackTool to $defs/Tool.
func TestToolStructMatchesPromptPackSpec(t *testing.T) {
	assertStructMatchesSchemaDef(t, reflect.TypeOf(prompt.PackTool{}), "Tool")
}

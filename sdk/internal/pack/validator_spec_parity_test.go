package pack

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/prompt/schema"
)

// TestValidatorStructMatchesPromptPackSpec pins pack.Validator to the
// embedded PromptPack spec. Any drift — a renamed JSON tag, a removed
// field, a new field not in the spec — fails this test at build time.
// This is the mechanism that prevents the class of bug in issue #933.
func TestValidatorStructMatchesPromptPackSpec(t *testing.T) {
	raw := schema.GetEmbeddedSchema()
	require.NotEmpty(t, raw, "embedded promptpack schema must load")

	var root map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &root))

	defs, ok := root["$defs"].(map[string]any)
	require.True(t, ok, "embedded schema must have $defs")

	validatorDef, ok := defs["Validator"].(map[string]any)
	require.True(t, ok, "embedded schema must define $defs/Validator")

	props, ok := validatorDef["properties"].(map[string]any)
	require.True(t, ok, "Validator must have properties")

	expected := make(map[string]bool, len(props))
	for name := range props {
		expected[name] = true
	}

	required := map[string]bool{}
	if reqList, ok := validatorDef["required"].([]any); ok {
		for _, r := range reqList {
			if name, ok := r.(string); ok {
				required[name] = true
			}
		}
	}

	addlProps := true
	if v, ok := validatorDef["additionalProperties"].(bool); ok {
		addlProps = v
	}

	// Walk pack.Validator via reflection.
	tp := reflect.TypeOf(Validator{})
	actual := make(map[string]bool, tp.NumField())
	omitEmpty := make(map[string]bool, tp.NumField())
	for i := 0; i < tp.NumField(); i++ {
		field := tp.Field(i)
		tag := field.Tag.Get("json")
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

	// Assertion 1: every schema property exists on the struct.
	for name := range expected {
		if !actual[name] {
			t.Errorf("promptpack Validator property %q is missing from pack.Validator", name)
		}
	}

	// Assertion 2: the struct has no fields the schema doesn't define
	// (mirrors the schema's additionalProperties:false).
	if !addlProps {
		for name := range actual {
			if !expected[name] {
				t.Errorf("pack.Validator has JSON field %q not in the promptpack spec "+
					"(additionalProperties:false)", name)
			}
		}
	}

	// Assertion 3: required fields must not have omitempty — they must
	// always serialize, so a round-trip preserves the spec's required set.
	for name := range required {
		if omitEmpty[name] {
			t.Errorf("promptpack required field %q has omitempty on pack.Validator — "+
				"required fields must always serialize", name)
		}
	}
}

package generators

import (
	"sort"

	"github.com/invopop/jsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	// Blank import populates the default eval handler registry (handlers
	// self-register via init), so knownEvalTypes reflects exactly the set this
	// PromptKit build supports.
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
)

// knownEvalTypes returns the sorted list of eval/assertion handler types this
// PromptKit build registers. Assertions, evals and guardrails all resolve
// against this one registry, so a single source feeds every type field.
func knownEvalTypes() []string {
	types := evals.NewEvalTypeRegistry().Types()
	sort.Strings(types)
	return types
}

// applyOpenTypeEnum rewrites the named property of def into an "open enum": the
// known types are offered as suggestions (an enum branch) while any string is
// still accepted (a plain string branch). This makes the PromptKit/Arena schema
// self-documenting and editor-friendly without being strict — a different
// runtime may define types this build has never heard of, and those must not be
// rejected. Runtime correctness is still enforced separately by
// `promptarena validate` (ValidateAssertionTypes / ValidateParams).
func applyOpenTypeEnum(def *jsonschema.Schema, prop string, knownTypes []string, description string) {
	if def == nil || def.Properties == nil {
		return
	}
	if _, ok := def.Properties.Get(prop); !ok {
		return
	}

	enumVals := make([]interface{}, len(knownTypes))
	for i, t := range knownTypes {
		enumVals[i] = t
	}

	def.Properties.Set(prop, &jsonschema.Schema{
		AnyOf: []*jsonschema.Schema{
			{Enum: enumVals},
			{Type: jsonTypeString},
		},
		Description: description,
	})
}

// typeEnumDescription is the shared description for every open handler-type enum.
const typeEnumDescription = "Handler type. PromptKit-known types are suggested, " +
	"but values are not restricted to this list — a different runtime may define additional types."

// applyKnownTypeSuggestions decorates every definition in the schema that
// carries an eval-handler `type` field (AssertionConfig, EvalDef) with the open
// type enum. Assertions, evals and guardrails all resolve against the same
// registry, so a single suggestion set applies uniformly.
func applyKnownTypeSuggestions(schema *jsonschema.Schema) {
	if schema == nil || schema.Definitions == nil {
		return
	}
	known := knownEvalTypes()
	for _, defName := range []string{"AssertionConfig", "EvalDef"} {
		applyOpenTypeEnum(schema.Definitions[defName], "type", known, typeEnumDescription)
	}
}

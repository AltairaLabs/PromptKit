package main

import (
	"fmt"
	"sort"
	"strings"
)

// isOpenObject reports whether a def declares additionalProperties: true
// alongside named properties.
//
// Such a def is an ENVELOPE: the spec fixes some fields and explicitly invites
// runtimes to add their own. RFC 0006 says so in as many words about $defs/
// MetricDef — "the labels, help, aggregation, alert_threshold and slo fields are
// not part of this specification; they are runtime extensions enabled by
// additionalProperties: true. The spec defines the envelope; runtimes extend
// it."
//
// A generated struct with only the named fields silently discards every one of
// those extensions. That is not a shortcut, it is the inert-declaration bug
// again: the schema says "extra keys are meaningful here" and the type says
// nothing, so a pack carrying them loads and the data disappears. Four defs are
// affected, and promptkit uses the capability in two of them today
// (MetricDef.labels, and metadata.performance / metadata.changelog).
func (n Node) isOpenObject() bool {
	if !n.Has(kwProperties) {
		return false
	}
	ap, present := n.Raw["additionalProperties"]
	if !present {
		// JSON Schema's default is TRUE. The spec omits the keyword on 15
		// object nodes — Tool.parameters, Reducer, StepModifiers, the predicate
		// variants and others — so extras are legal there and promptkit's
		// validator already accepts them. Treating absent as closed would drop
		// data from packs that validate, which is the failure this whole
		// mechanism exists to prevent.
		//
		// Several of those omissions look like oversights rather than intent;
		// if the spec later adds additionalProperties:false the Extra fields
		// disappear on the next regeneration, which is the point of generating.
		return true
	}
	open, ok := ap.(bool)
	return ok && open
}

// emitExtraField writes the catch-all that makes an open object round-trip.
func emitExtraField(b *strings.Builder) {
	b.WriteString("\n\t// Extra carries properties the schema allows but does not name.\n")
	b.WriteString("\t// This def is additionalProperties:true — an envelope the spec expects\n")
	b.WriteString("\t// runtimes to extend — so unknown keys are preserved here rather than\n")
	b.WriteString("\t// dropped. Marshaled back as top-level properties, not nested.\n")
	b.WriteString("\tExtra map[string]any `json:\"-\" yaml:\"-\"`\n")
}

// emitOpenObjectJSON emits marshaling that lifts Extra to top-level properties
// on the way out and captures unknown keys on the way in.
// emitRequiredContainerInit emits marshaling that replaces a nil REQUIRED map or
// slice with an empty one.
//
// A required property must be present, and `null` is not an object or an array.
// ToolParameters.properties is the live case: a parameterless tool declared as
// {"type":"object"} marshaled to {"properties":null,"type":"object"}, and that
// document is forwarded verbatim to OpenAI and Anthropic as the function's
// parameter schema. It is invalid there and invalid against PromptPack's own
// Tool.parameters. The hand-written interface{} field preserved the author's
// bytes and never produced it.
func emitRequiredContainerInit(fields []requiredContainer) string {
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range fields {
		fmt.Fprintf(&b, "\tif v.%s == nil {\n\t\tv.%s = %s{}\n\t}\n", f.goName, f.goName, f.goType)
	}
	return b.String()
}

// requiredContainer is a required map/slice field, which must never marshal as
// null.
type requiredContainer struct{ goName, goType string }

func emitOpenObjectJSON(name string, jsonNames []string, reqContainers []requiredContainer) string {
	sort.Strings(jsonNames)
	var b strings.Builder
	fmt.Fprintf(&b, "\n// %sKnownFields are the properties the schema names. Anything else in the\n", name)
	fmt.Fprintf(&b, "// document belongs in Extra.\n")
	fmt.Fprintf(&b, "var %sKnownFields = map[string]bool{\n", name)
	for _, n := range jsonNames {
		fmt.Fprintf(&b, "\t%q: true,\n", n)
	}
	b.WriteString("}\n")

	fmt.Fprintf(&b, "\n// MarshalJSON writes the named properties plus everything in Extra, flattened\n")
	fmt.Fprintf(&b, "// to top level. A key in Extra that collides with a named property is dropped:\n")
	fmt.Fprintf(&b, "// the typed field is authoritative.\n")
	fmt.Fprintf(&b, "func (v %s) MarshalJSON() ([]byte, error) {\n", name)
	b.WriteString(emitRequiredContainerInit(reqContainers))
	fmt.Fprintf(&b, "\ttype plain %s\n", name)
	fmt.Fprintf(&b, "\tdata, err := json.Marshal(plain(v))\n")
	fmt.Fprintf(&b, "\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	fmt.Fprintf(&b, "\tif len(v.Extra) == 0 {\n\t\treturn data, nil\n\t}\n")
	fmt.Fprintf(&b, "\tmerged := map[string]any{}\n")
	fmt.Fprintf(&b, "\tif err := json.Unmarshal(data, &merged); err != nil {\n\t\treturn nil, err\n\t}\n")
	fmt.Fprintf(&b, "\tfor k, val := range v.Extra {\n")
	fmt.Fprintf(&b, "\t\tif !%sKnownFields[k] {\n\t\t\tmerged[k] = val\n\t\t}\n\t}\n", name)
	fmt.Fprintf(&b, "\treturn json.Marshal(merged)\n}\n")

	fmt.Fprintf(&b, "\n// UnmarshalJSON reads the named properties and captures every other key into\n")
	fmt.Fprintf(&b, "// Extra, so a runtime extension survives a load/save round trip instead of\n")
	fmt.Fprintf(&b, "// being silently discarded.\n")
	fmt.Fprintf(&b, "func (v *%s) UnmarshalJSON(data []byte) error {\n", name)
	fmt.Fprintf(&b, "\ttype plain %s\n\tvar named plain\n", name)
	fmt.Fprintf(&b, "\tif err := json.Unmarshal(data, &named); err != nil {\n\t\treturn err\n\t}\n")
	fmt.Fprintf(&b, "\t*v = %s(named)\n", name)
	fmt.Fprintf(&b, "\tvar raw map[string]json.RawMessage\n")
	fmt.Fprintf(&b, "\tif err := json.Unmarshal(data, &raw); err != nil {\n\t\treturn err\n\t}\n")
	fmt.Fprintf(&b, "\tfor k, rawVal := range raw {\n")
	fmt.Fprintf(&b, "\t\tif %sKnownFields[k] {\n\t\t\tcontinue\n\t\t}\n", name)
	fmt.Fprintf(&b, "\t\tvar val any\n")
	fmt.Fprintf(&b, "\t\tif err := json.Unmarshal(rawVal, &val); err != nil {\n\t\t\treturn err\n\t\t}\n")
	fmt.Fprintf(&b, "\t\tif v.Extra == nil {\n\t\t\tv.Extra = map[string]any{}\n\t\t}\n")
	fmt.Fprintf(&b, "\t\tv.Extra[k] = val\n\t}\n")
	fmt.Fprintf(&b, "\treturn nil\n}\n")
	return b.String()
}

// emitOpenObjectRegistry lists every generated type that accepts extensions.
//
// Exists so a conformance test can exercise all of them without a hand-written
// table that silently falls behind the schema — the failure mode being that a
// newly-open def gets marshaling nothing ever runs. The registry is generated,
// so it cannot drift.
func emitOpenObjectRegistry(names []string) string {
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("\n// OpenObjectPrototypes returns a zero value of every type that accepts\n")
	b.WriteString("// properties the schema does not name (additionalProperties is true, or\n")
	b.WriteString("// absent, which JSON Schema defines as true).\n//\n")
	b.WriteString("// Each value is a pointer, ready to unmarshal into. Intended for conformance\n")
	b.WriteString("// tests and tooling that needs to know which parts of a pack are extensible.\n")
	b.WriteString("func OpenObjectPrototypes() map[string]any {\n")
	b.WriteString("\treturn map[string]any{\n")
	for _, n := range names {
		fmt.Fprintf(&b, "\t\t%q: &%s{},\n", n, n)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// emitYAMLCodec emits YAML marshaling for a type that carries custom JSON
// codecs, by routing YAML through them.
//
// Required because the repo decodes YAML DIRECTLY into these structs —
// prompt.ParseConfig does yaml.Unmarshal into Config, whose Evals carry
// MetricDef — and a yaml library does not consult MarshalJSON/UnmarshalJSON.
// Without this, a generated type silently loses everything its JSON codec
// handles: metric.labels vanished from YAML configs, and a `requires` block or
// a composition step failed to parse at all. Both regressions were introduced by
// generating the types and were invisible until a YAML path was exercised.
//
// The signature `UnmarshalYAML(unmarshal func(any) error) error` is the
// obsolete-but-supported form in yaml.v3, chosen deliberately: it needs no yaml
// import, so packspec stays stdlib-only.
//
// Routing through JSON rather than reimplementing means the union, shorthand
// and extension rules have exactly one definition. A second implementation is
// how the two formats drift.
func emitYAMLCodec(name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n// UnmarshalYAML decodes YAML through the JSON codec above, so unions,\n")
	fmt.Fprintf(&b, "// shorthands and extensions behave identically in both formats.\n")
	fmt.Fprintf(&b, "func (v *%s) UnmarshalYAML(unmarshal func(any) error) error {\n", name)
	fmt.Fprintf(&b, "\treturn DecodeYAMLViaJSON(unmarshal, v)\n}\n")
	fmt.Fprintf(&b, "\n// MarshalYAML encodes through the JSON codec above, for the same reason.\n")
	fmt.Fprintf(&b, "func (v %s) MarshalYAML() (any, error) {\n", name)
	fmt.Fprintf(&b, "\treturn EncodeYAMLViaJSON(v)\n}\n")
	return b.String()
}

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
	open, ok := n.Raw["additionalProperties"].(bool)
	return ok && open && n.Has(kwProperties)
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
func emitOpenObjectJSON(name string, jsonNames []string) string {
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

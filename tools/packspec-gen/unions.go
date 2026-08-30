package main

import (
	"fmt"
	"sort"
	"strings"
)

// unionProperties resolves the full set of property names a def can present,
// following $ref and flattening oneOf/anyOf/allOf.
//
// This is what makes a union generatable at all: the closure is the field set
// of the flattened struct. Computing it also surfaced composition.Step missing
// `description`, which was silently dropped from every pack that set it.
func (s *Schema) unionProperties(n Node, seen map[string]bool) map[string]Node {
	if ref := n.Str("$ref"); ref != "" {
		return s.followRef(ref, seen)
	}
	out := map[string]Node{}
	for _, p := range n.PropertyNames() {
		out[p] = n.Property(p)
	}
	// First definition of a property name wins. Variants of one union describe
	// the same field, so a later variant redefining it is a spec inconsistency
	// rather than a distinct field.
	for _, kw := range []string{kwOneOf, kwAnyOf, kwAllOf} {
		for _, variant := range n.variantNodes(kw) {
			for k, v := range s.unionProperties(variant, seen) {
				if _, dup := out[k]; !dup {
					out[k] = v
				}
			}
		}
	}
	return out
}

func (s *Schema) followRef(ref string, seen map[string]bool) map[string]Node {
	target := strings.TrimPrefix(ref, "#/$defs/")
	if seen[target] {
		return map[string]Node{} // cycle guard
	}
	seen[target] = true
	def, ok := s.Defs[target]
	if !ok {
		return map[string]Node{}
	}
	return s.unionProperties(def, seen)
}

// variantNodes returns the object schemas listed under a combining keyword.
func (n Node) variantNodes(kw string) []Node {
	list, ok := n.Raw[kw].([]any)
	if !ok {
		return nil
	}
	out := make([]Node, 0, len(list))
	for _, raw := range list {
		if m, ok := raw.(map[string]any); ok {
			out = append(out, Node{Raw: m})
		}
	}
	return out
}

// isUnion reports whether a def is expressed as a variant choice.
func (n Node) isUnion() bool {
	return n.Has(kwOneOf) || n.Has(kwAnyOf)
}

// emitFlattenedUnion emits a union def as a single struct carrying every field
// any variant can present, all optional, with the discriminator included.
//
// Go has no sum type, but that does NOT mean a union cannot be generated — only
// that the representation is a choice. Flattening is the choice the hand-written
// types already made (composition.Step carries args, branches, predicate, then,
// else, tool ...), so generating it is mechanical rather than novel. It took the
// exclusion list from 16 defs to 2, and the unguarded share of the spec surface
// from a sixth to nothing.
//
// What this deliberately does NOT express: which field combinations are legal
// for a given discriminator. That is a validation concern the schema already
// enforces, and no Go struct shape can carry it.
func (e *Emitter) emitFlattenedUnion(name string, def Node) (string, error) {
	props := e.schema.unionProperties(def, map[string]bool{name: true})
	if len(props) == 0 {
		// No named fields to flatten. That does not mean the union is
		// unmodelable — a choice between bare shapes ("${ref}" string or a
		// free-form object) is a wrapper with a custom unmarshaller.
		if kinds := e.schema.variantKinds(def); len(kinds) > 0 {
			e.needsJSON = true
			return e.emitVariantWrapper(name, def, kinds), nil
		}
		return "", fmt.Errorf("$defs/%s: union has no properties to flatten and no recognizable "+
			"variant shapes — teach variantKinds about it, or add an exclusion with a reason", name)
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("\n")
	if d := def.Str("description"); d != "" {
		fmt.Fprintf(&b, "// %s %s\n", name, wrapComment(lowerFirst(d), "// "))
	}
	b.WriteString("//\n// Flattened from a oneOf/anyOf union: every field any variant can present,\n")
	b.WriteString("// all optional. Which combination is legal for a given discriminator is a\n")
	b.WriteString("// validation concern the schema enforces, not a shape this type can express.\n")
	fmt.Fprintf(&b, "type %s struct {\n", name)

	for i, prop := range names {
		qualified := name + "." + prop
		e.cov.DeclareProp(qualified)
		typ, err := e.goTypeNamed(props[prop], qualified, name+goName(prop))
		if err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString("\n")
		}
		if d := props[prop].Str("description"); d != "" {
			fmt.Fprintf(&b, "\t// %s %s\n", goName(prop), wrapComment(lowerFirst(d), "\t// "))
		}
		tag := prop + ",omitempty"
		fmt.Fprintf(&b, "\t%s %s `json:%q yaml:%q`\n", goName(prop), typ, tag, tag)
		e.cov.EmitProp(qualified)
	}

	shorthand := e.schema.scalarVariants(def)
	if len(shorthand) == 1 {
		fmt.Fprintf(&b, "\n\t// Shorthand holds the scalar form of this union when the pack used it\n")
		fmt.Fprintf(&b, "\t// instead of the object form. The spec defines what it expands to; this\n")
		fmt.Fprintf(&b, "\t// type preserves it verbatim rather than inventing the expansion.\n")
		fmt.Fprintf(&b, "\tShorthand %s `json:\"-\" yaml:\"-\"`\n", shorthand[0])
	}
	b.WriteString("}\n")

	if len(shorthand) == 1 {
		e.needsJSON = true
		b.WriteString(mixedUnionJSON(name, shorthand[0]))
	}
	return b.String(), nil
}

// mixedUnionJSON emits marshaling for a union that accepts a scalar shorthand
// as well as its object form.
func mixedUnionJSON(name, scalarType string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n// MarshalJSON writes the shorthand when it is set, otherwise the object.\n")
	fmt.Fprintf(&b, "func (v %s) MarshalJSON() ([]byte, error) {\n", name)
	fmt.Fprintf(&b, "\tif %s {\n\t\treturn json.Marshal(v.Shorthand)\n\t}\n", populated("v", "Shorthand", scalarType))
	fmt.Fprintf(&b, "\ttype plain %s\n", name)
	fmt.Fprintf(&b, "\treturn json.Marshal(plain(v))\n}\n")

	fmt.Fprintf(&b, "\n// UnmarshalJSON accepts either the scalar shorthand or the object form.\n")
	fmt.Fprintf(&b, "// Without this the scalar form fails to load — which is how the spec's own\n")
	fmt.Fprintf(&b, "// primary example for this def was silently rejected.\n")
	fmt.Fprintf(&b, "func (v *%s) UnmarshalJSON(data []byte) error {\n", name)
	fmt.Fprintf(&b, "\tvar shorthand %s\n", scalarType)
	fmt.Fprintf(&b, "\tif err := json.Unmarshal(data, &shorthand); err == nil {\n")
	fmt.Fprintf(&b, "\t\t*v = %s{Shorthand: shorthand}\n\t\treturn nil\n\t}\n", name)
	fmt.Fprintf(&b, "\ttype plain %s\n\tvar obj plain\n", name)
	fmt.Fprintf(&b, "\tif err := json.Unmarshal(data, &obj); err != nil {\n\t\treturn err\n\t}\n")
	fmt.Fprintf(&b, "\t*v = %s(obj)\n\treturn nil\n}\n", name)
	return b.String()
}

// scalarVariantField maps a JSON-Schema type to the Go field that holds it in a
// generated variant wrapper.
var scalarVariantField = map[string]struct{ name, typ string }{
	typeString: {"String", typeString},
	"integer":  {"Int", goInt},
	typeNum:    {"Number", goFloat},
	typeBool:   {"Bool", goBool},
	typeObject: {"Object", goAnyMap},
	typeArray:  {"Array", goAnySlice},
}

// scalarVariants returns the bare scalar shapes a union allows ALONGSIDE its
// object variants — the mixed case.
//
// ProviderRequirement is the spec's example: a bare string is shorthand for
// {key: <string>, role: "llm", required: true}, or the full object may be
// given. Flattening alone silently drops the string form: the generated struct
// rejected `- default`, the RFC's own primary example, with "cannot unmarshal
// string into Go value". A union is not modeled until BOTH shapes load.
//
// The expansion rule itself is spec semantics the generator cannot derive, so
// the scalar is preserved verbatim in Shorthand and the consumer applies the
// rule. Preserving it is the generator's job; interpreting it is not.
func (s *Schema) scalarVariants(n Node) []string {
	var kinds []string
	seen := map[string]bool{}
	hasObject := false
	for _, kw := range []string{kwOneOf, kwAnyOf} {
		for _, v := range n.variantNodes(kw) {
			if v.isObjectVariant() {
				hasObject = true
				continue
			}
			t := v.PrimaryType()
			f, ok := scalarVariantField[t]
			if !ok || t == typeObject || seen[t] {
				continue
			}
			seen[t] = true
			kinds = append(kinds, f.typ)
		}
	}
	if !hasObject || len(kinds) != 1 {
		// Only the single-scalar-plus-object case is handled. Anything else
		// would need a shape this generator does not emit, and must say so
		// rather than quietly dropping a variant.
		return nil
	}
	return kinds
}

// isObjectVariant reports whether a union member is a structured object rather
// than a bare shape.
func (n Node) isObjectVariant() bool {
	return n.Has(kwProperties) || n.Str("$ref") != ""
}

// variantKinds returns the distinct scalar/free-form types a union can be, or
// nil if any variant is something richer (named properties, a $ref).
//
// A union like StepInput — "a '${ref}' string, or a free-form object" — has an
// empty property closure, so it cannot be flattened into a struct. That does NOT
// make `any` the only option: the two shapes are perfectly modelable as a small
// wrapper with a custom unmarshaller. `any` forces every caller to type-switch
// and documents nothing.
func (s *Schema) variantKinds(n Node) []string {
	var kinds []string
	seen := map[string]bool{}
	for _, kw := range []string{kwOneOf, kwAnyOf} {
		for _, v := range n.variantNodes(kw) {
			if v.isObjectVariant() {
				return nil // richer than a bare shape; flattening applies instead
			}
			t := v.PrimaryType()
			if _, ok := scalarVariantField[t]; !ok {
				return nil
			}
			if !seen[t] {
				seen[t] = true
				kinds = append(kinds, t)
			}
		}
	}
	sort.Strings(kinds)
	return kinds
}

// emitVariantWrapper emits a union of bare shapes as a struct with one field per
// shape plus JSON marshaling that picks the right one.
//
// Exactly one field is populated. Marshaling prefers the first populated field
// in declaration order, which round-trips because only one is ever set by
// UnmarshalJSON.
func (e *Emitter) emitVariantWrapper(name string, def Node, kinds []string) string {
	var b strings.Builder
	b.WriteString("\n")
	if d := def.Str("description"); d != "" {
		fmt.Fprintf(&b, "// %s %s\n", name, wrapComment(lowerFirst(d), "// "))
	}
	b.WriteString("//\n// A union of bare shapes, so there are no named fields to flatten. Exactly one\n")
	b.WriteString("// field below is populated; UnmarshalJSON decides which from the JSON shape.\n")
	fmt.Fprintf(&b, "type %s struct {\n", name)
	for _, k := range kinds {
		f := scalarVariantField[k]
		fmt.Fprintf(&b, "\t// %s is set when the value is a JSON %s.\n", f.name, k)
		fmt.Fprintf(&b, "\t%s %s `json:\"-\" yaml:\"-\"`\n", f.name, f.typ)
	}
	b.WriteString("}\n")

	// MarshalJSON
	fmt.Fprintf(&b, "\n// MarshalJSON writes whichever shape is populated.\n")
	fmt.Fprintf(&b, "func (v %s) MarshalJSON() ([]byte, error) {\n", name)
	for _, k := range kinds {
		f := scalarVariantField[k]
		fmt.Fprintf(&b, "\tif %s {\n\t\treturn json.Marshal(v.%s)\n\t}\n", populated("v", f.name, f.typ), f.name)
	}
	b.WriteString("\treturn []byte(\"null\"), nil\n}\n")

	// UnmarshalJSON
	fmt.Fprintf(&b, "\n// UnmarshalJSON accepts any of the union's shapes and rejects the rest, so an\n")
	fmt.Fprintf(&b, "// unexpected shape is an error rather than a silently empty value.\n")
	fmt.Fprintf(&b, "func (v *%s) UnmarshalJSON(data []byte) error {\n", name)
	fmt.Fprintf(&b, "\t*v = %s{}\n", name)
	for _, k := range kinds {
		f := scalarVariantField[k]
		fmt.Fprintf(&b, "\tvar as%s %s\n", f.name, f.typ)
		fmt.Fprintf(&b, "\tif err := json.Unmarshal(data, &as%s); err == nil {\n", f.name)
		fmt.Fprintf(&b, "\t\tv.%s = as%s\n\t\treturn nil\n\t}\n", f.name, f.name)
	}
	fmt.Fprintf(&b, "\treturn fmt.Errorf(\"%s: expected %s, got %%s\", string(data))\n}\n",
		name, strings.Join(kinds, " or "))
	return b.String()
}

// populated renders the "this field is set" test for a variant field.
func populated(recv, field, typ string) string {
	switch typ {
	case "string":
		return recv + "." + field + ` != ""`
	case "int", "float64":
		return recv + "." + field + " != 0"
	case "bool":
		return recv + "." + field
	default: // maps and slices
		return recv + "." + field + " != nil"
	}
}

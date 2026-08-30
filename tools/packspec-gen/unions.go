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
		// A union of scalars (a "${ref}" string or a free-form object) flattens
		// to nothing. Emitting an empty struct would be worse than useless: it
		// would accept no field at all while looking like a real type. Require
		// an explicit exclusion so the choice is visible.
		return "", fmt.Errorf("$defs/%s: union presents no properties, so there is no struct to "+
			"flatten it into — add it to exclusions.go with a reason (`any` is usually the "+
			"complete representation for these)", name)
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
	b.WriteString("}\n")
	return b.String(), nil
}

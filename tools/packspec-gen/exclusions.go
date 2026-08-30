package main

import "fmt"

// Exclusions are the parts of the spec this generator deliberately does not
// emit, each with a reason a reviewer can weigh.
//
// This is the pressure valve that keeps the coverage check honest. Without it,
// the only way past an unhandled construct is to weaken the check — and a check
// that gets weakened whenever it fires stops being a check. With it, every gap
// is one line in one file, in the open.
//
// Adding an entry is a design decision. It is not a way to make the build pass.
type Exclusions struct {
	defs  map[string]string // $def name -> reason
	props map[string]string // "Def.property" -> reason
	used  map[string]bool
}

func NewExclusions() *Exclusions {
	e := &Exclusions{
		used: map[string]bool{},
		defs: map[string]string{
			// Only defs that present NO properties remain excluded. A union with
			// properties is generated as a flattened struct — Go has no sum
			// type, but flattening is a representation choice, not an
			// impossibility, and it is the choice the hand-written types
			// already made. Excluding them meant 16 of 49 defs went unchecked;
			// generating them keeps them under the coverage gate instead.
			"StepInput": "presents no properties — a '${ref}' string or a free-form object; " +
				"`any` is the complete representation, not a lossy one",
			"ProviderRequirement": "presents no properties — a provider name string or a " +
				"capability object. RFC 0012 is also unimplemented in promptkit; that is a " +
				"real gap, tracked separately, not a generation gap",
		},
		props: map[string]string{},
	}
	return e
}

func (e *Exclusions) Def(name string) string {
	if r, ok := e.defs[name]; ok {
		e.used["def:"+name] = true
		return r
	}
	return ""
}

func (e *Exclusions) Prop(qualified string) string {
	if r, ok := e.props[qualified]; ok {
		e.used["prop:"+qualified] = true
		return r
	}
	return ""
}

func (e *Exclusions) Count() int { return len(e.defs) + len(e.props) }

// Stale reports exclusions that no longer correspond to anything in the schema.
// An exclusion list that outlives what it excludes is how a stale workaround
// survives a spec change unnoticed.
func (e *Exclusions) Stale(c *Coverage) []string {
	declaredDefs := map[string]bool{}
	for _, d := range c.declaredDefs {
		declaredDefs[d] = true
	}
	declaredProps := map[string]bool{}
	for _, p := range c.declaredProps {
		declaredProps[p] = true
	}

	var stale []string
	for name := range e.defs {
		if !declaredDefs[name] {
			stale = append(stale, fmt.Sprintf(
				"    exclusion for $def %q, which the spec no longer defines — drop it", name))
		}
	}
	for qualified := range e.props {
		if !declaredProps[qualified] {
			stale = append(stale, fmt.Sprintf(
				"    exclusion for property %q, which the spec no longer defines — drop it", qualified))
		}
	}
	if len(stale) == 0 {
		return nil
	}
	return []string{"stale exclusions:\n" + joinLines(stale)}
}

func joinLines(s []string) string {
	out := ""
	for i, l := range s {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

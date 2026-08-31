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
		// Empty. Every $def in the spec is generated.
		//
		// This list exists as the pressure valve for a construct Go genuinely
		// cannot represent — but three separate claims that something was
		// unrepresentable turned out to be false on inspection:
		//
		//   - the six oneOf/anyOf unions: flattening is a representation choice,
		//     not an impossibility, and it is the one the hand-written types
		//     already made
		//   - ProviderRequirement, excluded as "presents no properties": it
		//     presents five, on its object variant
		//   - StepInput, a string-or-object: a wrapper with a custom
		//     unmarshaller models it exactly; `any` was never the only option
		//
		// So the bar for adding an entry is high: demonstrate that no Go shape
		// carries the information, rather than that none is convenient.
		defs:  map[string]string{},
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

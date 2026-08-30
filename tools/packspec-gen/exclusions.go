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

// Reasons shared by the union-variant exclusions.
const (
	reasonStepVariant      = "variant of the Step union; see Step"
	reasonPredicateVariant = "variant of the Predicate union; see Predicate"
)

func NewExclusions() *Exclusions {
	e := &Exclusions{
		used: map[string]bool{},
		defs: map[string]string{
			// The six oneOf/anyOf unions. Go has no sum type, so a generator can
			// only emit `any` (which loses every field name) or a flattened
			// struct guessing at which variants may combine. The hand-written
			// types in runtime/composition already flatten these deliberately,
			// with the discriminator (`kind`) modeled — that is a better
			// artifact than anything generated here would be.
			//
			// Evaluated, not assumed: atombender/go-jsonschema emits
			// `interface{}` for four of these six.
			"Predicate":            "oneOf union — hand-written as composition.Predicate; a generator can only emit `any`",
			"ProviderRequirement":  "oneOf union — RFC 0012; not implemented in promptkit at all yet, see #TODO",
			"SkillSource":          "oneOf union — hand-written as prompt.SkillSourceConfig with a custom UnmarshalJSON",
			"Step":                 "oneOf union over the five *Step kinds — hand-written as composition.Step",
			"StepInput":            "oneOf union — hand-written as composition.StepInput",
			"TerminationPredicate": "anyOf union — hand-written as composition.Termination",

			// The five Step variants exist only as members of the Step union.
			// Emitting them standalone would produce types nothing references.
			"PromptStep":   reasonStepVariant,
			"AgentStep":    reasonStepVariant,
			"ToolStep":     reasonStepVariant,
			"BranchStep":   reasonStepVariant,
			"ParallelStep": reasonStepVariant,

			// Predicate variants, likewise.
			"ComparePredicate": reasonPredicateVariant,
			"ExistsPredicate":  reasonPredicateVariant,
			"AllOfPredicate":   reasonPredicateVariant,
			"AnyOfPredicate":   reasonPredicateVariant,
			"NotPredicate":     reasonPredicateVariant,
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

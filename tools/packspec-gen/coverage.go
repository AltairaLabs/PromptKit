package main

import (
	"fmt"
	"sort"
	"strings"
)

// Coverage is the reason this generator exists in this form.
//
// A generator that silently skips what it cannot express rebuilds the problem it
// was written to solve, one level up: the schema declares vocabulary, the
// generated types quietly omit it, and a pack using it is dropped without a
// word. That is the inert-declaration pattern — the repo's most common latent
// bug — and promptkit already carries several instances of it that were found
// only by hand-diffing the schema against the Go types ($defs.DocumentConfig,
// Parameters.frequency_penalty, TestedModel.notes, top-level requires).
//
// So the rule here is: EVERYTHING in the schema is accounted for, or the
// generator fails. Every $def either produces a Go type or is listed in
// exclusions with a reason. Every property either produces a field or is
// excluded. Every JSON-Schema keyword is either acted on, or classified as
// shape-irrelevant. Anything unrecognized is an error, never a skip.
//
// The practical consequence: when the spec adds a construct we do not handle,
// the build breaks and names it. That is the whole point.
type Coverage struct {
	// keywords seen anywhere in the document, mapped to where they were found.
	keywords map[string][]string
	// defs that produced a Go type.
	emittedDefs map[string]bool
	// properties that produced a Go field, keyed "Def.property".
	emittedProps map[string]bool
	// everything the schema declares, to be reconciled against the emitted sets.
	declaredDefs  []string
	declaredProps []string
}

func NewCoverage() *Coverage {
	return &Coverage{
		keywords:     map[string][]string{},
		emittedDefs:  map[string]bool{},
		emittedProps: map[string]bool{},
	}
}

func (c *Coverage) SeeKeyword(kw, path string) { c.keywords[kw] = append(c.keywords[kw], path) }
func (c *Coverage) DeclareDef(name string)     { c.declaredDefs = append(c.declaredDefs, name) }
func (c *Coverage) DeclareProp(qualified string) {
	c.declaredProps = append(c.declaredProps, qualified)
}
func (c *Coverage) EmitDef(name string)       { c.emittedDefs[name] = true }
func (c *Coverage) EmitProp(qualified string) { c.emittedProps[qualified] = true }

// Keyword names used in more than one place.
const (
	kwProperties = "properties"
	kwDefs       = "$defs"
	kwConst      = "const"
	kwDefault    = "default"
	kwEnum       = "enum"
	kwExamples   = "examples"
)

// shapeKeywords affect the Go type and must be acted on by the emitter.
var shapeKeywords = map[string]bool{
	"type": true, kwProperties: true, "$ref": true, "items": true,
	"required": true, "additionalProperties": true, kwDefs: true,
	"oneOf": true, "anyOf": true, "allOf": true, "if": true, "then": true, "else": true,
}

// validationKeywords constrain values the schema validator already enforces at
// load time. They have no bearing on the Go type: a `minimum: 0` on an integer
// is still an int. Listing them explicitly — rather than ignoring unknown
// keywords by default — is what makes an unrecognized keyword an error.
var validationKeywords = map[string]bool{
	"minimum": true, "maximum": true, "minLength": true, "maxLength": true,
	"pattern": true, "minItems": true, "maxItems": true, "minProperties": true,
	"maxProperties": true, "format": true, kwEnum: true, kwConst: true,
	"uniqueItems": true, "multipleOf": true,
}

// documentationKeywords carry no meaning for either shape or validation.
var documentationKeywords = map[string]bool{
	"description": true, "title": true, kwExamples: true, kwDefault: true,
	"$schema": true, "$id": true, "$comment": true, "version": true, "deprecated": true,
}

// Reconcile returns one error describing everything unaccounted for, or nil.
func (c *Coverage) Reconcile(ex *Exclusions) error {
	var problems []string
	for _, p := range []string{
		c.unknownKeywordProblem(),
		c.missingDefProblem(ex),
		c.missingPropProblem(ex),
	} {
		if p != "" {
			problems = append(problems, p)
		}
	}
	problems = append(problems, ex.Stale(c)...)

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("schema coverage is incomplete:\n\n%s", strings.Join(problems, "\n\n"))
}

func (c *Coverage) unknownKeywordProblem() string {
	var unknown []string
	for kw, paths := range c.keywords {
		if shapeKeywords[kw] || validationKeywords[kw] || documentationKeywords[kw] {
			continue
		}
		unknown = append(unknown, fmt.Sprintf("    %-24s first seen at %s", kw, paths[0]))
	}
	if len(unknown) == 0 {
		return ""
	}
	sort.Strings(unknown)
	return "unrecognized JSON-Schema keywords — the spec uses a construct this generator does not " +
		"understand. Handle it in emit.go, or classify it in coverage.go as validation-only/" +
		"documentation if it cannot affect the Go type:\n" + strings.Join(unknown, "\n")
}

func (c *Coverage) missingDefProblem(ex *Exclusions) string {
	var missing []string
	for _, d := range c.declaredDefs {
		if c.emittedDefs[d] || ex.Def(d) != "" {
			continue
		}
		missing = append(missing, "    "+d)
	}
	if len(missing) == 0 {
		return ""
	}
	sort.Strings(missing)
	return "$defs in the spec with no generated Go type. Generate them, or add an exclusion with a " +
		"reason in exclusions.go:\n" + strings.Join(missing, "\n")
}

func (c *Coverage) missingPropProblem(ex *Exclusions) string {
	var missing []string
	for _, p := range c.declaredProps {
		if c.emittedProps[p] || ex.Prop(p) != "" {
			continue
		}
		missing = append(missing, "    "+p)
	}
	if len(missing) == 0 {
		return ""
	}
	sort.Strings(missing)
	return "spec properties with no generated Go field — a pack setting these would be silently " +
		"dropped:\n" + strings.Join(missing, "\n")
}

// Summary is printed on success so the excluded set stays visible rather than
// becoming a file nobody opens.
func (c *Coverage) Summary(ex *Exclusions) string {
	var b strings.Builder
	fmt.Fprintf(&b, "covered %d/%d $defs, %d/%d properties",
		len(c.emittedDefs), len(c.declaredDefs), len(c.emittedProps), len(c.declaredProps))
	if n := ex.Count(); n > 0 {
		fmt.Fprintf(&b, "; %d deliberate exclusions", n)
	}
	return b.String()
}

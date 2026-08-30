// Package composition defines RFC 0010 workflow-composition types and their
// validation. It is a leaf package: it imports neither runtime/pipeline nor
// runtime/workflow, so workflow.State can reference these types without a cycle.
package composition

import "github.com/AltairaLabs/PromptKit/runtime/packspec"

// StepKind identifies a composition step's kind.
type StepKind = string

// Step kinds (RFC 0010 v1).
const (
	KindPrompt   StepKind = "prompt"
	KindAgent    StepKind = "agent"
	KindTool     StepKind = "tool"
	KindBranch   StepKind = "branch"
	KindParallel StepKind = "parallel"
)

// Reduce strategies (RFC 0010 v1).
const (
	ReduceAppend  = "append"
	ReduceReplace = "replace"
	ReduceBarrier = "barrier"
)

// Composition is a named step graph over the pack's prompts, tools, and evals.
//
// Generated from the schema: an ALIAS for packspec.Composition.
type Composition = packspec.Composition

// Step is a single node in a composition graph. Only the fields legal for Kind
// are set; per-kind legality is enforced by Validate.
//
// Generated from the schema: an ALIAS for packspec.Step. The schema expresses
// this as a oneOf over the five *Step kinds; Go has no sum type, so the
// generator flattens it exactly as this hand-written type already did, keeping
// the `kind` discriminator.
type Step = packspec.Step

// StepInputValue returns a step's input in the form the resolver expects: the
// reference string, the literal object, or nil.
//
// The spec models input as a union — a "${...}" reference or an object of
// literals and references — so the generated field is a small wrapper rather
// than `any`. This unwraps it in one place instead of at each call site.
func StepInputValue(s *Step) any {
	if s == nil || s.Input == nil {
		return nil
	}
	if s.Input.Object != nil {
		return s.Input.Object
	}
	// An explicitly empty string is still an input. Returning nil for it would
	// hide the field from validateKind's allowed-field check, so a step kind
	// that may not carry `input` could declare an empty one and pass — a
	// validation guard weakened by the representation change, not by intent.
	return s.Input.String
}

// Termination bounds an agent step's tool loop. anyOf max_steps | tool_called.
//
// Generated from the schema: an ALIAS for packspec.TerminationPredicate.
type Termination = packspec.TerminationPredicate

// Reducer declares how parallel branch outputs merge. Both fields required.
// Generated from the schema: an ALIAS for packspec.Reducer, not a copy. The
// hand-written struct was field-for-field identical to $defs/Reducer.
type Reducer = packspec.Reducer

// StepModifiers are optional per-step behaviors (RFC 0010 v1: retry, eval).
//
// Generated from the schema: an ALIAS for packspec.StepModifiers.
type StepModifiers = packspec.StepModifiers

// RetryModifier re-runs a step on error up to MaxAttempts.
//
// An ALIAS for the generated packspec.StepModifiersRetry — the schema nests
// this shape inside StepModifiers rather than naming it, so the generator
// hoists it under a derived name. Field-for-field identical; the alias keeps
// the local name every call site already uses.
type RetryModifier = packspec.StepModifiersRetry

// Predicate is the constrained predicate language. Exactly one variant is set:
// compare (path+op+value), exists (path+exists), or a composite (all_of/any_of/not).
//
// Generated from the schema: an ALIAS for packspec.Predicate. The schema
// expresses this as a oneOf union; Go has no sum type, so the generator
// flattens it exactly as this hand-written type already did.
type Predicate = packspec.Predicate

// compareOps is the set of valid comparison operators.
var compareOps = map[string]bool{
	"equals": true, "not_equals": true,
	"in": true, "not_in": true,
	"less_than": true, "less_than_or_equals": true,
	"greater_than": true, "greater_than_or_equals": true,
}

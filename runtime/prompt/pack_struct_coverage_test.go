package prompt_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

// This file answers a question the per-type parity tests could not: which pack
// structs is nobody checking?
//
// The parity tests are opt-in. A struct with no case is not reported as
// unchecked — it is simply absent, and absence looks like success. That is how
// v1.6.0's metadata.governance was lost: prompt.Metadata was hand-written, had
// no parity case, and dropped the property silently. Adding a case for Metadata
// fixes that one field. It does not stop the next one.
//
// So the rule here is coverage, not correctness: walk the type graph reachable
// from prompt.Pack and require every hand-written struct in it to be accounted
// for — either pinned to a place in the schema, or recorded as deliberately not
// a spec type. A new struct, or a new struct-shaped field on an existing one,
// fails until someone says which it is.
//
// Generated types (runtime/packspec) are exempt: `make packspec-check` already
// proves they match the schema they came from, and more strictly than reflection
// can.
//
// The goal is that this list empties. A hand-written type beside a generated
// one is two definitions of the same thing that WILL drift, which is the
// problem this whole file exists to catch — so notGenerated records why a type
// has not been adopted YET, not a standing justification for keeping it.
//
// Four reasons were recorded here before anyone checked them, and three were
// wrong: TemplateEngineInfo "had no generated type" (packspec.PackTemplateEngine
// existed, field-for-field identical), SkillSource's union "could not be
// expressed" (the generator flattens it), and typed strings "gave safety"
// (a Go named string type accepts any literal). Those three are adopted or
// re-stated now. Check the reason before trusting it.

const generatedPkg = "github.com/AltairaLabs/PromptKit/runtime/packspec"

// pinnedStruct records what a hand-written pack struct corresponds to in the
// PromptPack schema.
//
// Exactly one of schemaRef and notSpec is set. schemaRef is a slash path from
// the schema root ("$defs/Tool", "properties/metadata"); notSpec means the type
// carries no spec-defined data and must say why.
type pinnedStruct struct {
	value     any
	schemaRef string
	notSpec   string
	omissions []deliberateOmission

	// notGenerated says why this type is hand-written when the generator has
	// already emitted an equivalent for its $def. Required on every pin with a
	// schemaRef, and the reason this field exists at all:
	//
	// `make packspec` emits a type for all 52 $defs, but the runtime adopts
	// only some of them. That gap was invisible — the pins recorded WHERE a
	// type maps in the schema and said nothing about why it was not the
	// generated one, so "we generate code and do not use it" could sit
	// unexamined behind a passing property check.
	//
	// A property check is weaker than generation. It verifies that every schema
	// property exists, that no extra fields appear when additionalProperties is
	// false, and that required fields do not carry omitempty. It does NOT
	// verify Go types: a value where absence matters, or a string where the
	// spec means an integer, passes it. So a hand-written type needs a reason,
	// not just a passing test.
	notGenerated string
}

// packStructPins accounts for every hand-written struct reachable from
// prompt.Pack. TestEveryPackStructIsAccountedFor fails if the type graph
// contains one that is not here.
var packStructPins = []pinnedStruct{
	// Not spec types. Each carries data the PromptPack format does not define.
	{value: prompt.Pack{}, notSpec: "the pack root itself; its properties are pinned " +
		"through the structs below and through the generated types it embeds"},
	{value: prompt.CompilationInfo{}, notSpec: "promptkit's own build provenance " +
		"(compiler version, timestamp), carried in the compilation envelope the spec " +
		"leaves open rather than describing spec data"},
	{value: evals.EvalWhen{}, notSpec: "gating on runtime conditions (turn index, " +
		"tool outcome) — evaluation scheduling, not pack data"},
	{value: evals.Threshold{}, notSpec: "guardrail enforcement thresholds; an eval " +
		"never states a pass/fail, so a threshold is the enforcing wrapper's, not the " +
		"eval's, and is not part of the portable pack"},

	// Blocked on ONE Go constraint: a type alias cannot carry methods
	// ("cannot define new methods on non-local type"). These types have
	// behavior attached, so aliasing them to packspec does not compile until
	// the behavior moves to free functions or the generator emits it.
	//
	// This is the only structural reason left in this list. It is a reason to
	// FIX something, not to keep a second definition — see the file header.
	{value: prompt.Variable{}, schemaRef: "$defs/Variable",
		notGenerated: "carries toMetadata(); a type alias cannot have methods. Also " +
			"needs validation map[string]any -> *packspec.VariableValidation. Four " +
			"compile errors, all in pack.go",
		omissions: []deliberateOmission{{
			property: "binding",
			reason: "variable binding (auto-populate from project/provider/workspace/secret/" +
				"configmap) is a runtime concern on the authoring prompt.VariableMetadata; " +
				"compileVariables drops it, so it is not part of the portable pack",
		}}},
	{value: prompt.SkillSourceConfig{}, schemaRef: "$defs/SkillSource",
		notGenerated: "carries UnmarshalJSON/UnmarshalYAML for the bare-string shorthand " +
			"and the legacy `dir` alias; a type alias cannot have methods. Note the " +
			"generated packspec.SkillSource DOES flatten the oneOf and keeps the " +
			"shorthand, so the shape is not the obstacle — only the `dir` alias is"},

	// Adoption changes typed values to bare strings. Worth stating plainly:
	// this is a WEAK reason. A Go named string type accepts any string literal,
	// so `var t EvalTrigger = "nonsense"` compiles either way and the safety is
	// mostly documentation. Where the schema actually closes an enum
	// (WorkflowState.orchestration does; trigger and persistence do not), the
	// real fix is for the generator to emit named constants.
	{value: evals.EvalDef{}, schemaRef: "$defs/Eval",
		notGenerated: "would change trigger to string and when to map[string]any, " +
			"losing evals.EvalTrigger and *evals.EvalWhen. Weak reason (see above); " +
			"blocked mainly by the blast radius across runtime/evals"},
	{value: workflow.Spec{}, schemaRef: "$defs/WorkflowConfig",
		notGenerated: "its states map holds *workflow.State, so it follows that type"},
	{value: workflow.State{}, schemaRef: "$defs/WorkflowState",
		notGenerated: "would change persistence and orchestration to string/*string. " +
			"orchestration IS a closed enum in the schema, so the generator could emit " +
			"named constants for it — a generator gap, not a reason to hand-write"},

	// Tracked. Mechanical but wide.
	{value: prompt.PackPrompt{}, schemaRef: "$defs/Prompt",
		notGenerated: "TRACKED: seven fields change shape (validators, evals, variables, " +
			"tested_models and model_overrides become slices/maps of pointers; pipeline " +
			"and media become typed rather than map[string]any), and this is the type " +
			"every prompt hangs off. Everything reachable from it was adopted first so " +
			"that this is the only step left"},
}

// reachableStructs walks the type graph from rt, collecting every named struct
// type it can reach through fields, pointers, slices, arrays and maps.
func reachableStructs(rt reflect.Type) map[reflect.Type]bool {
	found := map[reflect.Type]bool{}
	seen := map[reflect.Type]bool{}

	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for {
			switch rt.Kind() {
			case reflect.Ptr, reflect.Slice, reflect.Array:
				rt = rt.Elem()
			case reflect.Map:
				walk(rt.Key())
				rt = rt.Elem()
			default:
				goto done
			}
		}
	done:
		if rt.Kind() != reflect.Struct || seen[rt] || rt.PkgPath() == "" {
			return
		}
		seen[rt] = true
		found[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			walk(rt.Field(i).Type)
		}
	}
	walk(rt)
	return found
}

// TestEveryPackStructIsAccountedFor is the guard that makes a silently dropped
// spec property hard to ship.
//
// It does not check that a struct is correct — the pins below do that. It checks
// that no hand-written struct in the pack format is unwatched, which is the
// state that let governance disappear.
func TestEveryPackStructIsAccountedFor(t *testing.T) {
	pinned := make(map[reflect.Type]bool, len(packStructPins))
	for _, p := range packStructPins {
		rt := reflect.TypeOf(p.value)
		if pinned[rt] {
			t.Errorf("duplicate pin for %s", rt)
		}
		pinned[rt] = true

		switch {
		case p.schemaRef == "" && p.notSpec == "":
			t.Errorf("%s is pinned with neither a schemaRef nor a notSpec reason", rt)
		case p.schemaRef != "" && p.notSpec != "":
			t.Errorf("%s is pinned with both a schemaRef and a notSpec reason; it is one "+
				"or the other", rt)
		case p.schemaRef != "" && p.notGenerated == "":
			t.Errorf("%s is pinned to %s but does not say why it is hand-written when "+
				"the generator has emitted a type for that $def.\n\n"+
				"`make packspec` emits all 52; the runtime adopts only some. Leaving "+
				"that gap unstated is how it went unexamined behind a passing property "+
				"check. Set notGenerated: either the reason adopting would be wrong, "+
				"or that it is tracked work.", rt, p.schemaRef)
		case p.notSpec != "" && p.notGenerated != "":
			t.Errorf("%s is notSpec, so there is no generated type to explain away; "+
				"drop its notGenerated reason", rt)
		}
	}

	var unpinned []string
	for rt := range reachableStructs(reflect.TypeOf(prompt.Pack{})) {
		if rt.PkgPath() == generatedPkg || pinned[rt] {
			continue
		}
		unpinned = append(unpinned, rt.PkgPath()+"."+rt.Name())
	}
	sort.Strings(unpinned)

	if len(unpinned) > 0 {
		t.Errorf("these hand-written structs are reachable from prompt.Pack but nothing "+
			"checks them against the PromptPack spec:\n  %s\n\n"+
			"Nothing here is failing yet — that is the problem. A struct with no pin "+
			"drops a new spec property silently, which is how metadata.governance was "+
			"lost in v1.6.0.\n\n"+
			"Fix it one of three ways, best first:\n"+
			"  1. Delete the struct and alias the generated type "+
			"(type X = packspec.Y). It then tracks the schema by regeneration.\n"+
			"  2. Add a pin with a schemaRef in packStructPins, so its properties are "+
			"checked against the schema.\n"+
			"  3. Add a pin with a notSpec reason, if it genuinely carries no "+
			"spec-defined data.",
			strings.Join(unpinned, "\n  "))
	}

	// A pin for a type that is no longer reachable is dead weight that will
	// mislead the next reader, so it fails too.
	reachable := reachableStructs(reflect.TypeOf(prompt.Pack{}))
	for _, p := range packStructPins {
		rt := reflect.TypeOf(p.value)
		if !reachable[rt] {
			t.Errorf("stale pin: %s is no longer reachable from prompt.Pack — drop it", rt)
		}
	}
}

// TestPinnedPackStructsMatchTheSpec runs the property-level parity check for
// every pin that names a schema location. There is no opt-out list: a pin with
// a schemaRef is checked, which is what stops a pin becoming a place to park a
// type and forget it.
func TestPinnedPackStructsMatchTheSpec(t *testing.T) {
	for _, p := range packStructPins {
		if p.schemaRef == "" {
			continue
		}
		rt := reflect.TypeOf(p.value)
		t.Run(rt.Name(), func(t *testing.T) {
			assertStructMatchesSchemaAt(t, rt, p.schemaRef, p.omissions...)
		})
	}
}

// TestGeneratedTypesAreReachedDirectly guards the shortcut this whole file
// depends on: generated types are exempt from pinning because packspec-check
// covers them. That only holds while they are genuinely the generated types and
// not copies, so assert the package they come from is the generated one.
func TestGeneratedTypesAreReachedDirectly(t *testing.T) {
	if got := reflect.TypeOf(packspec.PackMetadata{}).PkgPath(); got != generatedPkg {
		t.Fatalf("generated types moved to %q; update generatedPkg", got)
	}
	if reflect.TypeOf(prompt.Metadata{}).PkgPath() != generatedPkg {
		t.Error("prompt.Metadata is no longer the generated type — it was hand-written " +
			"when it dropped metadata.governance in v1.6.0; keep it an alias")
	}
}

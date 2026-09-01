package prompt_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
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

const generatedPkg = "github.com/AltairaLabs/PromptKit/runtime/v2/packspec"

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

	// embedsGenerated names the generated type this struct embeds, when it does.
	//
	// Go has no partial classes and a type alias cannot carry methods, so a pack
	// type WITH behavior embeds the generated type rather than aliasing it. The
	// spec properties are then promoted from the embedded type and cannot drift;
	// only the methods and any non-spec fields live locally. That is strictly
	// better than hand-writing and is the answer wherever the method set is the
	// type's API.
	//
	// An alias is still preferable where the methods are few enough to become
	// free functions, because an alias gives true identity with the generated
	// type. Embedding needs .Pack to extract it.
	embedsGenerated string

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
	{value: prompt.Pack{}, embedsGenerated: "packspec.Pack",
		notGenerated: "carries eighteen methods (Validate, ValidateWorkflow, " +
			"ValidateAgents, GetPrompt, ...) and FilePath, the on-disk path a loader " +
			"sets so schema and fragment resolution work relative to the pack file. " +
			"A type alias can hold neither, so this EMBEDS packspec.Pack instead: " +
			"every spec property is promoted from the generated type and cannot " +
			"drift, while the methods and FilePath live here"},
}

// assertOnlyEmbedSerializes pins the guarantee that makes embedding safe: the
// embedding type contributes NO serializable field of its own.
//
// Without this, embedding is only as good as everyone's restraint. A field added
// beside the embed passes every other check in this file — it is not a missing
// spec property, and the type is pinned — and then leaks into every emitted
// pack. Verified: a `SneakyField string json:"sneaky_field"` on prompt.Pack
// showed up in MarshalPack output with all guards green.
//
// Local fields are still allowed, but they must be `json:"-"` AND `yaml:"-"`,
// which is what FilePath is: state a loader needs, that no pack ever carries.
func assertOnlyEmbedSerializes(t *testing.T, rt reflect.Type, embedded string) {
	t.Helper()

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Anonymous {
			continue // the embed itself; its properties are generated
		}
		for _, tag := range []string{"json", "yaml"} {
			name := strings.Split(f.Tag.Get(tag), ",")[0]
			if name == "-" {
				continue
			}
			t.Errorf("%s embeds %s but adds its own serializable field %q "+
				"(%s tag %q).\n\n"+
				"Embedding is safe only because every property comes from the "+
				"generated type. A field beside the embed leaks into every emitted "+
				"pack while passing all the other checks here — which is the exact "+
				"drift this file exists to stop.\n\n"+
				"Either put it in the spec and regenerate, or mark it "+
				"`json:\"-\" yaml:\"-\"` if it is loader state the pack never carries.",
				rt, embedded, f.Name, tag, name)
		}
	}
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

		if rt.PkgPath() == generatedPkg {
			t.Errorf("stale pin: %s is the generated type now, so packspec-check "+
				"covers it — drop the entry", rt)
			continue
		}

		if p.embedsGenerated != "" {
			assertOnlyEmbedSerializes(t, rt, p.embedsGenerated)
			// Properties come from the embedded generated type, so a schemaRef
			// would compare against fields reflection sees as one anonymous
			// field. packspec-check covers them instead.
			if p.schemaRef != "" {
				t.Errorf("%s embeds %s, so its properties are already generated; "+
					"drop the schemaRef", rt, p.embedsGenerated)
			}
			if p.notGenerated == "" {
				t.Errorf("%s embeds %s but does not say why it is not a plain alias",
					rt, p.embedsGenerated)
			}
			continue
		}

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

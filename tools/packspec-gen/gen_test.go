package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSchema(t *testing.T, doc map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// emptyExclusions is the default for tests. The production set (NewExclusions)
// names $defs from the real schema, so using it against a toy schema trips the
// stale-exclusion check — correct behavior, wrong test.
func emptyExclusions() *Exclusions {
	return &Exclusions{defs: map[string]string{}, props: map[string]string{}, used: map[string]bool{}}
}

// generate runs the full pipeline the way main does, returning source or error.
func generate(t *testing.T, doc map[string]any) (string, error) {
	t.Helper()
	return generateWith(t, doc, emptyExclusions())
}

func generateWith(t *testing.T, doc map[string]any, ex *Exclusions) (string, error) {
	t.Helper()
	s, err := LoadSchema(writeSchema(t, doc))
	if err != nil {
		return "", err
	}
	cov := NewCoverage()
	s.WalkKeywords(cov)
	src, err := NewEmitter(s, cov, ex, "packspec").Generate()
	if err != nil {
		return "", err
	}
	return src, cov.Reconcile(ex)
}

// minimalRoot is an object root with no properties, so tests can add just what
// they are exercising.
func minimalRoot(defs map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "$defs": defs}
}

func TestGoNameMatchesRepoSpelling(t *testing.T) {
	cases := map[string]string{
		"system_template": "SystemTemplate",
		"id":              "ID",
		"avg_latency_ms":  "AvgLatencyMs",
		"max_cost_usd":    "MaxCostUSD",
		"$schema":         "Schema",
		"p95_latency_ms":  "P95LatencyMs",
		"a2a":             "A2A",
		"input_modes":     "InputModes",
	}
	for in, want := range cases {
		if got := goName(in); got != want {
			t.Errorf("goName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestUnrecognisedKeywordIsAnError is the property the whole tool exists for: a
// construct the generator does not understand must stop the build, never be
// skipped. Without it a spec addition lands as a silently missing field.
func TestUnrecognisedKeywordIsAnError(t *testing.T) {
	_, err := generate(t, minimalRoot(map[string]any{
		"Thing": map[string]any{
			"type":          "object",
			"propertyNames": map[string]any{"pattern": "^x$"},
			"properties":    map[string]any{"a": map[string]any{"type": "string"}},
		},
	}))
	if err == nil {
		t.Fatal("expected an error for an unrecognized keyword, got none")
	}
	if !strings.Contains(err.Error(), "propertyNames") {
		t.Errorf("error must name the offending keyword; got: %v", err)
	}
}

// TestValidationKeywordsAreNotErrors — constraints the schema validator enforces
// at load time cannot affect the Go type, so they must not block generation.
// Without this the tool would be unusable: the spec uses minimum/pattern/enum
// heavily.
func TestValidationKeywordsAreNotErrors(t *testing.T) {
	_, err := generate(t, minimalRoot(map[string]any{
		"Thing": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"n": map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
				"s": map[string]any{"type": "string", "pattern": "^a$", "enum": []any{"a"}},
			},
		},
	}))
	if err != nil {
		t.Fatalf("validation-only keywords must not block generation: %v", err)
	}
}

func TestUnhandledTypeIsAnError(t *testing.T) {
	_, err := generate(t, minimalRoot(map[string]any{
		"Thing": map[string]any{
			"type":       "object",
			"properties": map[string]any{"x": map[string]any{"type": "tuple"}},
		},
	}))
	if err == nil || !strings.Contains(err.Error(), "tuple") {
		t.Fatalf("expected an unhandled-type error naming \"tuple\"; got %v", err)
	}
}

// TestUnionIsGeneratedFlattened replaces TestUnionDefMustBeExcluded, which
// asserted that a union $def stops the build. It no longer does, and that was
// the wrong bar: Go has no sum type, but flattening a union into one struct with
// every variant's fields is a representation CHOICE, not an impossibility — and
// it is the choice the hand-written types already made. Excluding unions left 16
// of 49 defs unchecked; generating them keeps them under the coverage gate.
//
// The discriminator must survive, since it is what tells the variants apart.
func TestUnionIsGeneratedFlattened(t *testing.T) {
	src, err := generate(t, minimalRoot(map[string]any{
		"Leg": map[string]any{"type": "object", "properties": map[string]any{
			"kind":  map[string]any{"const": "leg"},
			"only":  map[string]any{"type": "string"},
			"count": map[string]any{"type": "integer"},
		}},
		"Arm": map[string]any{"type": "object", "properties": map[string]any{
			"kind":  map[string]any{"const": "arm"},
			"other": map[string]any{"type": "boolean"},
		}},
		"Limb": map[string]any{"oneOf": []any{
			map[string]any{"$ref": "#/$defs/Leg"},
			map[string]any{"$ref": "#/$defs/Arm"},
		}},
	}))
	if err != nil {
		t.Fatalf("a union with properties must generate, not fail: %v", err)
	}
	if !strings.Contains(src, "type Limb struct") {
		t.Fatalf("union must produce a flattened struct:\n%s", src)
	}
	limb := src[strings.Index(src, "type Limb struct"):]
	limb = limb[:strings.Index(limb, "\n}")]
	for _, want := range []string{"Only string", "Count int", "Other bool", "Kind"} {
		if !strings.Contains(limb, want) {
			t.Errorf("flattened union missing %q — every variant's fields, and the "+
				"discriminator, must survive:\n%s", want, limb)
		}
	}
	// Every field is optional: no variant supplies them all.
	if strings.Contains(limb, "`json:\"only\"`") {
		t.Error("flattened union fields must all be omitempty; no variant carries every field")
	}
}

// TestBareShapeUnionGeneratesAWrapper replaces
// TestPropertylessUnionStillNeedsExclusion, which asserted that a union with no
// named properties must be excluded. That was wrong twice over: StepInput is "a
// string or an object", and both shapes are modelable — a wrapper with a custom
// unmarshaller carries the choice exactly, where `any` forces every caller to
// type-switch and cannot reject a third shape.
func TestBareShapeUnionGeneratesAWrapper(t *testing.T) {
	src, err := generate(t, minimalRoot(map[string]any{
		"Scalar": map[string]any{"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "object"},
		}},
	}))
	if err != nil {
		t.Fatalf("a union of bare shapes must generate, not fail: %v", err)
	}
	for _, want := range []string{
		"type Scalar struct", "String string", "Object map[string]any",
		"func (v Scalar) MarshalJSON()", "func (v *Scalar) UnmarshalJSON(",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in generated wrapper:\n%s", want, src)
		}
	}
	// The rejection branch is the point: a shape outside the union must error.
	if !strings.Contains(src, "expected object or string") {
		t.Errorf("wrapper must reject shapes outside the union:\n%s", src)
	}
}

// TestUnrecognizableUnionStillNeedsExclusion — the exclusion mechanism still has
// to exist for a union that is neither flattenable nor a set of bare shapes.
func TestUnrecognizableUnionStillNeedsExclusion(t *testing.T) {
	_, err := generate(t, minimalRoot(map[string]any{
		"Odd": map[string]any{"oneOf": []any{
			map[string]any{"type": "tuple"},
		}},
	}))
	if err == nil {
		t.Fatal("a union of unrecognized shapes must not generate silently")
	}
	if !strings.Contains(err.Error(), "exclusion") && !strings.Contains(err.Error(), "variantKinds") {
		t.Errorf("error should say how to proceed; got: %v", err)
	}
}

// TestStaleExclusionIsAnError stops a workaround outliving the thing it worked
// around.
func TestStaleExclusionIsAnError(t *testing.T) {
	s, err := LoadSchema(writeSchema(t, minimalRoot(map[string]any{})))
	if err != nil {
		t.Fatal(err)
	}
	cov := NewCoverage()
	// The production set is empty now — every $def generates — so exercise the
	// mechanism with an entry the schema does not define.
	ex := emptyExclusions()
	ex.defs["LongGone"] = "excluded when the spec still had it"
	s.WalkKeywords(cov)
	if _, err := NewEmitter(s, cov, ex, "p").Generate(); err != nil {
		t.Fatal(err)
	}
	err = cov.Reconcile(ex)
	if err == nil || !strings.Contains(err.Error(), "stale exclusions") {
		t.Fatalf("expected a stale-exclusion error; got %v", err)
	}
}

// TestProductionExclusionsAreEmpty pins the end state. Every claim that a
// construct could not be represented in Go turned out to be false on
// inspection — the unions, ProviderRequirement, and StepInput in turn. If an
// entry reappears here, it needs to clear a high bar, and this test failing is
// the prompt to check that it does.
func TestProductionExclusionsAreEmpty(t *testing.T) {
	if n := NewExclusions().Count(); n != 0 {
		t.Errorf("production exclusions should be empty; found %d. Adding one means "+
			"asserting no Go shape carries the information — verify that rather than "+
			"assuming it, then update this test with the reason.", n)
	}
}

// TestRootPropertiesAreCovered pins the blind spot found while testing the
// RFC 0013 shape: the generator originally walked only $defs, so the root's
// properties — metadata among them — were neither emitted nor reported missing.
func TestRootPropertiesAreCovered(t *testing.T) {
	src, err := generate(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
			"metadata": map[string]any{
				"type":       "object",
				"properties": map[string]any{"governance": map[string]any{"type": "string"}},
			},
		},
		"required": []any{"id"},
		"$defs":    map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type Pack struct", "type PackMetadata struct", "Governance string"} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source missing %q:\n%s", want, src)
		}
	}
}

// TestRequiredFieldsHaveNoOmitEmpty — a required field that vanishes on
// serialize produces a pack that fails its own validation. That is precisely the
// tested_models bug this generator is meant to make unrepeatable.
func TestRequiredFieldsHaveNoOmitEmpty(t *testing.T) {
	src, err := generate(t, minimalRoot(map[string]any{
		"Thing": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"req": map[string]any{"type": "string"},
				"opt": map[string]any{"type": "string"},
			},
			"required": []any{"req"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "`json:\"req\" yaml:\"req\"`") {
		t.Errorf("required field must not carry omitempty:\n%s", src)
	}
	if !strings.Contains(src, "`json:\"opt,omitempty\" yaml:\"opt,omitempty\"`") {
		t.Errorf("optional field must carry omitempty:\n%s", src)
	}
}

// TestOptionalScalarsAreValuesNotPointers pins the decision that makes the
// output alias-compatible with the hand-written types. An off-the-shelf
// generator makes 42% of fields pointers, which would change every read site.
func TestOptionalScalarsAreValuesNotPointers(t *testing.T) {
	src, err := generate(t, minimalRoot(map[string]any{
		"Thing": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"count":    map[string]any{"type": "integer"},
				"rate":     map[string]any{"type": "number"},
				"nullable": map[string]any{"type": []any{"integer", "null"}},
			},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "Count int ") || !strings.Contains(src, "Rate float64 ") {
		t.Errorf("optional scalars must be value types:\n%s", src)
	}
	// The one exception: type: [x, "null"] is how the spec says "nullable".
	if !strings.Contains(src, "Nullable *int ") {
		t.Errorf("a nullable integer must be a pointer:\n%s", src)
	}
}

// TestOutputIsDeterministic — the CI gate is `git diff --exit-code` on the
// generated file, which is worthless if map iteration order leaks into output.
func TestOutputIsDeterministic(t *testing.T) {
	doc := minimalRoot(map[string]any{
		"A": map[string]any{"type": "object", "properties": map[string]any{
			"z": map[string]any{"type": "string"}, "y": map[string]any{"type": "string"},
			"x": map[string]any{"type": "string"}, "w": map[string]any{"type": "string"},
		}},
		"B": map[string]any{"type": "object", "properties": map[string]any{
			"b": map[string]any{"type": "string"}, "a": map[string]any{"type": "string"},
		}},
	})
	first, err := generate(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		next, err := generate(t, doc)
		if err != nil {
			t.Fatal(err)
		}
		if next != first {
			t.Fatalf("generation is not deterministic (run %d differs)", i+2)
		}
	}
}

// TestExcludedRefFallsBackToAnyWithReason — a field pointing at an excluded
// union still has to exist, but the generated source must say why it is untyped
// rather than leaving a bare `any`.
func TestExcludedRefFallsBackToAnyWithReason(t *testing.T) {
	ex := emptyExclusions()
	ex.defs["Predicate"] = "oneOf union — hand-written"
	src, err := generateWith(t, minimalRoot(map[string]any{
		"Predicate": map[string]any{"oneOf": []any{map[string]any{"type": "string"}}},
		"Holder": map[string]any{"type": "object", "properties": map[string]any{
			"p": map[string]any{"$ref": "#/$defs/Predicate"},
		}},
	}), ex)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "any /* Predicate:") {
		t.Errorf("excluded $ref must degrade to any WITH the reason inline:\n%s", src)
	}
}

// TestCompositeTypes covers arrays, $refs and open maps — the shapes the real
// spec leans on most.
func TestCompositeTypes(t *testing.T) {
	src, err := generate(t, minimalRoot(map[string]any{
		"Leaf": map[string]any{"type": "object", "properties": map[string]any{
			"v": map[string]any{"type": "string"},
		}},
		"Holder": map[string]any{"type": "object", "properties": map[string]any{
			"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"leaves":   map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/Leaf"}},
			"leaf":     map[string]any{"$ref": "#/$defs/Leaf"},
			"freeform": map[string]any{"type": "object"},
			"typedMap": map[string]any{"type": "object",
				"additionalProperties": map[string]any{"$ref": "#/$defs/Leaf"}},
			"looseArr": map[string]any{"type": "array"},
			"untyped":  map[string]any{},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Tags []string ", "Leaves []Leaf ", "Leaf *Leaf ",
		"Freeform map[string]any ", "TypedMap map[string]Leaf ",
		"LooseArr []any ", "Untyped any ",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
}

func TestBadRefIsAnError(t *testing.T) {
	_, err := generate(t, minimalRoot(map[string]any{
		"Holder": map[string]any{"type": "object", "properties": map[string]any{
			"x": map[string]any{"$ref": "https://example.com/other.json#/Thing"},
		}},
	}))
	if err == nil || !strings.Contains(err.Error(), "unsupported $ref") {
		t.Fatalf("expected an unsupported-$ref error; got %v", err)
	}
}

func TestLongDescriptionsWrapUnderLintLimit(t *testing.T) {
	long := strings.Repeat("a very long sentence about this field ", 12)
	src, err := generate(t, minimalRoot(map[string]any{
		"Thing": map[string]any{"type": "object", "description": long,
			"properties": map[string]any{"x": map[string]any{"type": "string", "description": long}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(src, "\n") {
		if len(line) > 120 {
			t.Errorf("line %d exceeds the repo's 120-column lint limit (%d): %q", i+1, len(line), line)
		}
	}
}

func TestLoadSchemaRejectsBadInput(t *testing.T) {
	if _, err := LoadSchema(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("expected an error for a missing file")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSchema(bad); err == nil {
		t.Error("expected an error for malformed JSON")
	}
}

func TestSummaryReportsHonestDenominator(t *testing.T) {
	cov := NewCoverage()
	cov.DeclareDef("A")
	cov.EmitDef("A")
	cov.DeclareProp("A.x")
	cov.EmitProp("A.x")

	s := cov.Summary(emptyExclusions())
	if !strings.Contains(s, "1/1 $defs") || !strings.Contains(s, "1/1 properties") {
		t.Errorf("summary must report the honest denominator: %q", s)
	}

	// With an exclusion present the count must be surfaced, not hidden — the
	// original summary reported "214/214 properties" while a sixth of the spec
	// surface was excluded from the denominator entirely.
	ex := emptyExclusions()
	ex.defs["Skipped"] = "for the test"
	if s := cov.Summary(ex); !strings.Contains(s, "excluded") {
		t.Errorf("summary must surface exclusions: %q", s)
	}
}

// TestRunAgainstRealSchemaAndCheckMode exercises the command end to end against
// the schema actually shipped in this repo — the one case that proves the tool
// works on real input rather than fixtures — and then proves -check rejects a
// stale committed file.
func TestRunAgainstRealSchemaAndCheckMode(t *testing.T) {
	const realSchema = "../../runtime/prompt/schema/promptpack.schema.json"
	if _, err := os.Stat(realSchema); err != nil {
		t.Skipf("real schema not present: %v", err)
	}
	out := filepath.Join(t.TempDir(), "types.go")

	if err := runWith(realSchema, out, "packspec", false); err != nil {
		t.Fatalf("generating from the shipped schema must succeed: %v", err)
	}
	if err := runWith(realSchema, out, "packspec", true); err != nil {
		t.Fatalf("-check must pass immediately after a write: %v", err)
	}

	if err := os.WriteFile(out, []byte("package packspec\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runWith(realSchema, out, "packspec", true)
	if err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("-check must reject a stale file; got %v", err)
	}
}

// TestRunParsesFlags covers the CLI surface: argument parsing through to a
// written file, and a parse failure surfacing as an error rather than a panic.
func TestRunParsesFlags(t *testing.T) {
	const realSchema = "../../runtime/prompt/schema/promptpack.schema.json"
	if _, err := os.Stat(realSchema); err != nil {
		t.Skipf("real schema not present: %v", err)
	}
	out := filepath.Join(t.TempDir(), "types.go")

	if err := run([]string{"-schema", realSchema, "-out", out, "-package", "mypkg"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	src, err := os.ReadFile(out) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(src), "// Code generated") {
		t.Error("generated file must carry the DO NOT EDIT header")
	}
	if !strings.Contains(string(src), "package mypkg") {
		t.Error("-package must be honored")
	}

	if err := run([]string{"-nonexistent-flag"}); err == nil {
		t.Error("an unknown flag must return an error")
	}
	if err := run([]string{"-schema", filepath.Join(t.TempDir(), "missing.json")}); err == nil {
		t.Error("a missing schema must return an error")
	}
}

// TestRunWithIOFailures covers the write and read error paths, so a broken
// output path reports a cause rather than appearing to succeed.
func TestRunWithIOFailures(t *testing.T) {
	const realSchema = "../../runtime/prompt/schema/promptpack.schema.json"
	if _, err := os.Stat(realSchema); err != nil {
		t.Skipf("real schema not present: %v", err)
	}

	// Writing into a path whose parent is a regular file, not a directory.
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runWith(realSchema, filepath.Join(blocker, "types.go"), "packspec", false)
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("expected a write error; got %v", err)
	}

	// -check against a file that does not exist must say how to fix it.
	err = runWith(realSchema, filepath.Join(t.TempDir(), "absent.go"), "packspec", true)
	if err == nil || !strings.Contains(err.Error(), "make packspec") {
		t.Fatalf("expected a read error pointing at 'make packspec'; got %v", err)
	}
}

// TestNonZeroDefaultBecomesAPointer pins the three-state rule.
//
// A property whose spec default is not the Go zero value has three meanings:
// absent (use the default), explicitly the zero value, and explicitly something
// else. A plain field with omitempty collapses the first two and silently
// reverses the default — `enabled: false` on a validator serializes to nothing
// and reloads as enabled, turning a disabled guardrail back on. The schema has
// 14 such properties, including Validator.enabled and Eval.enabled.
func TestNonZeroDefaultBecomesAPointer(t *testing.T) {
	src, err := generate(t, minimalRoot(map[string]any{
		"Thing": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"enabled":    map[string]any{"type": "boolean", "default": true},
				"max_rounds": map[string]any{"type": "integer", "default": 5},
				"choice":     map[string]any{"type": "string", "default": "auto"},
				"modes":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "default": []any{"a"}},
				"off":        map[string]any{"type": "boolean", "default": false},
				"plain":      map[string]any{"type": "boolean"},
				"req":        map[string]any{"type": "boolean", "default": true},
			},
			"required": []any{"req"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Enabled *bool", "MaxRounds *int", "Choice *string",
		// Already nilable — a pointer would add nothing.
		"Modes []string",
		// A default equal to the zero value loses nothing to omitempty.
		"Off bool",
		// No default at all: absent and zero mean the same thing.
		"Plain bool",
		// Required properties are always serialized, so absent cannot arise.
		"Req bool",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("expected %q in:\n%s", want, src)
		}
	}
}

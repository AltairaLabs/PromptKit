package prompt_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

func packWithRequires(t *testing.T, requiresJSON string) *prompt.Pack {
	t.Helper()
	var p prompt.Pack
	src := `{"id":"p","name":"P","version":"1.0.0","description":"d",
		"template_engine":{"version":"v1","syntax":"{{variable}}"},
		"prompts":{},"requires":` + requiresJSON + `}`
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("load: %v", err)
	}
	return &p
}

// TestRequiresSurvivesLoading is the regression this whole feature exists for.
// The schema accepted `requires`, no Go field held it, and nothing read it — so
// a pack declaring its dependencies was in exactly the same position as one
// that did not.
func TestRequiresSurvivesLoading(t *testing.T) {
	p := packWithRequires(t, `{"providers":["default"]}`)
	if p.Requires == nil || len(p.Requires.Providers) != 1 {
		t.Fatalf("requires was dropped on load: %+v", p.Requires)
	}
}

// TestShorthandExpansion — RFC 0012's own first example is `- default`, and the
// expansion rule (string means {key, role: "llm", required: true}) is prose the
// JSON Schema cannot express, so it has to live here.
func TestShorthandExpansion(t *testing.T) {
	p := packWithRequires(t, `{"providers":["default"]}`)
	got, err := prompt.ResolveRequirements(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 requirement, got %d", len(got))
	}
	want := prompt.ResolvedRequirement{Key: "default", Role: "llm", Required: true}
	if got[0] != want {
		t.Errorf("shorthand expanded to %+v, want %+v", got[0], want)
	}
}

// TestRequiredDefaultsToTrue — omitted means required, and an explicit false
// must survive. A plain bool field would have collapsed these two, which is why
// the generated type uses a pointer.
func TestRequiredDefaultsToTrue(t *testing.T) {
	p := packWithRequires(t, `{"providers":[
		{"key":"a","role":"llm"},
		{"key":"b","role":"llm","required":false},
		{"key":"c","role":"llm","required":true}
	]}`)
	got, err := prompt.ResolveRequirements(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"a": true, "b": false, "c": true}
	for _, r := range got {
		if r.Required != want[r.Key] {
			t.Errorf("%s: Required=%v, want %v", r.Key, r.Required, want[r.Key])
		}
	}
}

// TestDuplicateKeysRejected — "key values MUST be unique (key is the sole
// discriminator)". The schema cannot express uniqueness across array items, so
// without this two requirements silently collapse into one.
func TestDuplicateKeysRejected(t *testing.T) {
	p := packWithRequires(t, `{"providers":[
		{"key":"fast","role":"llm"},
		{"key":"fast","role":"embedding"}
	]}`)
	_, err := prompt.ResolveRequirements(p)
	if err == nil {
		t.Fatal("duplicate keys must be rejected")
	}
	if !strings.Contains(err.Error(), "fast") {
		t.Errorf("error should name the offending key: %v", err)
	}
}

func TestObjectFormNeedsKeyAndRole(t *testing.T) {
	for _, bad := range []string{`{"providers":[{"key":"a"}]}`, `{"providers":[{"role":"llm"}]}`} {
		p := packWithRequires(t, bad)
		if _, err := prompt.ResolveRequirements(p); err == nil {
			t.Errorf("%s: key and role are both required in the object form", bad)
		}
	}
}

// TestUnsatisfiedSplitsRequiredFromOptional is the rule the spec actually
// places on a runtime: required unsatisfied is an error, optional is a warning.
func TestUnsatisfiedSplitsRequiredFromOptional(t *testing.T) {
	reqs := []prompt.ResolvedRequirement{
		{Key: "default", Role: "llm", Required: true},
		{Key: "embeddings", Role: "embedding", Required: true},
		{Key: "judge", Role: "llm", Required: false},
	}
	have := prompt.ProviderInventory{"llm": {"default"}}

	required, optional := prompt.Unsatisfied(reqs, have)
	if len(required) != 1 || required[0].Key != "embeddings" {
		t.Errorf("required unsatisfied = %+v, want just embeddings", required)
	}
	if len(optional) != 1 || optional[0].Key != "judge" {
		t.Errorf("optional unsatisfied = %+v, want just judge", optional)
	}
}

// TestUnsatisfiedMatchesOnRoleAsWellAsKey — a key alone is not enough. An
// `llm` named "judge" does not satisfy a requirement for an `embedding` named
// "judge", and matching on key alone would report success while the pack fails
// at the first retrieval.
func TestUnsatisfiedMatchesOnRoleAsWellAsKey(t *testing.T) {
	reqs := []prompt.ResolvedRequirement{{Key: "judge", Role: "embedding", Required: true}}
	have := prompt.ProviderInventory{"llm": {"judge"}}

	required, _ := prompt.Unsatisfied(reqs, have)
	if len(required) != 1 {
		t.Error("a provider of the wrong role must not satisfy a requirement")
	}
}

func TestNoRequirementsIsNotAnError(t *testing.T) {
	var p prompt.Pack
	got, err := prompt.ResolveRequirements(&p)
	if err != nil || got != nil {
		t.Errorf("a pack without requires must resolve to nothing: %v, %v", got, err)
	}
	if got, err := prompt.ResolveRequirements(nil); err != nil || got != nil {
		t.Errorf("a nil pack must resolve to nothing: %v, %v", got, err)
	}
}

// TestDescribeNamesTheDescription — the spec keeps `description` for humans
// wiring providers up, so an error that omits it wastes the field.
func TestDescribeNamesTheDescription(t *testing.T) {
	got := prompt.DescribeUnsatisfied([]prompt.ResolvedRequirement{
		{Key: "embeddings", Role: "embedding", Description: "RAG over the handbook"},
	})
	for _, want := range []string{"embeddings", "embedding", "RAG over the handbook"} {
		if !strings.Contains(got, want) {
			t.Errorf("description %q missing %q", got, want)
		}
	}
	if prompt.DescribeUnsatisfied(nil) != "" {
		t.Error("nothing unsatisfied should describe as empty")
	}
}

// TestRequiresRoundTrips — a pack that declares requirements must still declare
// them after a load/save cycle, including the shorthand form.
func TestRequiresRoundTrips(t *testing.T) {
	p := packWithRequires(t, `{"providers":["default",{"key":"judge","role":"llm","required":false}]}`)
	data, err := prompt.NewPackCompiler(nil).MarshalPack(p)
	if err != nil {
		t.Fatal(err)
	}
	var back prompt.Pack
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	got, err := prompt.ResolveRequirements(&back)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "default" || got[1].Key != "judge" || got[1].Required {
		t.Errorf("requirements changed across a round trip: %+v\n%s", got, data)
	}
}

func TestNullRequirementEntryIsRejected(t *testing.T) {
	p := &prompt.Pack{Pack: packspec.Pack{Requires: &prompt.Requires{
		Providers: []*packspec.ProviderRequirement{nil},
	}}}
	if _, err := prompt.ResolveRequirements(p); err == nil {
		t.Error("a null entry must be rejected rather than panicking")
	}
}

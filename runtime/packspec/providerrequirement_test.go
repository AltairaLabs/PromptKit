package packspec_test

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

// TestProviderRequirementAcceptsBothForms covers the mixed union: RFC 0012 lets
// a requirement be either a bare string shorthand or the full object.
//
// The flattened struct alone handled only the object. `- default` — the RFC's
// own first example — failed with "cannot unmarshal string into Go value", and
// nothing reported it, because generating a type is not the same as modeling
// the union. A pack using the documented shorthand would have been rejected.
//
// The expansion rule (a string means {key: X, role: "llm", required: true}) is
// spec semantics, so the scalar is preserved verbatim in Shorthand and the
// consumer applies it. This test pins that it survives at all.
func TestProviderRequirementAcceptsBothForms(t *testing.T) {
	t.Run("string shorthand", func(t *testing.T) {
		var r packspec.ProviderRequirement
		if err := json.Unmarshal([]byte(`"default"`), &r); err != nil {
			t.Fatalf("RFC 0012 shorthand must load: %v", err)
		}
		if r.Shorthand != "default" {
			t.Errorf("Shorthand = %q, want %q", r.Shorthand, "default")
		}
		// The generator must not invent the expansion — that is the consumer's
		// job, and guessing it here would put spec semantics in the wrong place.
		if r.Key != "" || r.Role != "" {
			t.Errorf("shorthand must not be expanded by the type: key=%q role=%q", r.Key, r.Role)
		}
		out, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != `"default"` {
			t.Errorf("shorthand must round-trip as a string, got %s", out)
		}
	})

	t.Run("object form", func(t *testing.T) {
		var r packspec.ProviderRequirement
		src := `{"key":"embeddings","role":"embedding","required":true,"description":"RAG"}`
		if err := json.Unmarshal([]byte(src), &r); err != nil {
			t.Fatal(err)
		}
		if r.Key != "embeddings" || r.Role != "embedding" {
			t.Errorf("object form lost fields: %+v", r)
		}
		// Required is a pointer because the spec defaults it to true: nil means
		// "absent, use the default", which is a different fact from an explicit
		// false. A plain bool would make `required: false` unrepresentable after
		// a round trip.
		if r.Required == nil || !*r.Required {
			t.Errorf("explicit required:true must survive as a non-nil true, got %v", r.Required)
		}
		if r.Shorthand != "" {
			t.Errorf("Shorthand must stay empty for the object form, got %q", r.Shorthand)
		}
		out, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var back map[string]any
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("object form must round-trip as an object, got %s", out)
		}
		if back["key"] != "embeddings" {
			t.Errorf("round trip lost key: %s", out)
		}
	})

	t.Run("a whole requires block", func(t *testing.T) {
		var p packspec.Pack
		src := `{"requires":{"providers":["default",{"key":"judge","role":"llm","required":false}]}}`
		if err := json.Unmarshal([]byte(src), &p); err != nil {
			t.Fatalf("a mixed providers array must load: %v", err)
		}
		if p.Requires == nil || len(p.Requires.Providers) != 2 {
			t.Fatalf("expected two requirements, got %+v", p.Requires)
		}
		if p.Requires.Providers[0].Shorthand != "default" {
			t.Errorf("first entry should be the shorthand, got %+v", p.Requires.Providers[0])
		}
		if p.Requires.Providers[1].Key != "judge" {
			t.Errorf("second entry should be the object, got %+v", p.Requires.Providers[1])
		}
		// The tri-state that a plain bool would lose: an explicit false.
		if req := p.Requires.Providers[1].Required; req == nil || *req {
			t.Errorf("explicit required:false must survive, got %v", req)
		}
		if p.Requires.Providers[0].Required != nil {
			t.Error("the shorthand declares nothing about `required`; it must stay nil " +
				"so the consumer applies the spec default rather than reading a fabricated false")
		}
	})
}

// TestSkillSourceAcceptsBothForms — SkillSource is the spec's other mixed
// union: a skill is a path/package string, a SkillPathSource object, or an
// InlineSkill object. The string form is the common one in real packs
// (`skills: ["./skills/refunds"]`) and, like ProviderRequirement's, it did not
// load before mixed unions were handled.
func TestSkillSourceAcceptsBothForms(t *testing.T) {
	var s packspec.SkillSource
	if err := json.Unmarshal([]byte(`"./skills/refunds"`), &s); err != nil {
		t.Fatalf("string skill source must load: %v", err)
	}
	if s.Shorthand != "./skills/refunds" {
		t.Errorf("Shorthand = %q", s.Shorthand)
	}
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"./skills/refunds"` {
		t.Errorf("string form must round-trip as a string, got %s", out)
	}

	var obj packspec.SkillSource
	if err := json.Unmarshal([]byte(`{"name":"refunds","path":"./skills/refunds"}`), &obj); err != nil {
		t.Fatalf("object skill source must load: %v", err)
	}
	if obj.Name != "refunds" {
		t.Errorf("object form lost name: %+v", obj)
	}
	if obj.Shorthand != "" {
		t.Errorf("Shorthand must stay empty for the object form, got %q", obj.Shorthand)
	}
	if _, err := json.Marshal(obj); err != nil {
		t.Fatal(err)
	}
}

// TestMixedUnionsRejectMalformedInput — a shape that is neither the scalar nor
// the object must error rather than yield a silently empty value. This is the
// branch that distinguishes a modeled union from `any`.
func TestMixedUnionsRejectMalformedInput(t *testing.T) {
	for _, bad := range []string{`[1,2]`, `{"key":`, `12.5`} {
		var r packspec.ProviderRequirement
		if err := json.Unmarshal([]byte(bad), &r); err == nil {
			t.Errorf("ProviderRequirement accepted %s as %+v; want an error", bad, r)
		}
		var s packspec.SkillSource
		if err := json.Unmarshal([]byte(bad), &s); err == nil {
			t.Errorf("SkillSource accepted %s as %+v; want an error", bad, s)
		}
	}
}

// TestStepInputRejectsMalformedJSON covers the wrapper's parse-failure branch,
// distinct from the "valid JSON of the wrong shape" case.
func TestStepInputRejectsMalformedJSON(t *testing.T) {
	var in packspec.StepInput
	if err := json.Unmarshal([]byte(`{"unterminated":`), &in); err == nil {
		t.Error("malformed JSON must error")
	}
}

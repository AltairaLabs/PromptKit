package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestProviderRole_DefaultsToLLM(t *testing.T) {
	p := &Provider{}
	if got := p.GetRole(); got != RoleLLM {
		t.Fatalf("expected default %q, got %q", RoleLLM, got)
	}
}

func TestProviderRole_ExplicitTTS(t *testing.T) {
	p := &Provider{Role: "tts"}
	if got := p.GetRole(); got != RoleTTS {
		t.Fatalf("expected %q, got %q", RoleTTS, got)
	}
}

func TestProviderRole_UnknownRejected(t *testing.T) {
	p := &Provider{Role: "garbage"}
	if err := p.ValidateRole(); err == nil {
		t.Fatal("expected validation error for unknown role")
	}
}

func TestProviderRole_KnownAccepted(t *testing.T) {
	for _, c := range []string{"", "llm", "tts", "stt", "embedding", "image", "video", "inference", "rerank"} {
		p := &Provider{Role: c}
		if err := p.ValidateRole(); err != nil {
			t.Fatalf("role %q rejected unexpectedly: %v", c, err)
		}
	}
}

func TestProviderRole_EmbeddingAndImageGetRole(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"embedding", RoleEmbedding},
		{"image", RoleImage},
	} {
		p := &Provider{Role: c.in}
		if got := p.GetRole(); got != c.want {
			t.Errorf("GetRole for %q = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestProviderRole_EveryKnownRoleIsAccepted keeps ValidateRole and knownRoles
// from drifting apart. A role constant added to one and not the other is the
// inert-declaration shape: the value exists in Go, and a provider file
// declaring it is rejected with "unknown provider role".
func TestProviderRole_EveryKnownRoleIsAccepted(t *testing.T) {
	for role := range knownRoles {
		p := &Provider{Role: role}
		if err := p.ValidateRole(); err != nil {
			t.Errorf("role %q is in knownRoles but ValidateRole rejects it: %v", role, err)
		}
		if got := p.GetRole(); got != role {
			t.Errorf("GetRole for %q = %q", role, got)
		}
	}
}

// TestProviderRole_RerankIsKnown pins the specific role added for #1993.
func TestProviderRole_RerankIsKnown(t *testing.T) {
	p := &Provider{Role: RoleRerank}
	if err := p.ValidateRole(); err != nil {
		t.Fatalf("rerank must validate: %v", err)
	}
	if p.GetRole() != "rerank" {
		t.Errorf("GetRole = %q, want rerank", p.GetRole())
	}
}

// TestProviderRole_SchemaEnumMatchesKnownRoles keeps the two places a role has
// to be declared from drifting apart.
//
// knownRoles governs Go-side validation; the jsonschema enum on Provider.Role
// governs the generated JSON schema, which promptarena's schema-gen produces
// by reflecting this struct. A role added to one and not the other is the
// worst kind of half-wired: `role: rerank` passes ValidateRole in Go and is
// rejected by schema validation in YAML, which reads as a typo rather than a
// missing declaration.
func TestProviderRole_SchemaEnumMatchesKnownRoles(t *testing.T) {
	field, ok := reflect.TypeOf(Provider{}).FieldByName("Role")
	if !ok {
		t.Fatal("Provider has no Role field")
	}

	fromTag := map[string]struct{}{}
	for _, part := range strings.Split(field.Tag.Get("jsonschema"), ",") {
		if v, found := strings.CutPrefix(part, "enum="); found {
			fromTag[v] = struct{}{}
		}
	}

	for role := range knownRoles {
		if _, present := fromTag[role]; !present {
			t.Errorf("role %q is in knownRoles but missing from the jsonschema enum: "+
				"it will validate in Go and be rejected in YAML", role)
		}
	}
	for role := range fromTag {
		if _, present := knownRoles[role]; !present {
			t.Errorf("role %q is in the jsonschema enum but not knownRoles: "+
				"the schema will accept a value ValidateRole rejects", role)
		}
	}
}

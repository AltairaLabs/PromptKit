package config

import "testing"

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
	for _, c := range []string{"", "llm", "tts", "stt", "embedding", "image"} {
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

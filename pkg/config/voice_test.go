package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestArenaSpec_TTSProvidersAndVoices(t *testing.T) {
	src := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: t
spec:
  providers:
    - file: providers/llm.provider.yaml
  tts_providers:
    - file: providers/cartesia-confident-man.provider.yaml
    - file: providers/mock-tts.provider.yaml
  stt_providers: []
  voices:
    - id: confident-man
      provider: cartesia-confident-man
`
	var arena struct {
		Spec Config `yaml:"spec"`
	}
	if err := yaml.Unmarshal([]byte(src), &arena); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(arena.Spec.TTSProviders) != 2 {
		t.Fatalf("tts_providers: got %d entries, want 2", len(arena.Spec.TTSProviders))
	}
	if len(arena.Spec.Voices) != 1 {
		t.Fatalf("voices: got %d entries, want 1", len(arena.Spec.Voices))
	}
	if arena.Spec.Voices[0].ID != "confident-man" {
		t.Fatalf("voices[0].id: got %q", arena.Spec.Voices[0].ID)
	}
	if arena.Spec.Voices[0].Provider != "cartesia-confident-man" {
		t.Fatalf("voices[0].provider: got %q", arena.Spec.Voices[0].Provider)
	}
}

func TestConfig_ResolveVoice(t *testing.T) {
	cfg := &Config{
		Voices: []VoiceBinding{{ID: "confident-man", Provider: "cartesia-confident-man"}},
		LoadedTTSProviders: map[string]*Provider{
			"cartesia-confident-man": {ID: "cartesia-confident-man", Type: "cartesia", Voice: "vid-1", Capability: "tts"},
		},
	}
	p, err := cfg.ResolveVoice("confident-man")
	if err != nil {
		t.Fatalf("ResolveVoice: %v", err)
	}
	if p.Voice != "vid-1" {
		t.Fatalf("provider voice: got %q", p.Voice)
	}
}

func TestConfig_ResolveVoice_UnknownID(t *testing.T) {
	cfg := &Config{}
	if _, err := cfg.ResolveVoice("nope"); err == nil {
		t.Fatal("expected error for unknown voice id")
	}
}

func TestConfig_ResolveVoice_BindingMissingProvider(t *testing.T) {
	cfg := &Config{
		Voices:             []VoiceBinding{{ID: "confident-man", Provider: "ghost"}},
		LoadedTTSProviders: map[string]*Provider{},
	}
	if _, err := cfg.ResolveVoice("confident-man"); err == nil {
		t.Fatal("expected error when binding's provider is not loaded")
	}
}

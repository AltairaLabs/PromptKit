package config

import "testing"

func TestProviderCapability_DefaultsToLLM(t *testing.T) {
	p := &Provider{}
	if got := p.GetCapability(); got != CapabilityLLM {
		t.Fatalf("expected default %q, got %q", CapabilityLLM, got)
	}
}

func TestProviderCapability_ExplicitTTS(t *testing.T) {
	p := &Provider{Capability: "tts"}
	if got := p.GetCapability(); got != CapabilityTTS {
		t.Fatalf("expected %q, got %q", CapabilityTTS, got)
	}
}

func TestProviderCapability_UnknownRejected(t *testing.T) {
	p := &Provider{Capability: "garbage"}
	if err := p.ValidateCapability(); err == nil {
		t.Fatal("expected validation error for unknown capability")
	}
}

func TestProviderCapability_KnownAccepted(t *testing.T) {
	for _, c := range []string{"", "llm", "tts", "stt"} {
		p := &Provider{Capability: c}
		if err := p.ValidateCapability(); err != nil {
			t.Fatalf("capability %q rejected unexpectedly: %v", c, err)
		}
	}
}

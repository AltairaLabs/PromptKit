package config

import "fmt"

// Capability values for the Provider.Capability field. Defaults to LLM
// when unset so existing provider yamls (which predate the field) keep
// working with no migration.
const (
	CapabilityLLM = "llm"
	CapabilityTTS = "tts"
	CapabilitySTT = "stt"
)

// knownCapabilities is the set accepted by ValidateCapability. An empty
// string is also accepted (treated as LLM by GetCapability).
var knownCapabilities = map[string]struct{}{
	CapabilityLLM: {},
	CapabilityTTS: {},
	CapabilitySTT: {},
}

// GetCapability returns the provider's capability, defaulting to "llm".
func (p *Provider) GetCapability() string {
	if p == nil || p.Capability == "" {
		return CapabilityLLM
	}
	return p.Capability
}

// ValidateCapability returns an error if Capability is set to an
// unrecognized value. Empty is treated as LLM and accepted.
func (p *Provider) ValidateCapability() error {
	if p == nil || p.Capability == "" {
		return nil
	}
	if _, ok := knownCapabilities[p.Capability]; !ok {
		return fmt.Errorf("unknown provider capability %q (valid: %s, %s, %s)",
			p.Capability, CapabilityLLM, CapabilityTTS, CapabilitySTT)
	}
	return nil
}

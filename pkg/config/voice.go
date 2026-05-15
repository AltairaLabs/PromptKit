package config

import "fmt"

// VoiceBinding binds a voice id (the namespace personas reference) to a
// loaded TTS provider id. Parallel to SelfPlayRoleGroup's id→provider
// shape.
type VoiceBinding struct {
	ID       string `yaml:"id" json:"id"`             // Voice id used by personas
	Provider string `yaml:"provider" json:"provider"` // TTS provider id (must exist in spec.tts_providers)
}

// ResolveVoice returns the TTS provider config bound to the given voice id.
// Returns an error if the voice id is not in spec.voices, or if the binding
// points to a provider that wasn't loaded.
func (c *Config) ResolveVoice(voiceID string) (*Provider, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}
	for i := range c.Voices {
		if c.Voices[i].ID != voiceID {
			continue
		}
		providerID := c.Voices[i].Provider
		if p, ok := c.LoadedTTSProviders[providerID]; ok && p != nil {
			return p, nil
		}
		return nil, fmt.Errorf("voice %q binds to provider %q which is not loaded in spec.tts_providers",
			voiceID, providerID)
	}
	return nil, fmt.Errorf("voice id %q not found in spec.voices", voiceID)
}

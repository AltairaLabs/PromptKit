package config

// VoiceBinding binds a voice id (the namespace personas reference) to a
// loaded TTS provider id. Parallel to SelfPlayRoleGroup's id→provider
// shape.
type VoiceBinding struct {
	ID       string `yaml:"id" json:"id"`             // Voice id used by personas
	Provider string `yaml:"provider" json:"provider"` // TTS provider id (must exist in spec.tts_providers)
}

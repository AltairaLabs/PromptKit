package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A Live session reports its voice and output rate when AUDIO is a response
// modality, and nothing when its replies are text.
func TestStreamSession_OutputAudio(t *testing.T) {
	cases := []struct {
		name   string
		config StreamSessionConfig
		want   providers.OutputAudio
		wantOK bool
	}{
		{"audio at the default rate", StreamSessionConfig{ResponseModalities: []string{"AUDIO"}},
			providers.OutputAudio{Voice: "Puck", SampleRate: 24000, Family: "gemini-live"}, true},
		{"audio at a configured rate", StreamSessionConfig{ResponseModalities: []string{"AUDIO"}, OutputSampleRate: 16000},
			providers.OutputAudio{Voice: "Puck", SampleRate: 16000, Family: "gemini-live"}, true},
		{"text", StreamSessionConfig{ResponseModalities: []string{"TEXT"}}, providers.OutputAudio{}, false},
		{"default modality", StreamSessionConfig{}, providers.OutputAudio{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &StreamSession{config: tc.config}
			got, ok := s.OutputAudio()
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// The voice OutputAudio reports is the one the setup message asks for.
func TestStreamSession_OutputAudioVoiceIsTheSetupVoice(t *testing.T) {
	cfg := buildGenerationConfig([]string{"AUDIO"})
	speech := cfg["speechConfig"].(map[string]interface{})
	voice := speech["voiceConfig"].(map[string]interface{})["prebuiltVoiceConfig"].(map[string]interface{})
	got, _ := (&StreamSession{config: StreamSessionConfig{ResponseModalities: []string{"AUDIO"}}}).OutputAudio()
	assert.Equal(t, voice["voiceName"], got.Voice)
}

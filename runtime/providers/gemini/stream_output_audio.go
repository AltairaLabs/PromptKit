package gemini

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

const (
	// liveVoice is the prebuilt voice a Live session speaks in.
	liveVoice = "Puck"
	// liveFamily is the provider family OutputAudio reports for a Live
	// session's audio.
	liveFamily = "gemini-live"
)

// OutputAudio implements providers.OutputAudioReporter: the session's voice at
// its output rate. ok is false unless AUDIO is a response modality.
func (s *StreamSession) OutputAudio() (providers.OutputAudio, bool) {
	if !sliceContains(getResponseModalities(s.config.ResponseModalities), modalityAudio) {
		return providers.OutputAudio{}, false
	}
	return providers.OutputAudio{Voice: liveVoice, SampleRate: s.outputSampleRate(), Family: liveFamily}, true
}

var _ providers.OutputAudioReporter = (*StreamSession)(nil)

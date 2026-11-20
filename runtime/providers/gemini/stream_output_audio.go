package gemini

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

// liveFamily is the provider family OutputAudio reports for a Live session's
// audio.
const liveFamily = "gemini-live"

// OutputAudio implements providers.OutputAudioReporter: the session's voice
// (its configured voice, or defaultLiveVoice) at its output rate. ok is false
// unless AUDIO is a response modality.
func (s *StreamSession) OutputAudio() (providers.OutputAudio, bool) {
	if !sliceContains(getResponseModalities(s.config.ResponseModalities), modalityAudio) {
		return providers.OutputAudio{}, false
	}
	voice := s.config.Voice
	if voice == "" {
		voice = defaultLiveVoice
	}
	return providers.OutputAudio{Voice: voice, SampleRate: s.outputSampleRate(), Family: liveFamily}, true
}

var _ providers.OutputAudioReporter = (*StreamSession)(nil)

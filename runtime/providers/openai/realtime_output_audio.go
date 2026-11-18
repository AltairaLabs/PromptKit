package openai

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

// realtimeFamily is the provider family OutputAudio reports for a Realtime
// session's audio.
const realtimeFamily = "openai-realtime"

// OutputAudio implements providers.OutputAudioReporter: the session's voice,
// at the Realtime PCM16 rate. ok is false for a text-only session.
func (s *RealtimeSession) OutputAudio() (providers.OutputAudio, bool) {
	if outputModalities(s.config.Modalities)[0] != modalityAudio {
		return providers.OutputAudio{}, false
	}
	return providers.OutputAudio{Voice: s.config.Voice, SampleRate: DefaultRealtimeSampleRate,
		Family: realtimeFamily}, true
}

var _ providers.OutputAudioReporter = (*RealtimeSession)(nil)

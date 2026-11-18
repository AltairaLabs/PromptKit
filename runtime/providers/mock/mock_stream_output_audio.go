package mock

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

// WithOutputAudio makes the session report spoken output in audio's voice and
// format (providers.OutputAudioReporter). Without it the session reports none.
func (m *MockStreamSession) WithOutputAudio(audio providers.OutputAudio) *MockStreamSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outputAudio = &audio
	return m
}

// OutputAudio implements providers.OutputAudioReporter with what
// WithOutputAudio set; ok is false when it was not called.
func (m *MockStreamSession) OutputAudio() (providers.OutputAudio, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.outputAudio == nil {
		return providers.OutputAudio{}, false
	}
	return *m.outputAudio, true
}

// WithOutputAudio makes the sessions the provider creates report spoken output
// in audio's voice and format.
func (p *StreamingProvider) WithOutputAudio(audio providers.OutputAudio) *StreamingProvider {
	p.outputAudio = &audio
	return p
}

var _ providers.OutputAudioReporter = (*MockStreamSession)(nil)

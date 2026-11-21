package mock

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

var _ providers.PlayoutHolder = (*MockStreamSession)(nil)

// HoldPlayout implements providers.PlayoutHolder, recording that the
// pipeline holds reply audio back (PlayoutHeld).
func (m *MockStreamSession) HoldPlayout() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.playoutHeld = true
}

// AudioReleased implements providers.PlayoutHolder, recording the audio the
// pipeline released to the caller (ReleasedAudio).
func (m *MockStreamSession) AudioReleased(chunk *providers.StreamChunk) {
	if chunk == nil || chunk.MediaData == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releasedAudio = append(m.releasedAudio, chunk.MediaData.Data...)
}

// PlayoutHeld reports whether the pipeline asked the session to count reply
// audio only as it is released (HoldPlayout).
func (m *MockStreamSession) PlayoutHeld() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.playoutHeld
}

// ReleasedAudio is the reply audio the pipeline reported released, in order.
func (m *MockStreamSession) ReleasedAudio() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.releasedAudio...)
}

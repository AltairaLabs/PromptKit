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

// ReleaseAudio implements providers.PlayoutHolder, recording the audio the
// pipeline released to the caller (ReleasedAudio), unless the test had the
// caller speak over the held audio (InterruptHeldAudio).
func (m *MockStreamSession) ReleaseAudio(chunk *providers.StreamChunk) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.heldInterrupted {
		return false
	}
	if chunk != nil && chunk.MediaData != nil {
		m.releasedAudio = append(m.releasedAudio, chunk.MediaData.Data...)
	}
	return true
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

// HeldAudioInterrupted implements providers.PlayoutHolder: whether the test
// had the caller speak over held audio (InterruptHeldAudio) since the
// pipeline last dropped what it held.
func (m *MockStreamSession) HeldAudioInterrupted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.heldInterrupted
}

// HeldAudioDropped implements providers.PlayoutHolder, clearing
// HeldAudioInterrupted and counting the drop (HeldDrops).
func (m *MockStreamSession) HeldAudioDropped() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heldInterrupted = false
	m.heldDrops++
}

// InterruptHeldAudio has the caller speak over the audio the pipeline holds:
// HeldAudioInterrupted reports true until the pipeline drops it. A real
// session also reports the barge-in with an Interrupted chunk; a test emits
// that itself.
func (m *MockStreamSession) InterruptHeldAudio() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heldInterrupted = true
}

// HeldDrops is how many times the pipeline reported dropping held audio.
func (m *MockStreamSession) HeldDrops() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.heldDrops
}

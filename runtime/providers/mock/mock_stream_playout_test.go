package mock

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// The mock session records a pipeline holding reply audio back and the audio
// it releases, in order, so tests can assert what the caller heard.
func TestMockStreamSession_RecordsHeldPlayout(t *testing.T) {
	s := NewMockStreamSession()
	assert.False(t, s.PlayoutHeld())
	s.HoldPlayout()
	s.AudioReleased(&providers.StreamChunk{MediaData: &providers.StreamMediaData{Data: []byte{1, 2}}})
	s.AudioReleased(&providers.StreamChunk{})
	s.AudioReleased(nil)
	s.AudioReleased(&providers.StreamChunk{MediaData: &providers.StreamMediaData{Data: []byte{3}}})

	assert.True(t, s.PlayoutHeld())
	assert.Equal(t, []byte{1, 2, 3}, s.ReleasedAudio())
}

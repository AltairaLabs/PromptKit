package mock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A mock session reports no spoken output until told what to report, and a
// provider configured with it gives its sessions that output.
func TestMockStreamSession_OutputAudio(t *testing.T) {
	want := providers.OutputAudio{Voice: "alloy", SampleRate: 16000, Family: "mock"}

	s := NewMockStreamSession()
	got, ok := s.OutputAudio()
	assert.False(t, ok)
	assert.Equal(t, providers.OutputAudio{}, got)

	got, ok = s.WithOutputAudio(want).OutputAudio()
	assert.True(t, ok)
	assert.Equal(t, want, got)

	p := NewStreamingProvider("p", "m", false).WithOutputAudio(want)
	sess, err := p.CreateStreamSession(context.Background(), &providers.StreamingInputConfig{})
	require.NoError(t, err)
	r, ok := sess.(providers.OutputAudioReporter)
	require.True(t, ok)
	got, ok = r.OutputAudio()
	assert.True(t, ok)
	assert.Equal(t, want, got)
}

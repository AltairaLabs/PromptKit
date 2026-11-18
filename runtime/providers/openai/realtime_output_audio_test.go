package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A Realtime session reports its voice and the Realtime output rate when its
// replies are spoken, and nothing when they are text only.
func TestRealtimeSession_OutputAudio(t *testing.T) {
	cases := []struct {
		name       string
		modalities []string
		voice      string
		want       providers.OutputAudio
		wantOK     bool
	}{
		{"audio", []string{modalityText, modalityAudio}, "verse",
			providers.OutputAudio{Voice: "verse", SampleRate: 24000, Family: "openai-realtime"}, true},
		{"default modalities", nil, "alloy",
			providers.OutputAudio{Voice: "alloy", SampleRate: 24000, Family: "openai-realtime"}, true},
		{"text only", []string{modalityText}, "verse", providers.OutputAudio{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &RealtimeSession{config: RealtimeSessionConfig{Modalities: tc.modalities, Voice: tc.voice}}
			got, ok := s.OutputAudio()
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// The bookkeeping wrapper CreateStreamSession returns forwards OutputAudio:
// hidden, a cue would play in no voice at the wrong rate.
func TestRealtimeSessionBookkeeping_ForwardsOutputAudio(t *testing.T) {
	inner := &RealtimeSession{config: RealtimeSessionConfig{Modalities: []string{modalityAudio}, Voice: "verse"}}
	var wrapped providers.StreamInputSession = &realtimeSessionBookkeeping{StreamInputSession: inner,
		release: func() {}}
	r, ok := wrapped.(providers.OutputAudioReporter)
	require.True(t, ok, "the wrapper hides OutputAudio")
	got, spoken := r.OutputAudio()
	assert.True(t, spoken)
	assert.Equal(t, providers.OutputAudio{Voice: "verse", SampleRate: 24000, Family: "openai-realtime"}, got)

	plain := &realtimeSessionBookkeeping{StreamInputSession: newFakeStreamSession(), release: func() {}}
	got, spoken = plain.OutputAudio()
	assert.False(t, spoken, "a session without the interface reports no spoken output")
	assert.Equal(t, providers.OutputAudio{}, got)
}

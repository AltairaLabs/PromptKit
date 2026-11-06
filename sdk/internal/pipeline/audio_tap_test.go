package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// AudioTap adds an input tap before the provider and an output tap after it.
func TestCollectPipelineStages_AudioTapPlacement(t *testing.T) {
	cfg := &Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
		Provider:       mock.NewProvider("test", "test-model", false),
		AudioTap:       func(stage.RecordingPosition, *stage.AudioData, time.Time) {},
	}
	stages, err := collectPipelineStages(cfg, nil, false)
	require.NoError(t, err)

	pos := map[string]int{}
	for i, s := range stages {
		pos[s.Name()] = i
	}
	in, hasIn := pos["audio-monitor-input"]
	out, hasOut := pos["audio-monitor-output"]
	prov, hasProv := pos["provider"]
	require.True(t, hasIn && hasOut && hasProv, "stages: %v", pos)
	assert.Less(t, in, prov)
	assert.Greater(t, out, prov)

	cfg.AudioTap = nil
	stages, err = collectPipelineStages(cfg, nil, false)
	require.NoError(t, err)
	for _, s := range stages {
		assert.NotContains(t, s.Name(), "audio-monitor")
	}
}

// The tap sees audio elements only and forwards every element unchanged.
func TestAudioTapStage_TapsAudioAndPassesThrough(t *testing.T) {
	var tapped []stage.RecordingPosition
	tap := newAudioTapStage(stage.RecordingPositionOutput,
		func(p stage.RecordingPosition, a *stage.AudioData, _ time.Time) {
			assert.Equal(t, []byte{1, 2}, a.Samples)
			tapped = append(tapped, p)
		})

	text := "hello"
	input := make(chan stage.StreamElement, 2)
	output := make(chan stage.StreamElement, 2)
	input <- stage.StreamElement{Audio: &stage.AudioData{Samples: []byte{1, 2}}}
	input <- stage.StreamElement{Text: &text}
	close(input)
	require.NoError(t, tap.Process(context.Background(), input, output))

	assert.Equal(t, []stage.RecordingPosition{stage.RecordingPositionOutput}, tapped)
	first, second := <-output, <-output
	assert.NotNil(t, first.Audio)
	require.NotNil(t, second.Text)
	assert.Equal(t, "hello", *second.Text)
}

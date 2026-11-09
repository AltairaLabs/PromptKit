package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Typed text in a VAD session (SendText, TriggerStart) is a whole user turn:
// it reaches the model on its own, without waiting for a spoken turn to end
// it, so the model can speak first (#2609). Runs the full VAD topology:
// AudioTurn -> STT -> Provider -> TTS, input left open as a live session's is.
func TestVADPipeline_TypedTextStartsATurn(t *testing.T) {
	provider := newRecordingProvider()
	vadConfig := stage.AudioTurnConfig{VAD: &mockVADAnalyzer{}, TurnDetector: &mockTurnDetector{}}
	cfg := &Config{
		Provider:   provider,
		VADConfig:  &vadConfig,
		STTService: &mockSTTService{},
		TTSService: &mockTTSService{},
	}
	stages, err := buildVADPipelineStages(cfg, stage.NewTurnState())
	require.NoError(t, err)
	pc := stage.DefaultPipelineConfig()
	pc.ExecutionTimeout = 0
	pc.IdleTimeout = 0
	p, err := stage.NewPipelineBuilderWithConfig(pc).Chain(stages...).Build()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan stage.StreamElement, 1)
	out, err := p.Execute(ctx, input)
	require.NoError(t, err)
	go func() {
		for range out { //nolint:revive // drain
		}
	}()

	input <- stage.StreamElement{Text: testutil.Ptr("start the call")}

	require.Eventually(t, func() bool {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		return len(provider.requests) > 0
	}, 2*time.Second, time.Millisecond, "typed text must reach the model without a spoken turn to end it")
	got := provider.onlyRequest(t).Messages
	require.NotEmpty(t, got)
	assert.Equal(t, types.Message{Role: "user", Content: "start the call"}, got[len(got)-1])
	close(input)
}

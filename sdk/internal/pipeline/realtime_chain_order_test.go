package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	memorystore "github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopExtractor satisfies memorystore.Extractor; the order tests only build
// the pipeline.
type noopExtractor struct{}

func (noopExtractor) Extract(
	context.Context, map[string]string, []types.Message,
) ([]*memorystore.Memory, error) {
	return nil, nil
}

// realtimeStageNames lists, in order, the stages Build assembles for cfg,
// state store stages included.
func realtimeStageNames(t *testing.T, cfg *Config) []string {
	t.Helper()
	ssc, rag := buildStateStoreConfig(cfg)
	stages, err := collectPipelineStages(cfg, ssc, rag)
	require.NoError(t, err)
	names := make([]string, 0, len(stages))
	for _, s := range stages {
		names = append(names, s.Name())
	}
	return names
}

// The realtime (ASM) pipeline's element order, for the combinations of
// recording, cue, memory, broadcast, save, transcript reorder, playout and
// pacing a host can configure. The cue stage always follows the provider
// stage directly, ahead of the transcript reorder stage.
func TestCollectPipelineStages_RealtimeStageOrder(t *testing.T) {
	recStore, err := events.NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = recStore.Close() })

	base := func() *Config {
		return &Config{
			PromptRegistry:      createTestRegistry("chat"),
			TaskType:            "chat",
			ConversationID:      "conv",
			StreamInputProvider: mock.NewStreamingProvider("test", "test-model", false),
			StreamInputConfig:   &providers.StreamingInputConfig{},
		}
	}
	full := func(cfg *Config) *Config {
		cfg.StateStore = statestore.NewMemoryStore()
		cfg.RecordingConfig = &stage.RecordingStageConfig{IncludeAudio: true}
		cfg.RecordingStore = recStore
		cfg.AudioTap = func(stage.RecordingPosition, *stage.AudioData, time.Time) {}
		cfg.MemoryExtractor = noopExtractor{}
		cfg.MemoryStore = memorystore.NewInMemoryStore()
		cfg.EventEmitter = events.NewEmitter(events.NewEventBus(), "run", "sess", "conv")
		cfg.ReorderInputTranscript = true
		cfg.PaceOutputAudio = true
		cfg.TurnReporter = &recordingReporter{}
		return cfg
	}
	front := []string{"variable_provider", "prompt_assembly", "template"}

	tests := []struct {
		name string
		cfg  *Config
		want []string
	}{
		{
			name: "minimal: provider and cue",
			cfg:  base(),
			want: append(append([]string{}, front...), "duplex_provider", "cue"),
		},
		{
			name: "everything a host can turn on",
			cfg:  full(base()),
			want: []string{
				"statestore_load", "variable_provider", "prompt_assembly", "template",
				"recording_input", "audio-monitor-input",
				"duplex_provider", "cue", "transcript-reorder", "playout-hold",
				"recording_output", "audio-monitor-output", "memory_extraction", "message_broadcast",
				"incremental_save", "turn-observer", "audio-pacing-output",
			},
		},
		{
			name: "recording, unpaced, untracked: no playout hold, no pacing",
			cfg: func() *Config {
				cfg := base()
				cfg.RecordingConfig = &stage.RecordingStageConfig{}
				cfg.RecordingStore = recStore
				return cfg
			}(),
			want: append(append([]string{}, front[0], front[1], front[2], "recording_input"),
				"duplex_provider", "cue", "recording_output"),
		},
		{
			name: "playout tracked without pacing",
			cfg: func() *Config {
				cfg := base()
				cfg.TrackPlayout = true
				return cfg
			}(),
			want: append(append([]string{}, front...), "duplex_provider", "cue", "playout-hold"),
		},
		{
			name: "memory without an emitter; paced",
			cfg: func() *Config {
				cfg := base()
				cfg.MemoryExtractor = noopExtractor{}
				cfg.MemoryStore = memorystore.NewInMemoryStore()
				cfg.PaceOutputAudio = true
				return cfg
			}(),
			want: append(append([]string{}, front...),
				"duplex_provider", "cue", "playout-hold", "memory_extraction", "audio-pacing-output"),
		},
		{
			name: "a recording store without a config, an extractor without a store: neither stage",
			cfg: func() *Config {
				cfg := base()
				cfg.RecordingStore = recStore
				cfg.MemoryExtractor = noopExtractor{}
				return cfg
			}(),
			want: append(append([]string{}, front...), "duplex_provider", "cue"),
		},
		{
			name: "save and broadcast, reorder, no recording",
			cfg: func() *Config {
				cfg := base()
				cfg.StateStore = statestore.NewMemoryStore()
				cfg.EventEmitter = events.NewEmitter(events.NewEventBus(), "run", "sess", "conv")
				cfg.ReorderInputTranscript = true
				return cfg
			}(),
			want: []string{
				"statestore_load", "variable_provider", "prompt_assembly", "template",
				"duplex_provider", "cue", "transcript-reorder", "message_broadcast", "incremental_save",
			},
		},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, realtimeStageNames(t, tc.cfg), tc.name)
	}
}

// The realtime chain's transcript reorder stage writes the configured
// placeholder as the caller's turn when a reply ends with no transcript.
func TestCollectPipelineStages_RealtimeReorderUsesPlaceholder(t *testing.T) {
	cfg := &Config{
		PromptRegistry:             createTestRegistry("chat"),
		TaskType:                   "chat",
		StreamInputProvider:        mock.NewStreamingProvider("test", "test-model", false),
		StreamInputConfig:          &providers.StreamingInputConfig{},
		ReorderInputTranscript:     true,
		InputTranscriptPlaceholder: "[inaudible]",
	}
	stages, err := collectPipelineStages(cfg, nil, false)
	require.NoError(t, err)
	var reorder stage.Stage
	for _, s := range stages {
		if s.Name() == "transcript-reorder" {
			reorder = s
		}
	}
	require.NotNil(t, reorder)

	in := make(chan stage.StreamElement, 2)
	out := make(chan stage.StreamElement, 4)
	reply := "Reply"
	in <- stage.StreamElement{Text: &reply}
	in <- stage.StreamElement{EndOfStream: true, Message: &types.Message{Role: "assistant", Content: reply}}
	close(in)
	require.NoError(t, reorder.Process(context.Background(), in, out))

	first := <-out
	require.NotNil(t, first.Message)
	assert.Equal(t, "user", first.Message.Role)
	assert.Equal(t, "[inaudible]", first.Message.Content)
}

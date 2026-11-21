package stage

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRealtimeTestProvider() *DuplexProviderStage {
	return NewDuplexProviderStage(providersmock.NewStreamingProvider("t", "m", false), baseConfig())
}

// hostStage is a host-built passthrough stage with a name of its choosing.
func hostStage(name string) Stage {
	return NewPassthroughStage(name)
}

func chainNames(stages []Stage) []string {
	names := make([]string, 0, len(stages))
	for _, s := range stages {
		names = append(names, s.Name())
	}
	return names
}

// fullRealtimeConfig turns every slot on.
func fullRealtimeConfig(t *testing.T) RealtimeChainConfig {
	t.Helper()
	store, err := events.NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return RealtimeChainConfig{
		Cue:                        CueOptions{Cache: cueclip.NewMemoryCache(1 << 20)},
		ReorderInputTranscript:     true,
		InputTranscriptPlaceholder: "[inaudible]",
		PlayoutHold:                true,
		RecordingStore:         store,
		Recording:              RecordingStageConfig{Position: RecordingPositionInput, SessionID: "s"},
		AudioTap:               hostStage("tap"),
		MemoryExtraction:       hostStage("extract"),
		MessageBroadcast:       events.NewEmitter(events.NewEventBus(), "r", "s", "c"),
		Save:                   hostStage("save"),
		Pacing:                 RealtimePacingFlushOnCut,
	}
}

var fullRealtimeNames = []string{
	"duplex_provider", "cue", "transcript-reorder", "playout-hold", "recording_output",
	"tap", "extract", "message_broadcast", "save", "audio-pacing-output",
}

// The zero config is the provider stage and its cue stage, and the cue stage
// is wired to that provider.
func TestNewRealtimeChain_ZeroConfig(t *testing.T) {
	provider := newRealtimeTestProvider()
	stages, err := NewRealtimeChain(provider, RealtimeChainConfig{})
	require.NoError(t, err)
	assert.Equal(t, []string{"duplex_provider", "cue"}, chainNames(stages))
	assert.Same(t, provider, stages[0])
	cue, ok := stages[1].(*cueStage)
	require.True(t, ok)
	assert.Same(t, cue.bank, provider.cues, "the cue stage plays the cues of this provider")
}

// Every slot filled gives the chain order, with the output recording stage
// recording at the output position whatever position the host's config says.
func TestNewRealtimeChain_FullOrder(t *testing.T) {
	cfg := fullRealtimeConfig(t)
	stages, err := NewRealtimeChain(newRealtimeTestProvider(), cfg)
	require.NoError(t, err)
	assert.Equal(t, fullRealtimeNames, chainNames(stages))
	cue, ok := stages[1].(*cueStage)
	require.True(t, ok)
	assert.Same(t, cfg.Cue.Cache, cue.cfg.Cache, "the cue stage keeps its clips in the host's cache")
	reorder, ok := stages[2].(*TranscriptReorderStage)
	require.True(t, ok)
	assert.Equal(t, "[inaudible]", reorder.placeholder)
	rec, ok := stages[4].(*RecordingStage)
	require.True(t, ok)
	assert.Equal(t, RecordingPositionOutput, rec.config.Position)
	assert.Equal(t, "s", rec.config.SessionID)
}

// The pacing mode picks the pacing stage: none, one that drops a cut reply's
// queued audio, or one that plays it out.
func TestNewRealtimeChain_PacingModes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  RealtimePacing
		flush bool
	}{
		{"flush on cut", RealtimePacingFlushOnCut, true},
		{"play through", RealtimePacingPlayThrough, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stages, err := NewRealtimeChain(newRealtimeTestProvider(), RealtimeChainConfig{Pacing: tc.mode})
			require.NoError(t, err)
			require.Equal(t, []string{"duplex_provider", "cue", "audio-pacing-output"}, chainNames(stages))
			pacer, ok := stages[2].(*AudioPacingStage)
			require.True(t, ok)
			assert.Equal(t, tc.flush, pacer.flushOnCut)
		})
	}
	stages, err := NewRealtimeChain(newRealtimeTestProvider(), RealtimeChainConfig{Pacing: RealtimePacingNone})
	require.NoError(t, err)
	assert.Equal(t, []string{"duplex_provider", "cue"}, chainNames(stages))
}

// A host inserts stages after a named slot, in the order given; a slot that
// is empty still places its stages where it would be.
func TestNewRealtimeChain_InsertAfterSlot(t *testing.T) {
	cfg := fullRealtimeConfig(t)
	cfg.After = map[RealtimeSlot][]Stage{
		RealtimeSlotCue:      {hostStage("avatar"), hostStage("captions")},
		RealtimeSlotSave:     {hostStage("turn-observer")},
		RealtimeSlotPacing:   {hostStage("speaker-tap")},
		RealtimeSlotProvider: {hostStage("after-provider")},
	}
	stages, err := NewRealtimeChain(newRealtimeTestProvider(), cfg)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"duplex_provider", "after-provider", "cue", "avatar", "captions", "transcript-reorder", "playout-hold",
		"recording_output", "tap", "extract", "message_broadcast", "save", "turn-observer",
		"audio-pacing-output", "speaker-tap",
	}, chainNames(stages))

	empty, err := NewRealtimeChain(newRealtimeTestProvider(), RealtimeChainConfig{
		After: map[RealtimeSlot][]Stage{RealtimeSlotRecording: {hostStage("externalize")}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"duplex_provider", "cue", "externalize"}, chainNames(empty))
}

// Dropping a slot removes its stage and keeps what is inserted after it; a
// dropped cue stage is never wired to the provider.
func TestNewRealtimeChain_DropSlots(t *testing.T) {
	cfg := fullRealtimeConfig(t)
	cfg.Drop = []RealtimeSlot{RealtimeSlotRecording, RealtimeSlotPacing}
	cfg.After = map[RealtimeSlot][]Stage{RealtimeSlotRecording: {hostStage("externalize")}}
	stages, err := NewRealtimeChain(newRealtimeTestProvider(), cfg)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"duplex_provider", "cue", "transcript-reorder", "playout-hold", "externalize",
		"tap", "extract", "message_broadcast", "save",
	}, chainNames(stages))

	provider := newRealtimeTestProvider()
	stages, err = NewRealtimeChain(provider, RealtimeChainConfig{Drop: []RealtimeSlot{RealtimeSlotCue}})
	require.NoError(t, err)
	assert.Equal(t, []string{"duplex_provider"}, chainNames(stages))
	assert.Nil(t, provider.cues, "no cue stage plays this provider's cues")
}

// The provider is required and cannot be dropped; an unknown slot or a nil
// inserted stage is an error naming it.
func TestNewRealtimeChain_Errors(t *testing.T) {
	_, err := NewRealtimeChain(nil, RealtimeChainConfig{})
	assert.ErrorIs(t, err, ErrRealtimeChainProvider)

	_, err = NewRealtimeChain(newRealtimeTestProvider(),
		RealtimeChainConfig{Drop: []RealtimeSlot{RealtimeSlotProvider}})
	assert.ErrorIs(t, err, ErrRealtimeChainProvider)

	_, err = NewRealtimeChain(newRealtimeTestProvider(), RealtimeChainConfig{Drop: []RealtimeSlot{"avatar"}})
	require.ErrorIs(t, err, ErrRealtimeChainSlot)
	assert.EqualError(t, err, `realtime chain: invalid slot: drop "avatar"`)

	_, err = NewRealtimeChain(newRealtimeTestProvider(), RealtimeChainConfig{
		After: map[RealtimeSlot][]Stage{"avatar": {hostStage("x")}},
	})
	require.ErrorIs(t, err, ErrRealtimeChainSlot)
	assert.EqualError(t, err, `realtime chain: invalid slot: after "avatar"`)

	_, err = NewRealtimeChain(newRealtimeTestProvider(), RealtimeChainConfig{
		After: map[RealtimeSlot][]Stage{RealtimeSlotCue: {nil}},
	})
	require.ErrorIs(t, err, ErrRealtimeChainSlot)
	assert.EqualError(t, err, `realtime chain: invalid slot: nil stage after "cue"`)
}

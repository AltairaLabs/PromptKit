package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingVAD is a caller-supplied VADAnalyzer. It implements the exported
// interface directly, with no dependency on runtime internals — the whole point
// being that an SDK consumer can write one.
type countingVAD struct {
	mu       sync.Mutex
	analyzed int
	events   chan audio.VADEvent
}

func newCountingVAD() *countingVAD {
	return &countingVAD{events: make(chan audio.VADEvent, 1)}
}

func (v *countingVAD) Name() string { return "counting" }

func (v *countingVAD) Analyze(_ context.Context, _ []byte) (float64, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.analyzed++
	return 0, nil
}

func (v *countingVAD) State() audio.VADState                { return audio.VADStateQuiet }
func (v *countingVAD) OnStateChange() <-chan audio.VADEvent { return v.events }
func (v *countingVAD) Reset()                               {}

// TestVADModeConfigCarriesCustomVAD pins that the option survives conversion
// into the stage config, which is what actually decides whether the caller's
// detector runs. Recording it on the SDK config alone would look correct while
// changing nothing.
func TestVADModeConfigCarriesCustomVAD(t *testing.T) {
	custom := newCountingVAD()

	cfg := DefaultVADModeConfig()
	cfg.VAD = custom

	audioTurnCfg := cfg.toAudioTurnConfig(nil)

	if audioTurnCfg.VAD == nil {
		t.Fatal("the stage config has no VAD; the caller's analyzer was dropped in conversion")
	}
	if audioTurnCfg.VAD.Name() != "counting" {
		t.Errorf("stage config holds %q, want the caller's %q", audioTurnCfg.VAD.Name(), "counting")
	}
}

// convCountingTurnDetector counts the audio it is asked about and reports the
// user as done speaking once it has heard 40 chunks.
type convCountingTurnDetector struct {
	mu    sync.Mutex
	audio int
}

func (d *convCountingTurnDetector) Name() string { return "conv-counting" }
func (d *convCountingTurnDetector) ProcessAudio(_ context.Context, _ []byte) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.audio++
	return d.audio >= 40, nil
}
func (d *convCountingTurnDetector) ProcessVADState(_ context.Context, _ audio.VADState) (bool, error) {
	return false, nil
}
func (d *convCountingTurnDetector) IsUserSpeaking() bool { return d.calls() < 40 }
func (d *convCountingTurnDetector) Reset()               {}
func (d *convCountingTurnDetector) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.audio
}

// openConvVAD opens a VAD-mode duplex conversation whose silence and length
// limits are far beyond the test, so only a caller's detector can end a turn.
func openConvVAD(t *testing.T, opts ...Option) (*Conversation, *vadContractRecorder) {
	t.Helper()
	cfg := vadTestConfig()
	cfg.SilenceDuration = time.Minute
	cfg.MaxTurnDuration = time.Minute
	base := []Option{
		WithProvider(mock.NewProvider("mock-vad", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("detector ended it"), newConvMockTTSService(), cfg),
	}
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main", append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	responseCh, err := conv.Response()
	require.NoError(t, err)
	rec := &vadContractRecorder{}
	go rec.consume(responseCh)
	return conv, rec
}

// In a VAD-mode duplex conversation the caller's WithTurnDetector decides when
// the user's turn ends: with silence and length limits out of reach, its
// "no longer speaking" is what sends the turn to STT.
func TestConv_VADModeTurnDetectorEndsTheTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	det := &convCountingTurnDetector{}
	conv, rec := openConvVAD(t, WithTurnDetector(det))
	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool { return len(rec.transcripts()) > 0 }, 3*time.Second),
		"the turn detector must end the turn; it heard %d chunks", det.calls())
	assert.Equal(t, []string{"detector ended it"}, rec.transcripts())
}

// The caller's WithVAD analyzer is the one that hears the caller's audio.
func TestConv_VADModeRunsTheCallersVAD(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	vad := newCountingVAD()
	conv, _ := openConvVAD(t, WithVAD(vad))
	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool {
		vad.mu.Lock()
		defer vad.mu.Unlock()
		return vad.analyzed > 0
	}, 3*time.Second), "the caller's VAD must analyze the caller's audio")
}

package sdk

import (
	"context"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
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

// TestWithVADReachesAudioTurnConfig covers there being no way for an SDK caller
// to supply their own voice activity detector.
//
// The stage accepts one — AudioTurnConfig.VAD is an exported field taking the
// exported VADAnalyzer interface — but nothing in the SDK plumbs it. VADModeConfig
// exposes durations, sample rate, language, voice and speed, and there is a
// WithTurnDetector option, but no equivalent for the VAD itself. A caller with a
// better detector (a real ML VAD, or one tuned for their acoustics) has to
// abandon the SDK and build the pipeline through the runtime API.
func TestWithVADReachesAudioTurnConfig(t *testing.T) {
	custom := newCountingVAD()

	c := &config{}
	if err := WithVAD(custom)(c); err != nil {
		t.Fatalf("WithVAD: %v", err)
	}

	if c.vad == nil {
		t.Fatal("WithVAD did not record the analyzer on the config")
	}
	if c.vad.Name() != "counting" {
		t.Errorf("config holds a %q analyzer, want the caller's %q", c.vad.Name(), "counting")
	}
}

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

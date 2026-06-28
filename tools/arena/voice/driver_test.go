package voice

import (
	"context"
	"testing"
	"time"
)

type fakeIO struct {
	capture chan []byte
	played  [][]byte
	started bool
}

func (f *fakeIO) Start(context.Context) error  { f.started = true; return nil }
func (f *fakeIO) CaptureChunks() <-chan []byte { return f.capture }
func (f *fakeIO) Play(b []byte)                { f.played = append(f.played, b) }
func (f *fakeIO) Flush()                       {}
func (f *fakeIO) Close() error                 { return nil }

func TestDriver_PipesMicToRunnerAndPlaysOutput(t *testing.T) {
	io := &fakeIO{capture: make(chan []byte, 2)}
	// runner echoes each mic frame to play, then returns when mic closes.
	runner := func(ctx context.Context, mic <-chan []byte, play func([]byte), _ func()) error {
		for f := range mic {
			play(f)
		}
		return nil
	}
	d := NewDriver(io, runner, nil)

	io.capture <- []byte{1, 2}
	close(io.capture)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !io.started {
		t.Fatal("expected AudioIO.Start to be called")
	}
	if len(io.played) != 1 {
		t.Fatalf("expected 1 played frame, got %d", len(io.played))
	}
}

func TestDriver_ReportsLevelsViaTapAndPlay(t *testing.T) {
	io := &fakeIO{capture: make(chan []byte, 2)}
	var userLevels, agentLevels []float32
	onLevel := func(user, agent float32) {
		userLevels = append(userLevels, user)
		agentLevels = append(agentLevels, agent)
	}
	runner := func(ctx context.Context, mic <-chan []byte, play func([]byte), _ func()) error {
		for f := range mic {
			play(f)
		}
		return nil
	}
	d := NewDriver(io, runner, onLevel)

	frame := []byte{0x00, 0x40, 0x00, 0x40} // non-silent PCM
	io.capture <- frame
	close(io.capture)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// tapLevels should have emitted one user level > 0
	if len(userLevels) == 0 {
		t.Fatal("expected at least one user level callback")
	}
	if userLevels[0] <= 0 {
		t.Fatalf("expected positive user level, got %v", userLevels[0])
	}
	// play callback should have emitted one agent level > 0
	if len(agentLevels) == 0 {
		t.Fatal("expected at least one agent level callback")
	}
	if agentLevels[len(agentLevels)-1] <= 0 {
		t.Fatalf("expected positive agent level, got %v", agentLevels[len(agentLevels)-1])
	}
}

func TestRMS_SilenceIsZero(t *testing.T) {
	if got := rms(make([]byte, 64)); got != 0 {
		t.Fatalf("silence rms = %v, want 0", got)
	}
}

func TestRMS_NonSilenceIsPositive(t *testing.T) {
	frame := []byte{0x00, 0x40, 0x00, 0x40} // two samples ~0x4000
	if got := rms(frame); got <= 0 {
		t.Fatalf("expected positive rms, got %v", got)
	}
}

func TestDriverWithGuard_DropsQuietMicWhileAgentSpeaks(t *testing.T) {
	io := &fakeIO{capture: make(chan []byte, 2)}
	var received [][]byte
	runner := func(ctx context.Context, mic <-chan []byte, play func([]byte), _ func()) error {
		for f := range mic {
			received = append(received, f)
		}
		return nil
	}
	guard := NewEchoGuard(0.5)
	guard.SetAgentSpeaking(true) // simulate agent actively playing audio

	d := NewDriverWithGuard(io, runner, nil, guard)

	io.capture <- make([]byte, 64) // silence frame — should be gated
	close(io.capture)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(received) != 0 {
		t.Fatalf("expected quiet mic frame to be dropped, got %d frames", len(received))
	}
}

package engine

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
)

func TestBuildAudioMonitor_NilWhenNotEnabled(t *testing.T) {
	eng := &Engine{}
	scenario := &config.Scenario{Duplex: &config.DuplexConfig{}}
	router, sink := eng.buildAudioMonitor(scenario)
	if router != nil || sink != nil {
		t.Fatalf("expected nil router and sink when monitor not enabled, got router=%v sink=%v", router, sink)
	}
}

func TestBuildAudioMonitor_NilWhenScenarioNotDuplex(t *testing.T) {
	eng := &Engine{}
	if err := eng.EnableAudioMonitor(arenaaudio.Options{
		Rate: arenaaudio.Rate24k,
		Mode: arenaaudio.ModeOn,
	}); err != nil {
		t.Fatal(err)
	}
	scenario := &config.Scenario{Duplex: nil}
	router, sink := eng.buildAudioMonitor(scenario)
	if router != nil || sink != nil {
		t.Fatalf("expected nil router and sink for non-duplex scenario, got router=%v sink=%v", router, sink)
	}
}

func TestBuildAudioMonitor_NilWhenScenarioNil(t *testing.T) {
	eng := &Engine{}
	if err := eng.EnableAudioMonitor(arenaaudio.Options{
		Rate: arenaaudio.Rate24k,
		Mode: arenaaudio.ModeOn,
	}); err != nil {
		t.Fatal(err)
	}
	router, sink := eng.buildAudioMonitor(nil)
	if router != nil || sink != nil {
		t.Fatalf("expected nil router and sink for nil scenario, got router=%v sink=%v", router, sink)
	}
}

func TestBuildAudioMonitor_NilWhenModeOff(t *testing.T) {
	eng := &Engine{}
	if err := eng.EnableAudioMonitor(arenaaudio.Options{
		Rate: arenaaudio.Rate24k,
		Mode: arenaaudio.ModeOff,
	}); err != nil {
		t.Fatal(err)
	}
	scenario := &config.Scenario{Duplex: &config.DuplexConfig{}}
	router, sink := eng.buildAudioMonitor(scenario)
	if router != nil || sink != nil {
		t.Fatalf("expected nil router and sink for ModeOff, got router=%v sink=%v", router, sink)
	}
}

func TestBuildAudioMonitor_ConstructsRouterForDuplexModeOn(t *testing.T) {
	eng := &Engine{}
	if err := eng.EnableAudioMonitor(arenaaudio.Options{
		Rate: arenaaudio.Rate24k,
		Mode: arenaaudio.ModeOn,
	}); err != nil {
		t.Fatal(err)
	}
	scenario := &config.Scenario{Duplex: &config.DuplexConfig{}}
	router, sink := eng.buildAudioMonitor(scenario)
	if router == nil {
		t.Fatal("expected router for ModeOn duplex scenario")
	}
	defer router.Close()
	if sink != nil {
		// LocalSink not requested in opts; should remain nil.
		defer sink.Close()
		t.Fatalf("expected nil sink (LocalSink not enabled), got %v", sink)
	}
}

func TestBuildAudioMonitor_NilWhenAutoModeNonTTY(t *testing.T) {
	// Tests run with stdout redirected to a pipe (non-TTY), so ModeAuto
	// must return nil even for a duplex scenario. This protects CI runs from
	// accidentally constructing audio infrastructure.
	eng := &Engine{}
	if err := eng.EnableAudioMonitor(arenaaudio.Options{
		Rate: arenaaudio.Rate24k,
		Mode: arenaaudio.ModeAuto,
	}); err != nil {
		t.Fatal(err)
	}
	scenario := &config.Scenario{Duplex: &config.DuplexConfig{}}
	router, sink := eng.buildAudioMonitor(scenario)
	if router != nil || sink != nil {
		if router != nil {
			defer router.Close()
		}
		if sink != nil {
			defer sink.Close()
		}
		t.Fatalf("expected nil router and sink in non-TTY ModeAuto, got router=%v sink=%v", router, sink)
	}
}

func TestBuildAudioMonitor_LocalSinkSubscribed(t *testing.T) {
	// When LocalSink is enabled, NewLocalSink returns a non-nil sink (noop in
	// headless environments) and the engine subscribes a consumer goroutine to
	// the router. The router teardown must close the consumer channel cleanly.
	eng := &Engine{}
	if err := eng.EnableAudioMonitor(arenaaudio.Options{
		Rate:      arenaaudio.Rate24k,
		Mode:      arenaaudio.ModeOn,
		LocalSink: true,
	}); err != nil {
		t.Fatal(err)
	}
	scenario := &config.Scenario{Duplex: &config.DuplexConfig{}}
	router, sink := eng.buildAudioMonitor(scenario)
	if router == nil {
		t.Fatal("expected router")
	}
	defer router.Close()
	if sink == nil {
		t.Fatal("expected non-nil LocalSink (noop fallback should still return a sink)")
	}
	defer sink.Close()

	// Publish a frame; the consumer goroutine should receive it without
	// blocking the router. We don't assert reception (the goroutine is
	// internal) — just that publish + close are safe.
	router.Publish(arenaaudio.Frame{
		Direction: arenaaudio.DirectionOutput,
		Samples:   []int16{1, 2, 3, 4},
	})
	// Give the dispatch loop a moment to process; the test passing indicates
	// no deadlock between Publish, Subscribe, and Close.
	time.Sleep(20 * time.Millisecond)
}

func TestIsStdoutTTY_ReportsBoolean(t *testing.T) {
	// Sanity check that isStdoutTTY returns a boolean without panicking.
	// In CI / go test, stdout is a pipe so this returns false.
	_ = isStdoutTTY()
}

// TestBuildAudioMonitor_RouterFanout verifies frames published to the router
// reach a subscribed consumer end-to-end. Drives Task 14's wiring guarantee:
// when the engine constructs a router, MonitorTap stages can publish to it
// and consumers (LocalSink, SSE relay, TUI) will receive frames.
func TestBuildAudioMonitor_RouterFanout(t *testing.T) {
	eng := &Engine{}
	if err := eng.EnableAudioMonitor(arenaaudio.Options{
		Rate: arenaaudio.Rate24k,
		Mode: arenaaudio.ModeOn,
	}); err != nil {
		t.Fatal(err)
	}
	scenario := &config.Scenario{Duplex: &config.DuplexConfig{}}
	router, _ := eng.buildAudioMonitor(scenario)
	if router == nil {
		t.Fatal("expected router")
	}
	defer router.Close()

	consumer := router.Subscribe("test-consumer", 10)
	router.Publish(arenaaudio.Frame{
		Direction: arenaaudio.DirectionInput,
		Samples:   []int16{1, 2, 3, 4},
	})

	select {
	case f := <-consumer:
		if len(f.Samples) != 4 {
			t.Fatalf("expected 4 samples, got %d", len(f.Samples))
		}
		if f.Direction != arenaaudio.DirectionInput {
			t.Fatalf("unexpected direction %q", f.Direction)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("router did not deliver frame to subscriber")
	}
}

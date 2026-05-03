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
	if router := eng.buildAudioMonitor(scenario); router != nil {
		defer router.Close()
		t.Fatalf("expected nil router when monitor not enabled, got %v", router)
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
	if router := eng.buildAudioMonitor(scenario); router != nil {
		defer router.Close()
		t.Fatalf("expected nil router for non-duplex scenario, got %v", router)
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
	if router := eng.buildAudioMonitor(nil); router != nil {
		defer router.Close()
		t.Fatalf("expected nil router for nil scenario, got %v", router)
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
	if router := eng.buildAudioMonitor(scenario); router != nil {
		defer router.Close()
		t.Fatalf("expected nil router for ModeOff, got %v", router)
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
	router := eng.buildAudioMonitor(scenario)
	if router == nil {
		t.Fatal("expected router for ModeOn duplex scenario")
	}
	defer router.Close()
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
	if router := eng.buildAudioMonitor(scenario); router != nil {
		defer router.Close()
		t.Fatalf("expected nil router in non-TTY ModeAuto, got %v", router)
	}
}

func TestIsStdoutTTY_ReportsBoolean(t *testing.T) {
	// Sanity check that isStdoutTTY returns a boolean without panicking.
	// In CI / go test, stdout is a pipe so this returns false.
	_ = isStdoutTTY()
}

// TestRegisterAudioMonitorHook_FiresOnRouterCreation verifies that hooks
// registered via RegisterAudioMonitorHook receive the run ID, router, and
// rate when fireAudioMonitorHooks is invoked. The actual fire happens in
// executeRun; this test exercises the wiring directly.
func TestRegisterAudioMonitorHook_FiresOnRouterCreation(t *testing.T) {
	eng := &Engine{}
	if err := eng.EnableAudioMonitor(arenaaudio.Options{
		Rate: arenaaudio.Rate24k,
		Mode: arenaaudio.ModeOn,
	}); err != nil {
		t.Fatal(err)
	}

	var seenRunID string
	var seenRouter *arenaaudio.AudioRouter
	var seenRate int
	eng.RegisterAudioMonitorHook(func(runID string, router *arenaaudio.AudioRouter, rate int) {
		seenRunID = runID
		seenRouter = router
		seenRate = rate
	})

	router := arenaaudio.NewAudioRouter(arenaaudio.Rate24k)
	defer router.Close()
	eng.fireAudioMonitorHooks("test-run-1", router, arenaaudio.Rate24k)

	if seenRunID != "test-run-1" {
		t.Fatalf("expected run id 'test-run-1', got %q", seenRunID)
	}
	if seenRouter != router {
		t.Fatal("expected hook to receive the router pointer")
	}
	if seenRate != arenaaudio.Rate24k {
		t.Fatalf("expected rate %d, got %d", arenaaudio.Rate24k, seenRate)
	}
}

// TestRegisterAudioMonitorHook_MultipleHooks verifies that multiple hooks
// fire in registration order.
func TestRegisterAudioMonitorHook_MultipleHooks(t *testing.T) {
	eng := &Engine{}
	var calls []string
	eng.RegisterAudioMonitorHook(func(runID string, _ *arenaaudio.AudioRouter, _ int) {
		calls = append(calls, "hook1:"+runID)
	})
	eng.RegisterAudioMonitorHook(func(runID string, _ *arenaaudio.AudioRouter, _ int) {
		calls = append(calls, "hook2:"+runID)
	})
	router := arenaaudio.NewAudioRouter(arenaaudio.Rate24k)
	defer router.Close()
	eng.fireAudioMonitorHooks("r", router, arenaaudio.Rate24k)
	if len(calls) != 2 || calls[0] != "hook1:r" || calls[1] != "hook2:r" {
		t.Fatalf("expected hooks fired in order [hook1:r hook2:r], got %v", calls)
	}
}

// TestRegisterAudioMonitorHook_NilHookIgnored verifies that nil hooks are
// silently ignored at registration time.
func TestRegisterAudioMonitorHook_NilHookIgnored(t *testing.T) {
	eng := &Engine{}
	eng.RegisterAudioMonitorHook(nil)
	router := arenaaudio.NewAudioRouter(arenaaudio.Rate24k)
	defer router.Close()
	// Should not panic.
	eng.fireAudioMonitorHooks("r", router, arenaaudio.Rate24k)
}

// TestRegisterAudioMonitorHook_NoHooksNoOp verifies firing with zero hooks
// is a safe no-op.
func TestRegisterAudioMonitorHook_NoHooksNoOp(t *testing.T) {
	eng := &Engine{}
	router := arenaaudio.NewAudioRouter(arenaaudio.Rate24k)
	defer router.Close()
	// Should not panic.
	eng.fireAudioMonitorHooks("r", router, arenaaudio.Rate24k)
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
	router := eng.buildAudioMonitor(scenario)
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

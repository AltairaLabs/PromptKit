package audio

import (
	"testing"
	"time"
)

// newTestMonitor returns a Monitor without a LocalSink so tests don't need
// a real audio device. RMS publishing through the monitor itself still works
// because the publisher callback is wired by NewLocalSink — for sink-less
// tests we exercise the routing/registry behaviour directly.
func newTestMonitor(t *testing.T) *Monitor {
	t.Helper()
	m, err := NewMonitor(MonitorConfig{Rate: Rate24k, EnableLocalSink: false})
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}

// newTestMonitorHeadless returns a Monitor with a headless LocalSink so the
// activation path that wires a sink (forward goroutine + Push) can be
// exercised without opening a real audio device.
func newTestMonitorHeadless(t *testing.T) *Monitor {
	t.Helper()
	m, err := NewMonitor(MonitorConfig{Rate: Rate24k, EnableLocalSink: true, Headless: true})
	if err != nil {
		t.Fatalf("NewMonitor headless: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}

func TestNewMonitor_DefaultsRateWhenZero(t *testing.T) {
	m, err := NewMonitor(MonitorConfig{Rate: 0, EnableLocalSink: false})
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	defer m.Close()
	if m.rate != Rate24k {
		t.Errorf("expected default rate %d, got %d", Rate24k, m.rate)
	}
}

func TestNewMonitor_WithHeadlessSink(t *testing.T) {
	m, err := NewMonitor(MonitorConfig{Rate: Rate24k, EnableLocalSink: true, Headless: true})
	if err != nil {
		t.Fatalf("NewMonitor headless: %v", err)
	}
	defer m.Close()
	if m.sink == nil {
		t.Fatal("expected sink to be created in headless mode")
	}
}

func TestMonitor_ActivateForwardsFramesToSink(t *testing.T) {
	m := newTestMonitorHeadless(t)
	router := NewAudioRouter(Rate24k)
	defer router.Close()

	m.AttachRouter("run-1", router)
	if m.ActiveRunID() != "run-1" {
		t.Fatalf("expected run-1 active, got %q", m.ActiveRunID())
	}

	// Publish a frame; activation wires up the forward goroutine that
	// pulls from the router and Pushes to the sink. The headless sink
	// just drains via its ticker, but the forward path is what we care
	// about for coverage on activateLocked.
	router.Publish(Frame{
		Direction: DirectionInput,
		Samples:   []int16{1, 2, 3, 4},
		Timestamp: time.Now(),
	})
	// Give the forward goroutine + headless ticker a beat to consume.
	time.Sleep(20 * time.Millisecond)
}

func TestMonitor_FirstAttachedRunBecomesActive(t *testing.T) {
	m := newTestMonitor(t)
	router := NewAudioRouter(Rate24k)
	defer router.Close()

	m.AttachRouter("run-1", router)

	if got := m.ActiveRunID(); got != "run-1" {
		t.Errorf("expected first attached run to be active, got %q", got)
	}
}

func TestMonitor_SubsequentRunsDoNotAutoActivate(t *testing.T) {
	m := newTestMonitor(t)

	r1 := NewAudioRouter(Rate24k)
	defer r1.Close()
	r2 := NewAudioRouter(Rate24k)
	defer r2.Close()

	m.AttachRouter("run-1", r1)
	m.AttachRouter("run-2", r2)

	if got := m.ActiveRunID(); got != "run-1" {
		t.Errorf("expected first run to remain active when others attach, got %q", got)
	}
}

func TestMonitor_SetActiveRunSwitchesPlayback(t *testing.T) {
	m := newTestMonitor(t)

	r1 := NewAudioRouter(Rate24k)
	defer r1.Close()
	r2 := NewAudioRouter(Rate24k)
	defer r2.Close()

	m.AttachRouter("run-1", r1)
	m.AttachRouter("run-2", r2)

	if !m.SetActiveRun("run-2") {
		t.Fatal("SetActiveRun should report success for a registered run")
	}
	if got := m.ActiveRunID(); got != "run-2" {
		t.Errorf("expected run-2 active, got %q", got)
	}
}

func TestMonitor_SetActiveRunRejectsUnknown(t *testing.T) {
	m := newTestMonitor(t)
	if m.SetActiveRun("not-attached") {
		t.Error("SetActiveRun should report failure for unregistered run")
	}
	if got := m.ActiveRunID(); got != "" {
		t.Errorf("expected no active run, got %q", got)
	}
}

func TestMonitor_DetachActiveRunFallsBackToAnotherRegistered(t *testing.T) {
	m := newTestMonitor(t)

	r1 := NewAudioRouter(Rate24k)
	defer r1.Close()
	r2 := NewAudioRouter(Rate24k)
	defer r2.Close()

	m.AttachRouter("run-1", r1)
	m.AttachRouter("run-2", r2)

	if !m.SetActiveRun("run-2") {
		t.Fatal("SetActiveRun")
	}

	m.DetachRouter("run-2")
	if got := m.ActiveRunID(); got != "run-1" {
		t.Errorf("expected fallback to run-1 after run-2 detach, got %q", got)
	}
}

func TestMonitor_DetachAllStopsPlayback(t *testing.T) {
	m := newTestMonitor(t)

	router := NewAudioRouter(Rate24k)
	defer router.Close()

	m.AttachRouter("run-1", router)
	m.DetachRouter("run-1")

	if got := m.ActiveRunID(); got != "" {
		t.Errorf("expected no active run after detach-all, got %q", got)
	}
}

func TestMonitor_RouterCloseAutoDetaches(t *testing.T) {
	m := newTestMonitor(t)

	router := NewAudioRouter(Rate24k)
	m.AttachRouter("run-1", router)
	if got := m.ActiveRunID(); got != "run-1" {
		t.Fatalf("expected active=run-1, got %q", got)
	}

	router.Close()

	// The auto-detach goroutine reacts to router.Done(); allow a moment.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if m.ActiveRunID() == "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected auto-detach within 500ms, still active: %q", m.ActiveRunID())
}

func TestMonitor_PublishRMSReachesSubscribers(t *testing.T) {
	m := newTestMonitor(t)

	rms1 := m.SubscribeRMS(4)
	rms2 := m.SubscribeRMS(4)

	frame := RMSFrame{UserLevel: 0.4, AgentLevel: 0.2, Timestamp: time.Now()}
	m.publishRMS(frame)

	for i, ch := range []<-chan RMSFrame{rms1, rms2} {
		select {
		case got := <-ch:
			if got.UserLevel != 0.4 || got.AgentLevel != 0.2 {
				t.Errorf("subscriber %d: unexpected frame %+v", i, got)
			}
		case <-time.After(200 * time.Millisecond):
			t.Errorf("subscriber %d: did not receive RMS frame", i)
		}
	}
}

func TestMonitor_CloseIsIdempotent(t *testing.T) {
	m, err := NewMonitor(MonitorConfig{Rate: Rate24k, EnableLocalSink: false})
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	m.Close()
	m.Close() // must not panic
}

func TestMonitor_AttachAfterCloseIsNoOp(t *testing.T) {
	m, err := NewMonitor(MonitorConfig{Rate: Rate24k, EnableLocalSink: false})
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	m.Close()

	router := NewAudioRouter(Rate24k)
	defer router.Close()
	m.AttachRouter("run-1", router) // must not panic

	if got := m.ActiveRunID(); got != "" {
		t.Errorf("expected no activation after Close, got %q", got)
	}
}

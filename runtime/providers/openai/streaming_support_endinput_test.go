package openai

import (
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// fakeEndInputSession is a minimal StreamInputSession that tracks EndInput calls.
type fakeEndInputSession struct {
	providers.StreamInputSession
	endInputCalls atomic.Int32
}

func (f *fakeEndInputSession) EndInput() {
	f.endInputCalls.Add(1)
}

func TestRealtimeSessionBookkeeping_ForwardsEndInput(t *testing.T) {
	t.Parallel()
	inner := &fakeEndInputSession{}
	wrapper := &realtimeSessionBookkeeping{
		StreamInputSession: inner,
		release:            func() {},
	}

	wrapper.EndInput()

	if got := inner.endInputCalls.Load(); got != 1 {
		t.Errorf("EndInput forwarded %d times, want 1", got)
	}
}

func TestRealtimeSessionBookkeeping_EndInputNoopWithoutInterface(t *testing.T) {
	t.Parallel()
	// Inner session that does NOT implement EndInput — should not panic.
	wrapper := &realtimeSessionBookkeeping{
		StreamInputSession: &fakeStreamInputSession{},
		release:            func() {},
	}
	if r := func() (r any) { defer func() { r = recover() }(); wrapper.EndInput(); return nil }(); r != nil {
		t.Fatalf("EndInput panicked on an inner session without EndInput: %v", r)
	}
}

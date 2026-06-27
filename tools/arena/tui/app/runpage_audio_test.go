package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunPage_AudioMonitor_NilForTextRun verifies that an ordinary (non-audio)
// run does not create a process-wide audio Monitor — this is what keeps the
// conversation meter realtime-only: no monitor means no AudioLevelMsg and thus
// no meter. Cancel must remain safe when no monitor was attached.
func TestRunPage_AudioMonitor_NilForTextRun(t *testing.T) {
	ctx, eng, plan := newRunFixtureContext(t)
	p := NewRunPage(ctx, eng, plan, 1, "", len(plan.Combinations))

	sink := &msgSink{}
	p.Activate(sink.send)
	waitForFinish(t, p)

	require.Nil(t, p.audioMonitor, "text run must not attach an audio monitor")
	require.NotPanics(t, func() { p.Cancel() })
}

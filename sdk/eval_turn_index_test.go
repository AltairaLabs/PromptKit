package sdk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A session eval on the text path carries the 0-based index of the session's
// last turn: two turns, so 1.
func TestSessionEvals_TextTurnIndexIsZeroBased(t *testing.T) {
	bus, col, registry, _ := newStepEvalHarness(t)
	registry.Register(&stepRecordingHandler{typeName: "sess_stub"})
	registry.Register(&stepRecordingHandler{typeName: "conv_stub"})

	c, err := Open(writeWorkflowTestPack(t, lifecyclePackJSON), "chat",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithEventBus(bus),
	)
	require.NoError(t, err)
	for _, msg := range []string{"one", "two"} {
		_, err = c.Send(context.Background(), msg)
		require.NoError(t, err)
	}
	require.NoError(t, c.Close())

	require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerOnSessionComplete)) >= 1 },
		5*time.Second, 5*time.Millisecond, "the session evals never ran")
	got := col.byTrigger(evals.TriggerOnSessionComplete)
	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].TurnIndex, "two turns: the last is index 1")
}

// A session eval on the realtime path carries the 0-based index of the
// session's last completed turn, as the text path does: three turns, so 2.
func TestSessionEvals_DuplexTurnIndexIsLastTurn(t *testing.T) {
	bus, col, registry, _ := newStepEvalHarness(t)
	registry.Register(&stepRecordingHandler{typeName: "sess_stub"})
	registry.Register(&stepRecordingHandler{typeName: "conv_stub"})
	ms := mock.NewMockStreamSession()
	conv, err := OpenDuplex(writeWorkflowTestPack(t, lifecyclePackJSON), "chat",
		WithSkipSchemaValidation(),
		WithProvider(&heardEvalProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false), sess: ms}),
		WithStateStore(statestore.NewMemoryStore()),
		WithEvalRegistry(registry),
		WithEventBus(bus),
		WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio}}),
	)
	require.NoError(t, err)
	ch, err := conv.Response()
	require.NoError(t, err)
	var turns atomic.Int32
	go func() {
		for c := range ch {
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				turns.Add(1)
			}
		}
	}()

	const n = 3
	for i := 1; i <= n; i++ {
		require.NoError(t, conv.SendText(context.Background(), "hello"))
		require.Eventually(t, func() bool { return len(ms.GetTexts()) >= i }, 2*time.Second, 2*time.Millisecond)
		ms.EmitChunk(&providers.StreamChunk{Delta: "Hi.", Metadata: map[string]any{"type": "output_transcription"}})
		stop := "stop"
		ms.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
		require.Eventually(t, func() bool { return int(turns.Load()) >= i }, 3*time.Second, 2*time.Millisecond)
	}

	require.NoError(t, conv.Close())
	require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerOnSessionComplete)) >= 1 },
		5*time.Second, 5*time.Millisecond, "the session evals never ran")
	got := col.byTrigger(evals.TriggerOnSessionComplete)
	require.Len(t, got, 1)
	assert.Equal(t, n-1, got[0].TurnIndex, "three realtime turns: the last is index 2")
}

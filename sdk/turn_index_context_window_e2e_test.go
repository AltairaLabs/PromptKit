package sdk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestE2E_ContextWindowConversationReportsTranscriptTurn covers #1945.
// WithContextWindow swaps the load stage for ContextAssemblyStage, which
// loaded only the hot window and never derived the turn, so every guardrail
// and eval event on such a conversation carried turn 0 — the documented
// "unknown" value — and a resumed one could not be placed against its
// transcript at all. The window here is two messages, smaller than the
// history, so counting what the stage loaded would still be wrong.
func TestE2E_ContextWindowConversationReportsTranscriptTurn(t *testing.T) {
	ctx := context.Background()

	packPath := filepath.Join(t.TempDir(), "turnalign.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(turnAlignPackJSON), 0o600))

	store := statestore.NewMemoryStore()
	const convID = "windowed-conv"
	require.NoError(t, store.AppendMessages(ctx, convID, []types.Message{
		{Role: "user", Content: "one"}, {Role: "assistant", Content: "1"},
		{Role: "user", Content: "two"}, {Role: "assistant", Content: "2"},
		{Role: "user", Content: "three"}, {Role: "assistant", Content: "3"},
	}))

	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})

	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })

	col := &turnCollector{}
	bus.Subscribe(events.EventValidationPassed, func(e *events.Event) {
		if d, ok := e.Data.(*events.ValidationEventData); ok {
			col.mu.Lock()
			col.validation = append(col.validation, d.TurnIndex)
			col.mu.Unlock()
		}
	})
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		if d, ok := e.Data.(*events.EvalEventData); ok {
			col.mu.Lock()
			col.eval = append(col.eval, d.TurnIndex)
			col.mu.Unlock()
		}
	})

	conv, err := Open(packPath, "default",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithStateStore(store),
		WithConversationID(convID),
		WithContextWindow(2),
		WithEventBus(bus),
	)
	require.NoError(t, err)
	defer conv.Close()

	_, err = conv.Send(ctx, "four")
	require.NoError(t, err)
	_, err = conv.Send(ctx, "five")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		v, e := col.snapshot()
		return len(v) >= 2 && len(e) >= 2
	}, 5*time.Second, 10*time.Millisecond, "expected a validation and an eval event per turn")

	validation, eval := col.snapshot()
	assert.Equal(t, []int{4, 5}, eval, "evals must number turns from the transcript, not the window")
	assert.Equal(t, eval, validation, "guardrails and evals must agree on the turn")
	assert.Equal(t, 5, conv.turnState.TurnIndex())
}

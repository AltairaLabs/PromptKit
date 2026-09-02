package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
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

// A pack carrying BOTH a validator and an eval on the same prompt, so one turn
// produces a validation event and an eval event that must agree on the turn.
const turnAlignPackJSON = `{
  "$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
  "schema_version": "2025.1",
  "id": "turn-align-pack",
  "version": "1.0.0",
  "template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
  "prompts": {
    "default": {
      "id": "default", "name": "Default", "description": "helpful",
      "version": "1.0.0",
      "system_template": "You are helpful.",
      "validators": [
        {"type": "length", "enabled": true, "params": {"max": 5000}}
      ],
      "evals": [
        {"id": "e1", "type": "turn_align_stub", "trigger": "every_turn"}
      ]
    }
  }
}`

// alwaysPassEvalHandler is a deterministic eval so the assertion is about turn
// numbering rather than about what any real handler scores.
type alwaysPassEvalHandler struct{}

func (h *alwaysPassEvalHandler) Type() string { return "turn_align_stub" }

func (h *alwaysPassEvalHandler) Eval(
	_ context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	score := 1.0
	return &evals.EvalResult{Score: &score}, nil
}

// turnCollector gathers the turn index off both event families.
type turnCollector struct {
	mu         sync.Mutex
	validation []int
	eval       []int
}

func (c *turnCollector) snapshot() (validation, eval []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := append([]int(nil), c.validation...)
	e := append([]int(nil), c.eval...)
	sort.Ints(v)
	sort.Ints(e)
	return v, e
}

// End-to-end proof through the real Open/Send path that a guardrail and an eval
// covering the same turn report the SAME turn index.
//
// The unit tests cover the pieces; only this covers the wiring between them —
// that Conversation.turnState actually reaches both the ProviderStage (through
// intpipeline.Build) and the eval dispatch, so the number the load stage derived
// is the one both report. That is invisible to a test driving a stage directly.
//
// The turn index is what lines these events up with the transcript, so a
// disagreement here makes both unplaceable.
func TestE2E_GuardrailAndEvalAgreeOnTurnIndex(t *testing.T) {
	packPath := filepath.Join(t.TempDir(), "turnalign.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(turnAlignPackJSON), 0o600))

	// Built-ins plus the stub: the validator resolves "length" from this same
	// registry, so it cannot be an empty one.
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
		WithEventBus(bus),
	)
	require.NoError(t, err)
	defer conv.Close()

	const turns = 3
	for range turns {
		_, sendErr := conv.Send(context.Background(), "hello")
		require.NoError(t, sendErr)
	}

	// Turn evals dispatch on a goroutine, so wait for all of them to land.
	require.Eventually(t, func() bool {
		v, e := col.snapshot()
		return len(v) >= turns && len(e) >= turns
	}, 5*time.Second, 10*time.Millisecond,
		"expected a validation and an eval event for each of %d turns", turns)

	validation, eval := col.snapshot()

	assert.Equal(t, []int{1, 2, 3}, eval,
		"evals should number turns 1..3")
	assert.Equal(t, eval, validation,
		"a guardrail and an eval covering the same turn must report the same turn index")

	// Agreement alone is necessary but not sufficient, so pin the seam too: the
	// number both sides read is the one the pipeline derived onto the
	// conversation's own TurnState. Paired with
	// TestConversation_PipelineConfigCarriesTheTurnState and the stage-side
	// TestStateStoreLoadStage_DerivesTurnIndexFromTranscript, the chain closes.
	require.NotNil(t, conv.turnState, "the conversation must own the turn state")
	assert.Equal(t, turns, conv.turnState.TurnIndex(),
		"the pipeline must have derived the last turn onto the shared turn state")
}

// The pipeline must be built against the conversation's own TurnState.
//
// Without this the load stage derives the turn onto an object the conversation
// never reads, so evals fall back to their dispatch count and the two families
// disagree for any resumed conversation.
func TestConversation_PipelineConfigCarriesTheTurnState(t *testing.T) {
	packPath := filepath.Join(t.TempDir(), "turnalign.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(turnAlignPackJSON), 0o600))

	registry := evals.NewEvalTypeRegistry()
	registry.Register(&alwaysPassEvalHandler{})

	conv, err := Open(packPath, "default",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
	)
	require.NoError(t, err)
	defer conv.Close()

	require.NotNil(t, conv.turnState)

	cfg := conv.buildPipelineConfig(nil, "conv-1", nil, nil)
	require.NotNil(t, cfg.TurnState, "the builder must be given a turn state")
	assert.Same(t, conv.turnState, cfg.TurnState,
		"the pipeline must share the conversation's turn state, not a copy")
}

// A conversation resumed against an existing transcript reports the turn the
// transcript says, on BOTH event families.
//
// This is the case that discriminates. On a fresh conversation the eval
// middleware's own dispatch count and the transcript-derived turn agree at
// 1, 2, 3 — so the alignment test above passes even if evals ignore the
// pipeline entirely. Resume with three turns already on the record and the two
// sources diverge immediately: the dispatch count restarts at 1 while the
// transcript is on turn 4. The transcript is the one that is right, because it
// is the thing these events are placed against.
func TestE2E_ResumedConversationReportsTranscriptTurn(t *testing.T) {
	ctx := context.Background()

	packPath := filepath.Join(t.TempDir(), "turnalign.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(turnAlignPackJSON), 0o600))

	// Three completed turns already persisted under this conversation id.
	store := statestore.NewMemoryStore()
	const convID = "resumed-conv"
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
		WithEventBus(bus),
	)
	require.NoError(t, err)
	defer conv.Close()

	_, err = conv.Send(ctx, "four")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		v, e := col.snapshot()
		return len(v) >= 1 && len(e) >= 1
	}, 5*time.Second, 10*time.Millisecond, "expected both event families")

	validation, eval := col.snapshot()
	assert.Equal(t, []int{4}, eval,
		"a resumed conversation continues the transcript, it does not restart at 1")
	assert.Equal(t, []int{4}, validation,
		"the guardrail must report the same resumed turn")
}

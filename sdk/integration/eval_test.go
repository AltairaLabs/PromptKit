package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register built-in handlers
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// evalsPackJSON defines a pack with inline eval definitions.
// The "contains" eval checks whether the assistant response contains
// the pattern "mock" (the mock provider always returns a canned response
// containing "mock" in its text).
const evalsPackJSON = `{
	"id": "eval-test",
	"version": "1.0.0",
	"description": "Pack with evals for integration tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant."
		}
	},
	"evals": [
		{
			"id": "check-response-length",
			"type": "min_length",
			"trigger": "every_turn",
			"params": { "min": 1 },
			"groups": ["groupA"]
		},
		{
			"id": "check-no-forbidden",
			"type": "content_excludes",
			"trigger": "every_turn",
			"params": { "patterns": ["XYZZY_NEVER_APPEARS"] },
			"groups": ["groupA", "groupB"]
		},
		{
			"id": "check-groupB-only",
			"type": "min_length",
			"trigger": "every_turn",
			"params": { "min": 1 },
			"groups": ["groupB"]
		}
	]
}`

// ---------------------------------------------------------------------------
// 10.1 — Turn-level eval execution emits eval.completed events
// ---------------------------------------------------------------------------

func TestEval_TurnLevelEvalExecution(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	registry := evals.NewEvalTypeRegistry()
	runner := evals.NewEvalRunner(registry)

	conv := openTestConvWithPack(t, evalsPackJSON, "chat",
		sdk.WithEventBus(bus),
		sdk.WithEvalRunner(runner),
	)

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Hello")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())

	// Wait for pipeline to complete, then give async eval dispatch time to finish.
	require.True(t, ec.waitForEvent(events.EventPipelineCompleted, 2*time.Second),
		"pipeline.completed event should appear")

	// Eval dispatch is async — wait for eval.completed events.
	found := ec.waitForEvent(events.EventEvalCompleted, 3*time.Second)
	require.True(t, found, "at least one eval.completed event should appear")

	evalEvents := ec.ofType(events.EventEvalCompleted)
	assert.GreaterOrEqual(t, len(evalEvents), 1, "should have at least one eval.completed event")

	// Verify the event data has expected fields.
	for _, e := range evalEvents {
		data, ok := e.Data.(*events.EvalEventData)
		require.True(t, ok, "eval event data should be *EvalEventData")
		assert.NotEmpty(t, data.EvalID, "eval event should have an EvalID")
		assert.NotEmpty(t, data.EvalType, "eval event should have an EvalType")
	}
}

// ---------------------------------------------------------------------------
// 10.2 — Eval group filtering limits which evals execute
// ---------------------------------------------------------------------------

func TestEval_EvalGroupFiltering(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	registry := evals.NewEvalTypeRegistry()
	runner := evals.NewEvalRunner(registry)

	// Only run evals in "groupA" — should exclude "check-groupB-only".
	conv := openTestConvWithPack(t, evalsPackJSON, "chat",
		sdk.WithEventBus(bus),
		sdk.WithEvalRunner(runner),
		sdk.WithEvalGroups("groupA"),
	)

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Hello")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())

	// Wait for eval events.
	require.True(t, ec.waitForEvent(events.EventPipelineCompleted, 2*time.Second))

	// Give async eval dispatch time to finish.
	ec.waitForEvent(events.EventEvalCompleted, 3*time.Second)

	evalEvents := ec.ofType(events.EventEvalCompleted)

	// Collect eval IDs from events.
	evalIDs := make(map[string]bool)
	for _, e := range evalEvents {
		if data, ok := e.Data.(*events.EvalEventData); ok {
			evalIDs[data.EvalID] = true
		}
	}

	// "check-response-length" and "check-no-forbidden" are in groupA.
	assert.True(t, evalIDs["check-response-length"],
		"groupA eval 'check-response-length' should have run")
	assert.True(t, evalIDs["check-no-forbidden"],
		"groupA eval 'check-no-forbidden' should have run")

	// "check-groupB-only" is only in groupB and should NOT have run.
	assert.False(t, evalIDs["check-groupB-only"],
		"groupB-only eval should not run when filtering to groupA")
}

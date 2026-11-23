package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Activating a skill grants its tools from the next round, which sends the
// provider a different tools array: the conversation's bus reports it once,
// with the reason, the tool gained and the round first sent it. A later turn
// sent the same tools reports nothing.
func TestToolOfferRebuilt_SkillActivationIsReported(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{{ID: "c1", Name: "skill__activate", Args: []byte(`{"name":"refund-processing"}`)}},
		[]types.MessageToolCall{{ID: "c2", Name: "refund", Args: []byte(`{"id":"ORD-1"}`)}},
	)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{}, WithSkillsDir(writeToolGrantSkill(t)))

	var mu sync.Mutex
	var got []events.ToolOfferRebuiltData
	conv.EventBus().Subscribe(events.EventToolOfferRebuilt, func(e *events.Event) {
		if d, ok := e.Data.(*events.ToolOfferRebuiltData); ok {
			mu.Lock()
			got = append(got, *d)
			mu.Unlock()
		}
	})

	_, err := conv.Send(context.Background(), "refund ORD-1")
	require.NoError(t, err)
	_, err = conv.Send(context.Background(), "thanks")
	require.NoError(t, err)

	assert.Contains(t, provider.offeredOn(t, 1), "refund", "the round after the activation offers the grant")
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) > 0
	}, 2*time.Second, 5*time.Millisecond, "no tool.offer.rebuilt event")
	time.Sleep(50 * time.Millisecond) // a second event, if one were published, has been delivered
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonSkillGrant}, Added: []string{"refund"}, Round: 2,
	}}, got)
}

// twoStateOfferPack has two workflow states whose prompts offer different
// tools.
const twoStateOfferPack = `{
	"id": "two-state-offer", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {
		"orders": {"id": "orders", "name": "Orders", "version": "1.0.0",
			"system_template": "You look up orders.", "tools": ["lookup_order"]},
		"refunds": {"id": "refunds", "name": "Refunds", "version": "1.0.0",
			"system_template": "You issue refunds.", "tools": ["refund"]}},
	"tools": {
		"lookup_order": {"name": "lookup_order", "description": "Look up an order", "mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}},
		"refund": {"name": "refund", "description": "Issue a refund", "mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}}},
	"workflow": {"version": 1, "entry": "orders",
		"states": {
			"orders": {"prompt_task": "orders", "on_event": {"Refund": "refunds"}},
			"refunds": {"prompt_task": "refunds"}}}
}`

// A workflow opens a conversation per state, and the first call in a state
// whose prompt offers other tools sends the provider a different tools
// array: the workflow reports it once, as a workflow state change.
func TestToolOfferRebuilt_WorkflowTransitionIsReported(t *testing.T) {
	provider := newNudgeScriptProvider()
	bus := events.NewEventBus()
	var got []events.ToolOfferRebuiltData
	var mu sync.Mutex
	bus.Subscribe(events.EventToolOfferRebuilt, func(e *events.Event) {
		if d, ok := e.Data.(*events.ToolOfferRebuiltData); ok {
			mu.Lock()
			got = append(got, *d)
			mu.Unlock()
		}
	})
	wc, err := OpenWorkflow(createTestPackFile(t, twoStateOfferPack), WithProvider(provider),
		WithSkipSchemaValidation(), WithEventBus(bus))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	ctx := context.Background()

	_, err = wc.Send(ctx, "where is my order")
	require.NoError(t, err)
	_, err = wc.Transition("Refund")
	require.NoError(t, err)
	_, err = wc.Send(ctx, "refund it")
	require.NoError(t, err)
	_, err = wc.Send(ctx, "thanks")
	require.NoError(t, err)

	assert.Equal(t, []string{"lookup_order", "workflow__transition"}, provider.offeredOn(t, 0))
	assert.Equal(t, []string{"refund"}, provider.offeredOn(t, 1), "the final state has no transition to offer")
	bus.Close() // delivers every event published so far
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonWorkflowState}, Added: []string{"refund"},
		Removed: []string{"lookup_order", "workflow__transition"}, Round: 1,
	}}, got)
}

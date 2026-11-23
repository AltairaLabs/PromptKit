package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover tool loading mode stable through Open: every provider call of
// the conversation is sent the same tools array, and what the model may call
// is narrowed per call (PredictionRequest.ActiveTools) and enforced.

// rebuiltEvents collects the tool.offer.rebuilt events a bus delivers.
func rebuiltEvents(bus *events.EventBus) func() []events.ToolOfferRebuiltData {
	var mu sync.Mutex
	var got []events.ToolOfferRebuiltData
	bus.Subscribe(events.EventToolOfferRebuilt, func(e *events.Event) {
		if d, ok := e.Data.(*events.ToolOfferRebuiltData); ok {
			mu.Lock()
			got = append(got, *d)
			mu.Unlock()
		}
	})
	return func() []events.ToolOfferRebuiltData {
		bus.Close() // delivers every event published so far
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

var stableSkillArray = []string{
	"cancel_order", "lookup_order", "refund", "skill__activate", "skill__deactivate", "skill__read_resource",
}

// A skill grant in stable mode: the tool the skill grants is sent from the
// first call, a call to it before the skill is active is refused and does not
// run, and once the skill is active it runs. The array never changes, and
// nothing is reported rebuilt.
func TestToolLoadingStable_SkillGrantKeepsTheArray(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{{ID: "c0", Name: "refund", Args: []byte(`{"id":"ORD-1"}`)}},
		[]types.MessageToolCall{{ID: "c1", Name: "skill__activate", Args: []byte(`{"name":"refund-processing"}`)}},
		[]types.MessageToolCall{{ID: "c2", Name: "refund", Args: []byte(`{"id":"ORD-1"}`)}},
	)
	bus := events.NewEventBus()
	got := rebuiltEvents(bus)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{}, WithSkillsDir(writeToolGrantSkill(t)),
		WithEventBus(bus), WithToolLoading(ToolLoading{Mode: ToolLoadingStable}))

	_, err := conv.Send(context.Background(), "refund ORD-1")
	require.NoError(t, err)

	for call := 0; call < 4; call++ {
		assert.Equal(t, stableSkillArray, provider.offeredOn(t, call), "call %d", call)
	}
	provider.mu.Lock()
	active := provider.active
	provider.mu.Unlock()
	notGranted := []string{"cancel_order", "lookup_order", "skill__activate", "skill__deactivate", "skill__read_resource"}
	assert.Equal(t, [][]string{notGranted, notGranted, nil, nil}, active)
	texts, errs := provider.resultsOn(1)
	assert.Equal(t, "Tool refund was not run: it is not available this turn", texts["c0"])
	assert.Equal(t, texts["c0"], errs["c0"])
	texts, errs = provider.resultsOn(3)
	assert.Equal(t, "refunded", texts["c2"])
	assert.Empty(t, errs["c2"])
	assert.Empty(t, got())
}

// RuntimeConfig's spec.tool_loading.mode selects stable mode as the option
// does.
func TestToolLoadingStable_FromRuntimeConfig(t *testing.T) {
	rc := filepath.Join(t.TempDir(), "runtime.yaml")
	require.NoError(t, os.WriteFile(rc, []byte("apiVersion: promptkit.altairalabs.ai/v1alpha1\n"+
		"kind: RuntimeConfig\nspec:\n  tool_loading:\n    mode: stable\n"), 0o600))
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{{ID: "c1", Name: "skill__activate", Args: []byte(`{"name":"refund-processing"}`)}},
	)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{}, WithSkillsDir(writeToolGrantSkill(t)),
		WithRuntimeConfig(rc))

	_, err := conv.Send(context.Background(), "refund ORD-1")
	require.NoError(t, err)

	assert.Equal(t, stableSkillArray, provider.offeredOn(t, 0))
	assert.Equal(t, stableSkillArray, provider.offeredOn(t, 1))
}

// An unknown mode is refused when the conversation opens.
func TestToolLoading_UnknownModeIsAnError(t *testing.T) {
	_, err := Open(writeWorkflowTestPack(t, nudgePackJSON), "chat", WithProvider(newNudgeScriptProvider()),
		WithSkipSchemaValidation(), WithToolLoading(ToolLoading{Mode: "lazy"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `tool loading mode "lazy": want "rebuild" or "stable"`)
}

// The skill__activate index lists every skill in stable mode, though a skills
// selector chose one: a narrowed index would change the array each turn the
// selector chose differently. Rebuild mode narrows it.
func TestToolLoadingStable_SkillIndexIsNotNarrowed(t *testing.T) {
	const both = "Activate a skill to load its instructions and tools.\n\nAvailable skills:\n" +
		"- cancellations: cancellations policy\n- refund-processing: refund-processing policy"
	const one = "Activate a skill to load its instructions and tools.\n\nAvailable skills:\n" +
		"- refund-processing: refund-processing policy"
	for mode, want := range map[ToolLoadingMode]string{ToolLoadingStable: both, ToolLoadingRebuild: one} {
		root := t.TempDir()
		writeGrantingSkill(t, root, "refund-processing", "refund")
		writeGrantingSkill(t, root, "cancellations", "cancel_order")
		provider := newNudgeScriptProvider()
		conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{}, WithSkillsDir(root),
			WithSkillSelectorOption(grantsToolSelector{tool: "refund"}), WithToolLoading(ToolLoading{Mode: mode}))

		_, err := conv.Send(context.Background(), "I need a refund for ORD-1")
		require.NoError(t, err)

		provider.mu.Lock()
		assert.Equal(t, want, provider.descs[0]["skill__activate"], mode)
		provider.mu.Unlock()
	}
}

// twoWayOfferPack has two workflow states, each with its own tools and a
// transition to the other.
const twoWayOfferPack = `{
	"id": "two-way-offer", "version": "1.0.0",
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
			"refunds": {"prompt_task": "refunds", "on_event": {"Orders": "orders"}}}}
}`

// A workflow in stable mode sends every state the tools of every state's
// prompt: a transition changes what the model may call, not which tools the
// array holds. The array still changes, and is reported: workflow__transition's
// input schema lists the events of the state it is offered in.
func TestToolLoadingStable_WorkflowStatesShareTheArray(t *testing.T) {
	provider := newNudgeScriptProvider()
	bus := events.NewEventBus()
	got := rebuiltEvents(bus)
	wc, err := OpenWorkflow(createTestPackFile(t, twoWayOfferPack), WithProvider(provider),
		WithSkipSchemaValidation(), WithEventBus(bus), WithToolLoading(ToolLoading{Mode: ToolLoadingStable}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	ctx := context.Background()

	_, err = wc.Send(ctx, "where is my order")
	require.NoError(t, err)
	_, err = wc.Transition("Refund")
	require.NoError(t, err)
	_, err = wc.Send(ctx, "refund it")
	require.NoError(t, err)

	all := []string{"lookup_order", "refund", "workflow__transition"}
	assert.Equal(t, all, provider.offeredOn(t, 0))
	assert.Equal(t, all, provider.offeredOn(t, 1))
	provider.mu.Lock()
	assert.Equal(t, [][]string{{"lookup_order", "workflow__transition"}, {"refund", "workflow__transition"}},
		provider.active)
	provider.mu.Unlock()
	assert.Equal(t, []events.ToolOfferRebuiltData{{Reasons: []string{events.ToolOfferReasonWorkflowState}, Round: 1}},
		got(), "the same tools, one of them described differently")
}

// An explicit WithToolLoading wins over RuntimeConfig's spec.tool_loading,
// whichever comes first.
func TestToolLoading_OptionWinsOverRuntimeConfig(t *testing.T) {
	rc := filepath.Join(t.TempDir(), "runtime.yaml")
	require.NoError(t, os.WriteFile(rc, []byte("apiVersion: promptkit.altairalabs.ai/v1alpha1\n"+
		"kind: RuntimeConfig\nspec:\n  tool_loading:\n    mode: stable\n"), 0o600))
	rebuild := WithToolLoading(ToolLoading{Mode: ToolLoadingRebuild})
	for name, opts := range map[string][]Option{
		"option first": {rebuild, WithRuntimeConfig(rc)},
		"file first":   {WithRuntimeConfig(rc), rebuild},
	} {
		provider := newNudgeScriptProvider()
		conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{},
			append([]Option{WithSkillsDir(writeToolGrantSkill(t))}, opts...)...)

		_, err := conv.Send(context.Background(), "refund ORD-1")
		require.NoError(t, err)

		assert.NotContains(t, provider.offeredOn(t, 0), "refund", "%s: rebuild mode sends no ungranted tool", name)
	}
}

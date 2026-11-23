package provider

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestWithStableID_KeepsAnIDAndSetsNoEmptyOne(t *testing.T) {
	kept := types.Message{Role: "assistant", Meta: map[string]interface{}{types.MetaMessageID: "a"}}
	withStableID(&kept, "b")
	assert.Equal(t, "a", kept.ID())

	none := types.Message{Role: "assistant"}
	withStableID(&none, "")
	assert.Nil(t, none.Meta)

	set := types.Message{Role: "assistant", Meta: map[string]interface{}{"k": "v"}}
	withStableID(&set, "c")
	assert.Equal(t, map[string]interface{}{"k": "v", types.MetaMessageID: "c"}, set.Meta)
}

// A stage with no cutUsageWait waits the default hold for a cut stream's
// usage. A timer never fires early, so the wait is at least cutUsageHold.
func TestAwaitCutUsage_ZeroWaitHoldsForTheDefault(t *testing.T) {
	ps := NewProviderStage(nil, nil, nil, nil)
	stream := make(chan providers.StreamChunk)
	t.Cleanup(func() { close(stream) })
	start := time.Now()
	drained, late := ps.awaitCutUsage(context.Background(), stream, &streamAccumulator{})
	assert.GreaterOrEqual(t, time.Since(start), cutUsageHold)
	assert.True(t, drained, "the rest of the stream is read in the background")
	require.NotNil(t, late, "usage the stream sends later is still reported")
}

// A call refused as over the turn's budget did not run, so it does not count
// toward the repeated-call breaker: its one repeat is refused as over the
// budget again, not nudged.
func TestToolTurn_OverBudgetCallDoesNotCountTowardTheBreaker(t *testing.T) {
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(staticExecutor{})
	policy := &pipeline.ToolPolicy{MaxToolCallsPerTurn: 1, MaxIdenticalToolCalls: 2}
	r := &ToolRunner{Registry: reg, Policy: func() *pipeline.ToolPolicy { return policy }}
	turn := r.NewTurn(nil, 10)
	call := func(id, city string) types.MessageToolCall {
		return types.MessageToolCall{ID: id, Name: "weather", Args: json.RawMessage(`{"city":"` + city + `"}`)}
	}
	const overBudget = "Tool weather not executed: the tool call limit for this turn is reached"

	first := turn.RunRound(context.Background(), []types.MessageToolCall{call("c1", "Paris"), call("c2", "Rome")},
		RoundRef{Round: 1})
	require.Len(t, first.Results, 2)
	assert.Equal(t, "c1", first.Results[0].ToolResult.ID)
	assert.Empty(t, first.Results[0].ToolResult.Error)
	assert.Equal(t, overBudget, first.Results[1].ToolResult.Error)
	assert.False(t, first.Nudged)

	second := turn.RunRound(context.Background(), []types.MessageToolCall{call("c3", "Rome")}, RoundRef{Round: 2})
	require.Len(t, second.Results, 1)
	assert.Equal(t, "c3", second.Results[0].ToolResult.ID)
	assert.Equal(t, overBudget, second.Results[0].ToolResult.Error)
	assert.False(t, second.Nudged, "the over-budget call never ran, so its repeat is not a loop")
}

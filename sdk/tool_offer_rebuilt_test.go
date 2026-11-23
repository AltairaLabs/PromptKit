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

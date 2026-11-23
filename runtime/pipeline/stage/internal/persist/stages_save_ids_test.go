package persist

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A turn's messages are stored with a stable id each, forwarded carrying the
// same id, and a message that arrives with an id keeps it. The caller's
// message is not changed.
func TestIncrementalSaveStage_StoresStableMessageIDs(t *testing.T) {
	store := statestore.NewMemoryStore()
	t.Cleanup(store.Close)
	s := NewIncrementalSaveStage(&IncrementalSaveConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "conv-ids"},
	})

	user := types.Message{Role: "user", Content: "hi"}
	reply := types.Message{
		Role: "assistant", Content: "hello",
		Meta: map[string]interface{}{types.MetaMessageID: "client-given", "k": "v"},
	}
	results := runTestStage(t, s, []core.StreamElement{core.NewMessageElement(&user), core.NewMessageElement(&reply)})
	require.Len(t, results, 2)

	assert.Nil(t, user.Meta, "the caller's message is not changed")
	userID := results[0].Message.ID()
	assert.Regexp(t, `^msg_[0-9a-f-]{36}$`, userID)
	assert.Equal(t, "client-given", results[1].Message.ID())
	assert.Equal(t, "v", results[1].Message.Meta["k"])

	state, err := store.Load(context.Background(), "conv-ids")
	require.NoError(t, err)
	require.Len(t, state.Messages, 2)
	assert.Equal(t, []string{userID, "client-given"}, []string{state.Messages[0].ID(), state.Messages[1].ID()})
}

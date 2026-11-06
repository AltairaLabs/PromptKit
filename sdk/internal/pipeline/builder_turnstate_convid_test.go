package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
)

// A pipeline without a state store has no load stage to publish the
// conversation ID onto TurnState, so Build seeds it: the provider stage keys
// MaxCallsPerMinute on it, and the SDK admits approved calls under the same
// ID. Without the seed a store-less pipeline (a default Fork) counted under ""
// while approvals counted under the conversation's ID.
func TestBuild_SeedsTurnStateConversationIDWithoutStore(t *testing.T) {
	ts := stage.NewTurnState()
	_, err := BuildStreamPipeline(&Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
		ConversationID: "conv-fork",
		TurnState:      ts,
	})
	require.NoError(t, err)
	assert.Equal(t, "conv-fork", ts.ConversationID)
}

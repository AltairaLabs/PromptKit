package mock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The mock session records the history it is given, across calls, until it is
// closed; the provider declares support.
func TestMockStreamSession_SeedHistory(t *testing.T) {
	sess := NewMockStreamSession()
	require.NoError(t, sess.SeedHistory(context.Background(), []types.Message{{Role: "user", Content: "a"}}))
	require.NoError(t, sess.SeedHistory(context.Background(), []types.Message{{Role: "assistant", Content: "b"}}))
	assert.Equal(t, []providers.HistoryTurn{{Role: "user", Text: "a"}, {Role: "assistant", Text: "b"}}, sess.GetHistory())
	require.NoError(t, sess.Close())
	assert.Error(t, sess.SeedHistory(context.Background(), []types.Message{{Role: "user", Content: "c"}}))
	assert.Len(t, sess.GetHistory(), 2)
	assert.True(t, NewStreamingProvider("t", "m", false).SupportsStreamHistory())
}

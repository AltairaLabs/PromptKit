package gemini

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// SeedHistory sends the user and model messages with text as one
// client_content turn list with turn_complete false; nothing for no history.
func TestStreamSession_SeedHistoryFrame(t *testing.T) {
	url, msgs := recordLiveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := NewStreamSession(ctx, url, "k", &StreamSessionConfig{})
	require.NoError(t, err)
	defer s.Close()

	user := types.Message{Role: "user"}
	user.AddTextPart("my name is Ada")
	require.NoError(t, s.SeedHistory(ctx, nil), "no history sends nothing")
	require.NoError(t, s.SeedHistory(ctx, []types.Message{
		{Role: "system", Content: "ignored"},
		user,
		{Role: "assistant", Content: "hi Ada"},
		types.NewToolResultMessage(types.NewTextToolResult("c1", "lookup", "found")),
	}))
	require.NoError(t, s.SendText(ctx, "done"))

	var raw []string
	select {
	case raw = <-msgs:
	case <-ctx.Done():
		t.Fatal("server never saw the end marker")
	}
	require.Len(t, raw, 1, "exactly one history frame before the marker")
	assert.JSONEq(t, `{"client_content":{"turns":[
		{"role":"user","parts":[{"text":"my name is Ada"}]},
		{"role":"model","parts":[{"text":"hi Ada"}]}
	],"turn_complete":false}}`, raw[0])

	require.NoError(t, s.Close())
	assert.Error(t, s.SeedHistory(ctx, []types.Message{user}))
	assert.True(t, (&Provider{}).SupportsStreamHistory())
}

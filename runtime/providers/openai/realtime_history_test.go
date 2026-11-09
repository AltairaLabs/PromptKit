package openai

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SeedHistory sends each user and assistant message with text as a
// conversation.item.create, in order, with no response.create: user text as
// input_text, model text as output_text.
func TestRealtimeSession_SeedHistoryFrames(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newToolCaptureServer(t, received)
	defer srv.Close()
	session := openToolSession(t, srv)

	user := types.Message{Role: "user"}
	user.AddTextPart("my name is Ada")
	require.NoError(t, session.SeedHistory(context.Background(), []types.Message{
		{Role: "system", Content: "ignored"},
		user,
		{Role: "assistant", Content: "hi Ada"},
		types.NewToolResultMessage(types.NewTextToolResult("c1", "lookup", "")),
		{Role: "assistant", Content: ""},
	}))
	require.NoError(t, session.SendText(context.Background(), "marker"))

	type frame struct{ Type, Role, ContentType, Text string }
	var got []frame
	for {
		ev := recvEvent(t, received)
		item, _ := ev["item"].(map[string]any)
		var f frame
		f.Type, _ = ev["type"].(string)
		if item != nil {
			f.Role, _ = item["role"].(string)
			content, _ := item["content"].([]any)
			if len(content) == 1 {
				c := content[0].(map[string]any)
				f.ContentType, _ = c["type"].(string)
				f.Text, _ = c["text"].(string)
			}
		}
		if f.Text == "marker" {
			break
		}
		got = append(got, f)
	}
	assert.Equal(t, []frame{
		{Type: "conversation.item.create", Role: "user", ContentType: "input_text", Text: "my name is Ada"},
		{Type: "conversation.item.create", Role: "assistant", ContentType: "output_text", Text: "hi Ada"},
	}, got)
}

// A closed session refuses history, and the provider declares support.
func TestRealtimeSession_SeedHistoryClosedAndSupport(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newToolCaptureServer(t, received)
	defer srv.Close()
	session := openToolSession(t, srv)
	require.NoError(t, session.Close())
	assert.Error(t, session.SeedHistory(context.Background(), []types.Message{{Role: "user", Content: "x"}}))
	assert.True(t, (&Provider{}).SupportsStreamHistory())
}

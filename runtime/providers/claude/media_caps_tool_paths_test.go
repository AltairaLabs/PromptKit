package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The tool paths fail a request whose media cannot be loaded, including media
// over providers.MaxMediaItems, before any HTTP call; they used to send the
// message's text alone (#2468).
func TestToolPaths_MediaLoadErrorsFailBeforeTheCall(t *testing.T) {
	overCap := types.Message{Role: "user"}
	overCap.AddTextPart("describe these")
	for i := 0; i <= providers.MaxMediaItems; i++ {
		overCap.AddPart(types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil))
	}
	unloadable := &types.MessageToolResult{ID: "call-1", Name: "probe", Parts: []types.ContentPart{
		{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: types.MIMETypeImagePNG}},
	}}
	cases := map[string][]types.Message{
		"message over the item cap": {overCap},
		"unloadable tool result image": {
			{Role: "user", Content: "take a picture"},
			{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "call-1", Name: "probe", Args: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolResult: unloadable},
		},
	}
	for name, msgs := range cases {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", name, stream), func(t *testing.T) {
				var hits atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					hits.Add(1)
					_, _ = w.Write([]byte(`{}`))
				}))
				defer srv.Close()
				t.Setenv("ANTHROPIC_API_KEY", "k")
				tp := NewToolProvider("caps", "claude-sonnet-4-6", srv.URL, providers.ProviderDefaults{}, false)
				tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
					Name: "probe", Description: "probe", InputSchema: json.RawMessage(`{"type":"object"}`),
				}})
				require.NoError(t, err)
				req := providers.PredictionRequest{MaxTokens: 64, Messages: msgs}
				if stream {
					_, err = tp.PredictStreamWithTools(context.Background(), req, tools, "auto")
				} else {
					_, _, err = tp.PredictWithTools(context.Background(), req, tools, "auto")
				}
				require.Error(t, err)
				if name == "message over the item cap" {
					assert.ErrorIs(t, err, providers.ErrMediaItemLimitExceeded)
				}
				assert.Zero(t, hits.Load(), "a request went out without the message's media")
			})
		}
	}
}

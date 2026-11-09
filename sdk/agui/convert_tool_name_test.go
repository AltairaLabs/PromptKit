package agui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
)

// aguiToolExchange is an AG-UI history in which the assistant called
// get_weather and the client answered it.
func aguiToolExchange() []aguitypes.Message {
	return []aguitypes.Message{
		{ID: "u1", Role: aguitypes.RoleUser, Content: "weather?"},
		{ID: "a1", Role: aguitypes.RoleAssistant, ToolCalls: []aguitypes.ToolCall{{
			ID: "call-1", Type: "function",
			Function: aguitypes.FunctionCall{Name: "get_weather", Arguments: `{"city":"Paris"}`},
		}}},
		{ID: "t1", Role: aguitypes.RoleTool, ToolCallID: "call-1", Content: `{"temp":21}`},
	}
}

// An AG-UI tool message carries only its call ID. The converted tool result
// must carry the name of the call it answers, or a provider that keys
// results by name (Gemini's functionResponse.name) gets an invalid request.
func TestMessagesFromAGUI_ToolResultNamedFromItsCall(t *testing.T) {
	msgs := MessagesFromAGUI(aguiToolExchange())
	require.Len(t, msgs, 3)
	require.NotNil(t, msgs[2].ToolResult)
	assert.Equal(t, "call-1", msgs[2].ToolResult.ID)
	assert.Equal(t, "get_weather", msgs[2].ToolResult.Name)
}

// The same history replayed to Gemini sends a functionResponse named after
// the call.
func TestMessagesFromAGUI_GeminiRequestNamesTheFunctionResponse(t *testing.T) {
	var mu sync.Mutex
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GEMINI_API_KEY", "test-key")

	p := gemini.NewToolProvider("gemini", "gemini-2.0-flash", srv.URL, providers.ProviderDefaults{}, false)
	tooling, err := p.BuildTooling([]*providers.ToolDescriptor{{
		Name: "get_weather", Description: "weather", InputSchema: json.RawMessage(`{"type":"object"}`),
	}})
	require.NoError(t, err)
	_, _, err = p.PredictWithTools(context.Background(),
		providers.PredictionRequest{Messages: MessagesFromAGUI(aguiToolExchange())}, tooling, "auto")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	var req struct {
		Contents []struct {
			Parts []struct {
				FunctionResponse *struct {
					Name string `json:"name"`
				} `json:"functionResponse"`
			} `json:"parts"`
		} `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(body, &req), "body: %s", body)
	var names []string
	for _, c := range req.Contents {
		for _, part := range c.Parts {
			if part.FunctionResponse != nil {
				names = append(names, part.FunctionResponse.Name)
			}
		}
	}
	assert.Equal(t, []string{"get_weather"}, names)
}

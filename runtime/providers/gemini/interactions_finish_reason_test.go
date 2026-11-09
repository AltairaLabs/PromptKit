package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Every provider reports finish_reason in the canonical vocabulary
// (types.FinishReason*), and the generateContent path already does. The
// Interactions path must too, unary and streaming, or the same turn reports
// "tool_use" or "tool_calls" or nothing depending on which Gemini API mode
// the request was routed through.
func TestInteractions_FinishReasonIsCanonical(t *testing.T) {
	toolTurn := `{"id":"i1","status":"requires_action","steps":[
	  {"type":"function_call","id":"c1","name":"probe","arguments":{"q":"x"}}],
	  "usage":{"total_input_tokens":10,"total_output_tokens":4}}`
	textTurn := `{"id":"i2","status":"completed","steps":[
	  {"type":"model_output","content":[{"type":"text","text":"done"}]}],
	  "usage":{"total_input_tokens":10,"total_output_tokens":4}}`

	for _, tc := range []struct {
		name, body, want string
	}{
		{"tool call", toolTurn, types.FinishReasonToolUse},
		{"answer", textTurn, types.FinishReasonStop},
	} {
		t.Run("unary/"+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			resp, _, err := interactionsTestProvider(t, srv.URL).predictWithInteractions(context.Background(),
				providers.PredictionRequest{Messages: []types.Message{{Role: roleUser, Content: "hi"}}}, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, resp.FinishReason)
		})
	}

	t.Run("stream/tool call", func(t *testing.T) {
		body := sse(
			`event: step.start`,
			`data: {"index":0,"step":{"id":"call_7","type":"function_call","name":"get_temperature","arguments":{}}}`,
			``,
			`event: step.delta`,
			`data: {"index":0,"delta":{"type":"arguments_delta","arguments":"{\"city\":\"Bristol\"}"}}`,
			``,
			`event: interaction.completed`,
			`data: {"interaction":{"id":"i1","status":"requires_action"}}`,
			``,
		)
		chunks := streamFromServer(t, body, textRequest())
		final := chunks[len(chunks)-1]
		require.NotNil(t, final.FinishReason)
		assert.Equal(t, types.FinishReasonToolUse, *final.FinishReason)
	})
}

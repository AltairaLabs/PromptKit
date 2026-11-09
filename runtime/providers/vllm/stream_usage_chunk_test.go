package vllm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// streamTerminal runs one stream (tool or plain) against body and returns the
// chunks with a finish reason, and every error.
func streamTerminal(t *testing.T, body string, withTools bool) (terminals []providers.StreamChunk, errs []error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	p := NewProvider("v", "m", srv.URL, providers.ProviderDefaults{MaxTokens: 64}, false, nil)
	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	var ch <-chan providers.StreamChunk
	var err error
	if withTools {
		tools, buildErr := p.BuildTooling([]*providers.ToolDescriptor{{
			Name: "f", Description: "f", InputSchema: json.RawMessage(`{"type":"object"}`),
		}})
		require.NoError(t, buildErr)
		ch, err = p.PredictStreamWithTools(context.Background(), req, tools, "auto")
	} else {
		ch, err = p.PredictStream(context.Background(), req)
	}
	require.NoError(t, err)
	for c := range ch {
		if c.Error != nil {
			errs = append(errs, c.Error)
		}
		if c.FinishReason != nil {
			terminals = append(terminals, c)
		}
	}
	return terminals, errs
}

// A server that ignores stream_options.include_usage sends no usage chunk.
// The stream still ends with exactly one terminal chunk carrying the finish
// reason and content, with no cost, whether [DONE] arrives or the body just
// ends after the finish chunk.
func TestStream_FinishWithoutUsageChunkStillTerminates(t *testing.T) {
	const finish = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"
	for _, tc := range []struct {
		name, body string
	}{
		{"done marker", finish + "data: [DONE]\n\n"},
		{"body ends", finish},
	} {
		for _, withTools := range []bool{false, true} {
			name := tc.name + "/plain"
			if withTools {
				name = tc.name + "/tools"
			}
			t.Run(name, func(t *testing.T) {
				terminals, errs := streamTerminal(t, tc.body, withTools)
				require.Empty(t, errs)
				require.Len(t, terminals, 1, "exactly one terminal chunk")
				assert.Equal(t, types.FinishReasonStop, *terminals[0].FinishReason)
				assert.Equal(t, "ok", terminals[0].Content)
				assert.Nil(t, terminals[0].CostInfo, "no usage was reported")
			})
		}
	}
}

// With a usage-only chunk, the terminal chunk is sent once, carrying the cost,
// and the [DONE] after it does not send a second one.
func TestStream_UsageChunkCompletesTheTerminalOnce(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\n" +
		"data: [DONE]\n\n"
	for _, withTools := range []bool{false, true} {
		terminals, errs := streamTerminal(t, body, withTools)
		require.Empty(t, errs)
		require.Len(t, terminals, 1, "tools=%v", withTools)
		require.NotNil(t, terminals[0].CostInfo)
		assert.Equal(t, []int{7, 2}, []int{terminals[0].CostInfo.InputTokens, terminals[0].CostInfo.OutputTokens})
	}
}

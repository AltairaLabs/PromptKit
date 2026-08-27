package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestPredictStreamWithResponses_CarriesReasoningSummary covers a gap that only
// a live call exposed: o-series models do NOT emit
// response.reasoning_summary_text.delta. The summary arrives on the reasoning
// output item, in response.output_item.done.
//
// Because the pipeline streams, listening for the delta alone meant every
// o-series turn silently lost its reasoning — no error, no partial trace,
// nothing. The non-streaming parse was correct throughout, which is exactly why
// a canned-body conformance test could not catch it.
//
// Wire shape below is copied from a real o4-mini stream.
func TestPredictStreamWithResponses_CarriesReasoningSummary(t *testing.T) {
	const thought = "**Solving the bat and ball problem**\n\nThe bat is $1 more, so the ball is $0.05."

	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		// The reasoning item is announced with an EMPTY summary...
		`data: {"type":"response.output_item.added","output_index":0,` +
			`"item":{"type":"reasoning","id":"rs_1","summary":[]}}`,
		// ...and filled in when the item completes. This is the only place the
		// text appears for models that send no summary deltas.
		`data: {"type":"response.output_item.done","output_index":0,` +
			`"item":{"type":"reasoning","id":"rs_1","summary":[` +
			fmt.Sprintf(`{"type":"summary_text","text":%q}`, thought) + `]}}`,
		`data: {"type":"response.output_text.delta","delta":"The ball costs $0.05."}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","output":[],` +
			`"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`,
		"data: [DONE]",
		"",
	}, "\n\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer server.Close()

	provider := &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
		model:        "o4-mini",
		baseURL:      server.URL,
		apiKey:       "test-key",
		apiMode:      APIModeResponses,
		defaults:     providers.ProviderDefaults{MaxTokens: 100},
	}

	ch, err := provider.predictStreamWithResponses(context.Background(),
		providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "bat and ball?"}},
		}, nil, "")
	require.NoError(t, err)

	var reasoning, content strings.Builder
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		reasoning.WriteString(chunk.Reasoning)
		if chunk.Delta != "" {
			content.WriteString(chunk.Delta)
		}
	}

	require.NotEmpty(t, reasoning.String(),
		"streaming Responses dropped the reasoning summary; o-series models send it on "+
			"response.output_item.done, not as a summary_text delta")
	assert.Contains(t, reasoning.String(), "bat and ball problem")

	// Reasoning must never contaminate spoken content.
	assert.Equal(t, "The ball costs $0.05.", content.String())
	assert.NotContains(t, content.String(), "bat and ball problem")
}

// TestPredictStreamWithResponses_NoReasoning_EmitsNone keeps the absent case
// distinguishable: a stream with no reasoning item must yield no reasoning
// rather than an empty-but-present trace.
func TestPredictStreamWithResponses_NoReasoning_EmitsNone(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_2"}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp_2","output":[],` +
			`"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		"data: [DONE]",
		"",
	}, "\n\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer server.Close()

	provider := &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
		model:        "o4-mini",
		baseURL:      server.URL,
		apiKey:       "test-key",
		apiMode:      APIModeResponses,
		defaults:     providers.ProviderDefaults{MaxTokens: 100},
	}

	ch, err := provider.predictStreamWithResponses(context.Background(),
		providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "hi"}},
		}, nil, "")
	require.NoError(t, err)

	var reasoning strings.Builder
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		reasoning.WriteString(chunk.Reasoning)
	}
	assert.Empty(t, reasoning.String(), "emitted reasoning for a stream that contained none")
}

// TestHandleOutputDone_IgnoresNonReasoningAndMalformed covers the guards on the
// output-item handler. Each exists so a stream that is not carrying reasoning
// cannot produce a spurious reasoning chunk — which downstream would become a
// reasoning.completed event for a round that never reasoned.
func TestHandleOutputDone_IgnoresNonReasoningAndMalformed(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{
			name: "malformed json",
			data: `{"item":`,
		},
		{
			name: "not a reasoning item",
			data: `{"item":{"type":"function_call","id":"fc_1","call_id":"c1","name":"probe"}}`,
		},
		{
			name: "reasoning item with empty summary",
			data: `{"item":{"type":"reasoning","id":"rs_1","summary":[]}}`,
		},
		{
			name: "reasoning item with blank summary text",
			data: `{"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":""}]}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Provider{}
			// Buffered so a wrongful send does not deadlock; the assertion is
			// that nothing was sent at all.
			out := make(chan providers.StreamChunk, 4)
			p.handleOutputDone(tc.data, out)
			close(out)

			var got []providers.StreamChunk
			for c := range out {
				got = append(got, c)
			}
			assert.Emptyf(t, got, "%s produced a reasoning chunk; want none", tc.name)
		})
	}
}

// TestHandleOutputDone_ConcatenatesMultipleSummaryEntries verifies the whole
// summary is emitted, not just its first entry — a live o4-mini response
// returned two entries for one reasoning item.
func TestHandleOutputDone_ConcatenatesMultipleSummaryEntries(t *testing.T) {
	p := &Provider{}
	out := make(chan providers.StreamChunk, 4)
	p.handleOutputDone(`{"item":{"type":"reasoning","id":"rs_1","summary":[`+
		`{"type":"summary_text","text":"First part. "},`+
		`{"type":"summary_text","text":"Second part."}]}}`, out)
	close(out)

	var sb strings.Builder
	for c := range out {
		sb.WriteString(c.Reasoning)
	}
	assert.Equal(t, "First part. Second part.", sb.String())
}

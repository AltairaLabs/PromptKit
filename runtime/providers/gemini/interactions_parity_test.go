package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Payloads captured from the live Interactions API (gemini-3.7-flash,
// max_output_tokens 40). A reply cut off by the output cap comes back with
// status "incomplete", unary and as the interaction.completed event of a
// stream; the API has no other field that says why.
const (
	incompleteUnaryBody = `{"id":"i1","status":"incomplete","object":"interaction","steps":[
	  {"type":"thought","signature":"SIG"},
	  {"type":"model_output","content":[{"type":"text","text":"The lighthouse stood"}]}],
	  "usage":{"total_input_tokens":13,"total_output_tokens":36}}`
	incompleteStreamBody = "event: interaction.created\n" +
		`data: {"interaction":{"id":"i1","object":"interaction"},"event_type":"interaction.created"}` + "\n\n" +
		"event: step.start\n" +
		`data: {"index":0,"step":{"type":"model_output"},"event_type":"step.start"}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":0,"delta":{"type":"text","text":"The lighthouse stood"},"event_type":"step.delta"}` + "\n\n" +
		"event: interaction.completed\n" +
		`data: {"interaction":{"id":"i1","status":"incomplete","usage":{"total_input_tokens":13,` +
		`"total_output_tokens":36}},"event_type":"interaction.completed"}` + "\n\n" +
		"event: done\n"
)

// interactionsParityServer answers the Interactions endpoint with a fixed
// unary body or SSE stream, and records the last request body.
type interactionsParityServer struct {
	url      string
	lastBody []byte
}

func newInteractionsParityServer(t *testing.T, unary, stream string) *interactionsParityServer {
	t.Helper()
	s := &interactionsParityServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.lastBody = body
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, stream)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, unary)
	}))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

// parityProviderSeq makes provider IDs unique per run: the unsent-parameter
// warnings are logged once per provider ID and process, so a reused ID would
// stay silent under -count=N.
var parityProviderSeq atomic.Int64

func uniqueProviderID(prefix string) string {
	return prefix + "-" + strconv.FormatInt(parityProviderSeq.Add(1), 10)
}

// generationConfig returns the last request's generation_config, canonicalized.
func (s *interactionsParityServer) generationConfig(t *testing.T) string {
	t.Helper()
	var body struct {
		Generation map[string]any `json:"generation_config"`
	}
	if err := json.Unmarshal(s.lastBody, &body); err != nil {
		t.Fatalf("interactions body not JSON: %v (%s)", err, s.lastBody)
	}
	return canonicalJSON(t, body.Generation)
}

// newInteractionsParityProvider builds an api_mode interactions provider from
// a spec, as a host does, with extra additional_config on top.
func newInteractionsParityProvider(
	t *testing.T, id, url string, extra map[string]any,
) *ToolProvider {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "test-key")
	cfg := map[string]any{"api_mode": "interactions"}
	for k, v := range extra {
		cfg[k] = v
	}
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: id, Type: "gemini", Model: "gemini-3.7-flash", BaseURL: url, AdditionalConfig: cfg,
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	return p.(*ToolProvider)
}

// runInteractionsTurn runs one turn, unary or streamed, and returns the
// response (for a stream: the final chunk's content, reasoning and finish
// reason, with Reasoning the concatenation of every chunk's reasoning).
func runInteractionsTurn(
	t *testing.T, tp *ToolProvider, req providers.PredictionRequest, stream bool,
) providers.PredictionResponse {
	t.Helper()
	if req.Messages == nil {
		req.Messages = []types.Message{{Role: roleUser, Content: "Write a story about a lighthouse."}}
	}
	if !stream {
		resp, _, err := tp.PredictWithTools(context.Background(), req, nil, "")
		if err != nil {
			t.Fatalf("PredictWithTools: %v", err)
		}
		return resp
	}
	ch, err := tp.PredictStreamWithTools(context.Background(), req, nil, "")
	if err != nil {
		t.Fatalf("PredictStreamWithTools: %v", err)
	}
	var out providers.PredictionResponse
	var reasoning strings.Builder
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("stream chunk error: %v", c.Error)
		}
		reasoning.WriteString(c.Reasoning)
		out.Content = c.Content
		if c.FinishReason != nil {
			out.FinishReason = *c.FinishReason
		}
	}
	if reasoning.Len() > 0 {
		out.Reasoning = &types.ReasoningTrace{Text: reasoning.String()}
	}
	return out
}

// A reply the output cap cut off reports max_output_tokens, the reason
// generateContent reports for MAX_TOKENS, unary and streaming. It used to
// report stop, so a caller could not tell the reply was truncated.
func TestInteractions_IncompleteReportsMaxOutputTokens(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := map[bool]string{false: "unary", true: "stream"}[stream]
		t.Run(name, func(t *testing.T) {
			srv := newInteractionsParityServer(t, incompleteUnaryBody, incompleteStreamBody)
			tp := newInteractionsParityProvider(t, "int-incomplete-"+name, srv.url, nil)
			resp := runInteractionsTurn(t, tp, providers.PredictionRequest{MaxTokens: 40}, stream)
			if resp.FinishReason != normalizeFinishReason(finishReasonMaxTokens) {
				t.Errorf("finish reason = %q, want %q", resp.FinishReason, types.FinishReasonMaxOutputTokens)
			}
			if resp.Content != "The lighthouse stood" {
				t.Errorf("content = %q, want the truncated text kept", resp.Content)
			}
		})
	}
}

// The Interactions API has no top_k, presence_penalty or frequency_penalty in
// generation_config, and answers each with HTTP 400 "Unknown parameter"
// (probed live on gemini-3.7-flash and gemini-2.5-flash). They are not sent,
// and a request that sets them logs the warning generateContent gives for a
// parameter it does not send.
func TestInteractions_UnsentSamplingParamsWarn(t *testing.T) {
	topK := 5
	penalty := float32(0.5)
	for _, stream := range []bool{false, true} {
		name := map[bool]string{false: "unary", true: "stream"}[stream]
		t.Run(name, func(t *testing.T) {
			logs := captureWarnings(t)
			srv := newInteractionsParityServer(t, incompleteUnaryBody, incompleteStreamBody)
			id := uniqueProviderID("int-unsent-" + name)
			tp := newInteractionsParityProvider(t, id, srv.url, nil)
			runInteractionsTurn(t, tp, providers.PredictionRequest{
				TopK: &topK, FrequencyPenalty: &penalty, PresencePenalty: &penalty,
			}, stream)

			if got, want := srv.generationConfig(t), `{"temperature":0}`; got != want {
				t.Errorf("generation_config = %s, want %s", got, want)
			}
			for _, param := range []string{"top_k", "frequency_penalty", "presence_penalty"} {
				line := "provider=" + id + " param=" + param
				if !strings.Contains(logs.String(), line) {
					t.Errorf("no unsent-param warning %q in %q", line, logs.String())
				}
			}
		})
	}
}

// The provider's thinking config reaches generation_config: thinking_level as
// the API's thinking_level, include_thoughts as thinking_summaries "auto"
// (without it the API returns no summary). thinking_budget has no field here
// (HTTP 400 "Unknown parameter 'thinking_budget'"), so it is not sent and a
// warning says so. With a level set the level wins and the budget is dropped,
// as on generateContent.
func TestInteractions_ThinkingConfigReachesRequest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		config     map[string]any
		want       string
		warnBudget bool
	}{
		{"level", map[string]any{"thinking_level": "Medium"},
			`{"temperature":0,"thinking_level":"medium"}`, false},
		{"level with thoughts", map[string]any{"thinking_level": "high", "include_thoughts": true},
			`{"temperature":0,"thinking_level":"high","thinking_summaries":"auto"}`, false},
		{"level and budget", map[string]any{"thinking_level": "low", "thinking_budget": 512},
			`{"temperature":0,"thinking_level":"low"}`, false},
		{"budget only", map[string]any{"thinking_budget": 512},
			`{"temperature":0}`, true},
		{"none", nil, `{"temperature":0}`, false},
	} {
		for _, stream := range []bool{false, true} {
			name := tc.name + map[bool]string{false: "/unary", true: "/stream"}[stream]
			t.Run(name, func(t *testing.T) {
				logs := captureWarnings(t)
				srv := newInteractionsParityServer(t, incompleteUnaryBody, incompleteStreamBody)
				id := uniqueProviderID("int-thinking-" + strings.ReplaceAll(name, " ", "-"))
				tp := newInteractionsParityProvider(t, id, srv.url, tc.config)
				runInteractionsTurn(t, tp, providers.PredictionRequest{}, stream)

				if got := srv.generationConfig(t); got != tc.want {
					t.Errorf("generation_config = %s, want %s", got, tc.want)
				}
				warned := strings.Contains(logs.String(), "thinking_budget") &&
					strings.Contains(logs.String(), "provider="+id)
				if warned != tc.warnBudget {
					t.Errorf("thinking_budget warning logged = %v, want %v (logs %q)", warned, tc.warnBudget, logs.String())
				}
			})
		}
	}
}

// With thinking_summaries "auto" the API returns the thought summary: unary
// as the thought step's summary list, streamed as thought_summary deltas
// (shapes captured live). It is the turn's reasoning text, as a thought part's
// text is on generateContent.
func TestInteractions_ThoughtSummaryIsReasoning(t *testing.T) {
	unary := `{"id":"i1","status":"completed","steps":[
	  {"type":"thought","signature":"SIG","summary":[{"type":"text","text":"**Plan**\n\n"},{"type":"text","text":"Keep it short."}]},
	  {"type":"model_output","content":[{"type":"text","text":"Done."}]}]}`
	stream := "event: step.start\n" +
		`data: {"index":0,"step":{"type":"thought"},"event_type":"step.start"}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":0,"delta":{"content":{"text":"**Plan**\n\n"},"type":"thought_summary"},` +
		`"event_type":"step.delta"}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":0,"delta":{"content":{"text":"Keep it short."},"type":"thought_summary"},` +
		`"event_type":"step.delta"}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":0,"delta":{"signature":"SIG","type":"thought_signature"},"event_type":"step.delta"}` +
		"\n\n" +
		"event: step.start\n" +
		`data: {"index":1,"step":{"type":"model_output"},"event_type":"step.start"}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":1,"delta":{"type":"text","text":"Done."},"event_type":"step.delta"}` + "\n\n" +
		"event: interaction.completed\n" +
		`data: {"interaction":{"id":"i1","status":"completed"},"event_type":"interaction.completed"}` + "\n\n"

	for _, isStream := range []bool{false, true} {
		name := map[bool]string{false: "unary", true: "stream"}[isStream]
		t.Run(name, func(t *testing.T) {
			srv := newInteractionsParityServer(t, unary, stream)
			tp := newInteractionsParityProvider(t, "int-summary-"+name, srv.url,
				map[string]any{"thinking_level": "high", "include_thoughts": true})
			resp := runInteractionsTurn(t, tp, providers.PredictionRequest{}, isStream)
			if resp.Reasoning == nil || resp.Reasoning.Text != "**Plan**\n\nKeep it short." {
				t.Errorf("reasoning = %+v, want the thought summary", resp.Reasoning)
			}
			if resp.Content != "Done." {
				t.Errorf("content = %q, want %q", resp.Content, "Done.")
			}
			if resp.FinishReason != types.FinishReasonStop {
				t.Errorf("finish reason = %q, want %q", resp.FinishReason, types.FinishReasonStop)
			}
		})
	}
}

// An Interactions turn is priced from the whole usage block, as a
// generateContent turn is from usageMetadata. Captured live on
// gemini-3.7-flash (the second of two identical ~14k-token prompts):
// total_input_tokens is cache-INCLUSIVE, like promptTokenCount, and thought
// tokens are reported apart from total_output_tokens (total_tokens = input +
// output + thought). The cost used to read input and output only, so thinking
// was free and cache reads were billed as full-price input.
func TestInteractions_CostPricesThoughtAndCachedTokens(t *testing.T) {
	usage := `"usage":{"total_tokens":14508,"total_input_tokens":14486,"total_cached_tokens":12435,` +
		`"total_output_tokens":2,"total_thought_tokens":20,"total_tool_use_tokens":0}`
	unary := `{"id":"i1","status":"completed","steps":[
	  {"type":"model_output","content":[{"type":"text","text":"OK"}]}],` + usage + `}`
	stream := "event: step.start\n" +
		`data: {"index":0,"step":{"type":"model_output"}}` + "\n\n" +
		"event: step.delta\n" +
		`data: {"index":0,"delta":{"type":"text","text":"OK"}}` + "\n\n" +
		"event: interaction.completed\n" +
		`data: {"interaction":{"id":"i1","status":"completed",` + usage + `}}` + "\n\n"

	for _, isStream := range []bool{false, true} {
		name := map[bool]string{false: "unary", true: "stream"}[isStream]
		t.Run(name, func(t *testing.T) {
			srv := newInteractionsParityServer(t, unary, stream)
			tp := newInteractionsParityProvider(t, "int-cost-"+name, srv.url, nil)
			req := providers.PredictionRequest{Messages: []types.Message{{Role: roleUser, Content: "hi"}}}

			var cost *types.CostInfo
			if isStream {
				ch, err := tp.PredictStreamWithTools(context.Background(), req, nil, "")
				if err != nil {
					t.Fatalf("PredictStreamWithTools: %v", err)
				}
				for c := range ch {
					if c.CostInfo != nil {
						cost = c.CostInfo
					}
				}
			} else {
				resp, _, err := tp.PredictWithTools(context.Background(), req, nil, "")
				if err != nil {
					t.Fatalf("PredictWithTools: %v", err)
				}
				cost = resp.CostInfo
			}
			if cost == nil {
				t.Fatal("no cost info")
			}
			got := map[string]any{
				"input": cost.InputTokens, "cached": cost.CachedTokens, "output": cost.OutputTokens,
				"reasoning": cost.Quantities[base.UnitReasoningToken],
			}
			want := map[string]any{"input": 2051, "cached": 12435, "output": 2, "reasoning": 20.0}
			if canonicalJSON(t, got) != canonicalJSON(t, want) {
				t.Errorf("cost = %s, want %s", canonicalJSON(t, got), canonicalJSON(t, want))
			}
		})
	}
}

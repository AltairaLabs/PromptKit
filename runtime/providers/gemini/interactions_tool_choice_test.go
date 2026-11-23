package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// interactionsChoiceServer answers the Interactions endpoint (unary JSON or
// SSE when the request asks to stream) and records the last request body.
type interactionsChoiceServer struct {
	url      string
	lastBody []byte
}

func newInteractionsChoiceServer(t *testing.T) *interactionsChoiceServer {
	t.Helper()
	s := &interactionsChoiceServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, interactionsPath) {
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		s.lastBody = body
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, sse(
				`event: step.start`,
				`data: {"index":0,"step":{"id":"call_1","type":"function_call","name":"get_status","arguments":{}}}`,
				``,
				`event: interaction.completed`,
				`data: {"interaction":{"id":"i1","status":"requires_action"}}`,
				``,
			))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"i1","status":"requires_action","steps":[
		  {"type":"function_call","id":"c1","name":"get_status","arguments":{}}],
		  "usage":{"total_input_tokens":10,"total_output_tokens":4}}`)
	}))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

// toolChoiceJSON returns generation_config.tool_choice from the last request,
// canonicalized, or "" when the request carried none.
func (s *interactionsChoiceServer) toolChoiceJSON(t *testing.T) string {
	t.Helper()
	var body struct {
		Generation map[string]any `json:"generation_config"`
	}
	if err := json.Unmarshal(s.lastBody, &body); err != nil {
		t.Fatalf("interactions body not JSON: %v (%s)", err, s.lastBody)
	}
	choice, ok := body.Generation["tool_choice"]
	if !ok {
		return ""
	}
	return canonicalJSON(t, choice)
}

func newInteractionsChoiceProvider(t *testing.T, url string) (*ToolProvider, providers.ProviderTools) {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "test-gemini-int", Type: "gemini", Model: "gemini-2.5-flash", BaseURL: url,
		AdditionalConfig: map[string]any{"api_mode": "interactions"},
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	tp := p.(*ToolProvider)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "get_weather", Description: "weather", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "get_status", Description: "status", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	if err != nil {
		t.Fatalf("BuildTooling: %v", err)
	}
	return tp, tools
}

// The Interactions API takes the tool choice as generation_config.tool_choice:
// a mode string, or allowed_tools{mode, tools} to restrict the call to named
// tools. With api_mode interactions the choice used to be dropped, so
// required, none and a named tool all behaved as auto.
func TestInteractions_ToolChoiceReachesRequest(t *testing.T) {
	cases := []struct {
		choice string
		want   string
	}{
		{"", ""},
		{"auto", ""},
		{"required", `"any"`},
		{"any", `"any"`},
		{"none", `"none"`},
		{"get_status", `{"allowed_tools":{"mode":"any","tools":["get_status"]}}`},
	}
	req := providers.PredictionRequest{Messages: []types.Message{{Role: roleUser, Content: "hi"}}}

	for _, tc := range cases {
		t.Run("unary/choice="+tc.choice, func(t *testing.T) {
			srv := newInteractionsChoiceServer(t)
			tp, tools := newInteractionsChoiceProvider(t, srv.url)
			if _, _, err := tp.PredictWithTools(context.Background(), req, tools, tc.choice); err != nil {
				t.Fatalf("PredictWithTools: %v", err)
			}
			if got := srv.toolChoiceJSON(t); got != tc.want {
				t.Errorf("tool_choice = %s, want %s", got, tc.want)
			}
		})
		t.Run("stream/choice="+tc.choice, func(t *testing.T) {
			srv := newInteractionsChoiceServer(t)
			tp, tools := newInteractionsChoiceProvider(t, srv.url)
			ch, err := tp.PredictStreamWithTools(context.Background(), req, tools, tc.choice)
			if err != nil {
				t.Fatalf("PredictStreamWithTools: %v", err)
			}
			for range ch { //nolint:revive // drain the stream so the request completes
			}
			if got := srv.toolChoiceJSON(t); got != tc.want {
				t.Errorf("tool_choice = %s, want %s", got, tc.want)
			}
		})
	}
}

// A round that offers no tools (the schema-only answer round, for one) carries
// no tool_choice even when the caller's choice forces one: there is nothing
// to choose from.
func TestInteractions_ToolChoiceOmittedWithoutTools(t *testing.T) {
	srv := newInteractionsChoiceServer(t)
	tp, _ := newInteractionsChoiceProvider(t, srv.url)
	if _, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "hi"}},
	}, nil, "required"); err != nil {
		t.Fatalf("PredictWithTools: %v", err)
	}
	if got := srv.toolChoiceJSON(t); got != "" {
		t.Errorf("tool_choice = %s, want none sent", got)
	}
}

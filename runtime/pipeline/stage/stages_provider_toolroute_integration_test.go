package stage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
)

// TestProviderStage_ToolHistorySurvivesEmptyToolSet_Live pins issue #1735 at the
// wire level. The unit tests assert which provider entry point the stage picks;
// this asserts what that choice actually does to the request, because the damage
// happens inside the provider's serializer rather than in the stage.
//
// It sends the stage's route (a nil tool set) to the live API through a
// recording proxy, and asserts the request carries tool_calls on the assistant
// message and tool_call_id on the tool message, and that OpenAI accepts it.
// The tool-blind serializer drops both fields; OpenAI once rejected that and
// now tolerates it, so only the request body can show the route regressed.
//
// Skips without OPENAI_API_KEY; hits the paid API when it runs.
func TestProviderStage_ToolHistorySurvivesEmptyToolSet_Live(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("no OPENAI_API_KEY, skipping live tool-routing integration test")
	}

	// A reverse proxy to the real API records the request body the provider
	// sends, so the test can assert what the stage's route puts on the wire.
	target, err := url.Parse("https://api.openai.com")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var bodies [][]byte
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		if body, readErr := io.ReadAll(r.Body); readErr == nil {
			mu.Lock()
			bodies = append(bodies, body)
			mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		director(r)
		r.Host = target.Host
	}
	srv := httptest.NewServer(proxy)
	defer srv.Close()

	provider := openai.NewToolProvider(
		"openai", "gpt-4o-mini", srv.URL+"/v1",
		providers.ProviderDefaults{MaxTokens: 64}, false, nil, nil,
	)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{MaxTokens: 64})
	req := providers.PredictionRequest{Messages: historyWithToolLinkage(), MaxTokens: 64}

	// The stage's route, with an empty tool set, must keep the tool linkage and
	// be accepted.
	ch, err := stage.startStreamingRequest(context.Background(), req, nil, "")
	if err != nil {
		t.Fatalf("tool-aware path rejected tool history with an empty tool set: %v", err)
	}
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("tool-aware path rejected tool history with an empty tool set: %v", c.Error)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("expected one request to the API, got %d", len(bodies))
	}
	var sent struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID string `json:"id"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(bodies[0], &sent); err != nil {
		t.Fatal(err)
	}
	var callIDs, resultIDs []string
	for _, m := range sent.Messages {
		for _, tc := range m.ToolCalls {
			callIDs = append(callIDs, tc.ID)
		}
		if m.Role == "tool" {
			resultIDs = append(resultIDs, m.ToolCallID)
		}
	}
	if !reflect.DeepEqual(callIDs, []string{"call_A"}) || !reflect.DeepEqual(resultIDs, []string{"call_A"}) {
		t.Errorf("the request lost the tool linkage: assistant tool_calls %v, tool_call_id %v (want call_A for both)",
			callIDs, resultIDs)
	}
}

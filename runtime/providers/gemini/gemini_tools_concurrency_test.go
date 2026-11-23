package gemini

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// One provider serves every conversation of a host, so its tool calls run
// concurrently. PredictWithTools used to store each call's tools and request
// on the shared provider, a data race the race detector reports here.
func TestPredictWithTools_ConcurrentCallsOnOneProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"OK"}]},`+
			`"finishReason":"STOP"}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GEMINI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "gemini-concurrent", Type: "gemini", Model: "gemini-3.7-flash", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	tp := p.(*ToolProvider)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{Name: "probe", Description: "p"}})
	if err != nil {
		t.Fatalf("BuildTooling: %v", err)
	}

	const calls = 8
	contents := make([]string, calls)
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, _, callErr := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
				Messages: []types.Message{{Role: roleUser, Content: "hi"}},
			}, tools, "")
			if callErr != nil {
				contents[i] = callErr.Error()
				return
			}
			contents[i] = resp.Content
		}()
	}
	wg.Wait()
	for i, c := range contents {
		if c != "OK" {
			t.Errorf("call %d: content = %q, want %q", i, c, "OK")
		}
	}
}

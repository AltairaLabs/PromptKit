package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSampleArgs(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{
		"a":{"type":"number"},"n":{"type":"integer"},"ok":{"type":"boolean"},
		"xs":{"type":"array"},"o":{"type":"object"},"s":{"type":"string"}}}`)

	var got map[string]any
	if err := json.Unmarshal(sampleArgs(schema), &got); err != nil {
		t.Fatalf("sampleArgs produced invalid JSON: %v", err)
	}
	want := map[string]any{
		"a": float64(1), "n": float64(1), "ok": true,
		"xs": []any{}, "o": map[string]any{}, "s": sampleString,
	}
	if len(got) != len(want) {
		t.Fatalf("sampleArgs = %v, want %v", got, want)
	}
	for k, v := range want {
		gotJSON, _ := json.Marshal(got[k])
		wantJSON, _ := json.Marshal(v)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("arg %q = %s, want %s", k, gotJSON, wantJSON)
		}
	}
}

func TestSampleArgs_InvalidSchema(t *testing.T) {
	if got := string(sampleArgs(json.RawMessage(`not json`))); got != "{}" {
		t.Errorf("sampleArgs(invalid) = %s, want {}", got)
	}
}

// fakeServer is a minimal Streamable HTTP MCP server: it answers initialize,
// tools/list and tools/call, and records the tool calls it receives.
func fakeServer(t *testing.T, failCall bool) (url string, calls *[]string) {
	t.Helper()
	var received []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result string
		switch req.Method {
		case "initialize":
			result = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake","version":"1"}}`
		case "tools/list":
			result = `{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"number"}}}}]}`
		case "tools/call":
			received = append(received, string(req.Params))
			if failCall {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"error":{"code":-32602,"message":"bad"}}`, req.ID)
				return
			}
			result = `{"content":[{"type":"text","text":"ok"}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"result":%s}`, req.ID, result)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &received
}

func TestRealMain_CallsEveryListedTool(t *testing.T) {
	url, calls := fakeServer(t, false)
	var stderr bytes.Buffer
	if code := realMain([]string{"bin", url}, "tools_call", &stderr); code != 0 {
		t.Fatalf("realMain = %d, stderr: %s", code, stderr.String())
	}
	if len(*calls) != 1 || !strings.Contains((*calls)[0], `"a":1`) {
		t.Errorf("tool calls = %v, want one call to add with a=1", *calls)
	}
}

func TestRealMain_ReportsFailures(t *testing.T) {
	url, _ := fakeServer(t, true)
	var stderr bytes.Buffer
	if code := realMain([]string{"bin", url}, "tools_call", &stderr); code != 1 {
		t.Errorf("realMain = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "scenario tools_call: tools/call add") {
		t.Errorf("stderr = %q, want the failing scenario and tool named", stderr.String())
	}
}

func TestRealMain_Usage(t *testing.T) {
	var stderr bytes.Buffer
	if code := realMain([]string{"bin"}, "", &stderr); code != exitUsage {
		t.Errorf("realMain = %d, want %d", code, exitUsage)
	}
}

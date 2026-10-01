package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
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
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"error":{"code":-32602,"message":"bad"}}`, mustJSONID(req.ID))
				return
			}
			result = `{"content":[{"type":"text","text":"ok"}]}`
		default:
			// A handshake-era server: unknown methods, server/discover among
			// them, get Method not found.
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"error":{"code":-32601,"message":"Method not found"}}`, mustJSONID(req.ID))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"result":%s}`, mustJSONID(req.ID), result)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &received
}

func TestRealMain_CallsEveryListedTool(t *testing.T) {
	url, calls := fakeServer(t, false)
	var stderr bytes.Buffer
	if code := realMain([]string{"bin", url}, "tools_call", "", &stderr); code != 0 {
		t.Fatalf("realMain = %d, stderr: %s", code, stderr.String())
	}
	if len(*calls) != 1 || !strings.Contains((*calls)[0], `"a":1`) {
		t.Errorf("tool calls = %v, want one call to add with a=1", *calls)
	}
}

func TestRealMain_ToolCallRejectionIsReportedNotFatal(t *testing.T) {
	url, _ := fakeServer(t, true)
	var stderr bytes.Buffer
	if code := realMain([]string{"bin", url}, "tools_call", "", &stderr); code != 0 {
		t.Errorf("realMain = %d, want 0: the scenario's checks judge a rejected call", code)
	}
	if !strings.Contains(stderr.String(), "tools/call add") {
		t.Errorf("stderr = %q, want the rejected tool named", stderr.String())
	}
}

func TestRealMain_UsesTheScenarioToolCalls(t *testing.T) {
	url, calls := fakeServer(t, false)
	var stderr bytes.Buffer
	ctx := `{"toolCalls":[{"name":"add","arguments":{"a":7,"b":null}}]}`
	if code := realMain([]string{"bin", url}, "http-custom-headers", ctx, &stderr); code != 0 {
		t.Fatalf("realMain = %d, stderr: %s", code, stderr.String())
	}
	if len(*calls) != 1 || !strings.Contains((*calls)[0], `"a":7`) || !strings.Contains((*calls)[0], `"b":null`) {
		t.Errorf("tool calls = %v, want the scenario's arguments verbatim", *calls)
	}
}

func TestRealMain_UnreachableServerFails(t *testing.T) {
	var stderr bytes.Buffer
	if code := realMain([]string{"bin", "http://127.0.0.1:1/mcp"}, "tools_call", "", &stderr); code != 1 {
		t.Errorf("realMain = %d, want 1", code)
	}
}

func TestRealMain_Usage(t *testing.T) {
	var stderr bytes.Buffer
	if code := realMain([]string{"bin"}, "", "", &stderr); code != exitUsage {
		t.Errorf("realMain = %d, want %d", code, exitUsage)
	}
}

func mustJSONID(id any) string {
	b, _ := json.Marshal(id)
	return string(b)
}

func TestDefaultCalls_EchoesTheFocalSchemaVerbatim(t *testing.T) {
	focal := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object",` +
		`"prefixItems":[{"type":"string"}],"unevaluatedProperties":false}`)
	calls := defaultCalls([]mcp.Tool{
		{Name: schemaFocalTool, InputSchema: focal},
		{Name: schemaEchoTool, InputSchema: json.RawMessage(`{"type":"object","properties":{"schema":{"type":"object"}}}`)},
	})
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	want, _ := json.Marshal(map[string]json.RawMessage{"schema": focal})
	if string(calls[1].Arguments) != string(want) {
		t.Errorf("echo arguments = %s, want %s", calls[1].Arguments, want)
	}
}

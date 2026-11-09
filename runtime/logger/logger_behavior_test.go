package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// captureJSON routes the package logger to a JSON buffer at the given level and
// returns a function that decodes every record written so far.
func captureJSON(t *testing.T, level slog.Level) func() []map[string]any {
	t.Helper()
	saveLoggerState(t)
	origHandler := customHandler
	t.Cleanup(func() { customHandler = origHandler })

	currentFormat = FormatJSON
	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(level)

	return func() []map[string]any {
		t.Helper()
		var records []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("log line is not JSON: %v\n%s", err, line)
			}
			records = append(records, rec)
		}
		return records
	}
}

// onlyRecord asserts exactly one record was written and returns it.
func onlyRecord(t *testing.T, records []map[string]any) map[string]any {
	t.Helper()
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1: %v", len(records), records)
	}
	return records[0]
}

// assertAttrs asserts each key in want is present in rec with the given value.
// JSON numbers decode as float64, so numeric wants are written as float64.
func assertAttrs(t *testing.T, rec map[string]any, want map[string]any) {
	t.Helper()
	for k, v := range want {
		got, ok := rec[k]
		if !ok {
			t.Errorf("record has no %q: %v", k, rec)
			continue
		}
		if got != v {
			t.Errorf("%s = %#v, want %#v", k, got, v)
		}
	}
}

func messages(records []map[string]any) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r["msg"].(string))
	}
	return out
}

func TestSetLevel_FiltersRecordsBelowLevel(t *testing.T) {
	tests := []struct {
		level slog.Level
		want  []string
	}{
		{slog.LevelDebug, []string{"d", "i", "w", "e"}},
		{slog.LevelInfo, []string{"i", "w", "e"}},
		{slog.LevelWarn, []string{"w", "e"}},
		{slog.LevelError, []string{"e"}},
	}
	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			read := captureJSON(t, tt.level)
			Debug("d")
			Info("i")
			Warn("w")
			Error("e")
			if got := strings.Join(messages(read()), ","); got != strings.Join(tt.want, ",") {
				t.Errorf("emitted %q, want %q", got, strings.Join(tt.want, ","))
			}
		})
	}
}

func TestSetVerbose_TogglesDebugRecords(t *testing.T) {
	read := captureJSON(t, slog.LevelInfo)
	SetVerbose(true)
	Debug("shown")
	SetVerbose(false)
	Debug("hidden")
	Info("info")
	if got := strings.Join(messages(read()), ","); got != "shown,info" {
		t.Errorf("emitted %q, want %q", got, "shown,info")
	}
}

func TestLevelFunctions_WriteLevelMessageAndAttrs(t *testing.T) {
	ctx := WithSessionID(context.Background(), "sess-7")
	tests := []struct {
		name      string
		log       func()
		wantLevel string
		wantCtx   bool
	}{
		{"Info", func() { Info("m", "k", "v") }, "INFO", false},
		{"InfoContext", func() { InfoContext(ctx, "m", "k", "v") }, "INFO", true},
		{"Debug", func() { Debug("m", "k", "v") }, "DEBUG", false},
		{"DebugContext", func() { DebugContext(ctx, "m", "k", "v") }, "DEBUG", true},
		{"Warn", func() { Warn("m", "k", "v") }, "WARN", false},
		{"WarnContext", func() { WarnContext(ctx, "m", "k", "v") }, "WARN", true},
		{"Error", func() { Error("m", "k", "v") }, "ERROR", false},
		{"ErrorContext", func() { ErrorContext(ctx, "m", "k", "v") }, "ERROR", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := captureJSON(t, slog.LevelDebug)
			tt.log()
			rec := onlyRecord(t, read())
			assertAttrs(t, rec, map[string]any{"level": tt.wantLevel, "msg": "m", "k": "v"})
			_, hasSession := rec["session_id"]
			if hasSession != tt.wantCtx {
				t.Errorf("session_id present = %v, want %v: %v", hasSession, tt.wantCtx, rec)
			}
			if tt.wantCtx && rec["session_id"] != "sess-7" {
				t.Errorf("session_id = %v, want sess-7", rec["session_id"])
			}
		})
	}
}

func TestStructuredAttributes_KeepTheirTypes(t *testing.T) {
	read := captureJSON(t, slog.LevelInfo)
	Info("structured", "string", "value", "int", 42, "bool", true, "float", 3.14)
	assertAttrs(t, onlyRecord(t, read()), map[string]any{
		"string": "value", "int": float64(42), "bool": true, "float": 3.14,
	})
}

func TestLLMAndToolHelpers_WriteFixedFieldsThenExtras(t *testing.T) {
	tests := []struct {
		name      string
		log       func()
		wantLevel string
		want      map[string]any
	}{
		{
			name:      "LLMCall",
			log:       func() { LLMCall("openai", "assistant", 5, 0.7, "model", "gpt-4") },
			wantLevel: "INFO",
			want: map[string]any{
				"msg": "Provider API Call", "provider": "openai", "role": "assistant",
				"messages": float64(5), "temperature": 0.7, "model": "gpt-4",
			},
		},
		{
			name:      "LLMResponse",
			log:       func() { LLMResponse("anthropic", "assistant", 150, 50, 0.01, "latency_ms", 500) },
			wantLevel: "INFO",
			want: map[string]any{
				"msg": "Provider API Response", "provider": "anthropic", "role": "assistant",
				"tokens_in": float64(150), "tokens_out": float64(50), "cost": 0.01, "latency_ms": float64(500),
			},
		},
		{
			name:      "LLMError",
			log:       func() { LLMError("openai", "assistant", errors.New("rate limit"), "attempt", 3) },
			wantLevel: "ERROR",
			want: map[string]any{
				"msg": "Provider API Call Failed", "provider": "openai", "role": "assistant",
				"error": "rate limit", "attempt": float64(3),
			},
		},
		{
			name:      "ToolCall",
			log:       func() { ToolCall("openai", 5, 3, "required", "max_iterations", 10) },
			wantLevel: "INFO",
			want: map[string]any{
				"msg": "Provider Tool Call", "provider": "openai", "messages": float64(5),
				"tools": float64(3), "choice": "required", "max_iterations": float64(10),
			},
		},
		{
			name:      "ToolResponse",
			log:       func() { ToolResponse("openai", 150, 50, 2, 0.02, "duration_ms", 1500) },
			wantLevel: "INFO",
			want: map[string]any{
				"msg": "Provider Tool Response", "provider": "openai", "tokens_in": float64(150),
				"tokens_out": float64(50), "tool_calls": float64(2), "cost": 0.02, "duration_ms": float64(1500),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := captureJSON(t, slog.LevelInfo)
			tt.log()
			rec := onlyRecord(t, read())
			tt.want["level"] = tt.wantLevel
			assertAttrs(t, rec, tt.want)
		})
	}
}

const (
	fakeOpenAIKey = "sk-1234567890abcdefghijklmnopqrstuvwxyz12345678" // Fake test key - not a real credential
	fakeGoogleKey = "AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe"         // Fake test key - not a real credential
)

func TestAPIRequest_LogsRedactedRequestAtDebug(t *testing.T) {
	read := captureJSON(t, slog.LevelDebug)
	APIRequest("TestProvider", "POST", "https://api.test.com/v1?key="+fakeGoogleKey,
		map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + fakeOpenAIKey},
		map[string]any{"prompt": "Hello", "api_key": fakeOpenAIKey})

	rec := onlyRecord(t, read())
	assertAttrs(t, rec, map[string]any{
		"level": "DEBUG", "msg": "🔵 API Request", "provider": "TestProvider", "method": "POST",
		"url":  "https://api.test.com/v1?key=AIza...[REDACTED]",
		"body": `{"api_key":"sk-1...[REDACTED]","prompt":"Hello"}`,
	})
	headers, ok := rec["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers = %#v, want an object", rec["headers"])
	}
	assertAttrs(t, headers, map[string]any{
		"Content-Type": "application/json", "Authorization": "Bearer [REDACTED]",
	})
}

func TestAPIRequest_OmitsAbsentHeadersAndBody(t *testing.T) {
	read := captureJSON(t, slog.LevelDebug)
	APIRequest("P", "GET", "https://api.test.com", nil, nil)
	rec := onlyRecord(t, read())
	for _, k := range []string{"headers", "body", "body_error"} {
		if _, ok := rec[k]; ok {
			t.Errorf("record has %q, want it omitted: %v", k, rec)
		}
	}
}

func TestAPIRequest_UnmarshalableBodyLogsBodyError(t *testing.T) {
	read := captureJSON(t, slog.LevelDebug)
	APIRequest("P", "POST", "https://api.test.com", nil, make(chan int))
	rec := onlyRecord(t, read())
	if got := rec["body_error"]; got != "json: unsupported type: chan int" {
		t.Errorf("body_error = %v, want the marshal error", got)
	}
	if _, ok := rec["body"]; ok {
		t.Errorf("record has body, want none: %v", rec)
	}
}

func TestAPIRequestAndResponse_SilentAboveDebug(t *testing.T) {
	read := captureJSON(t, slog.LevelInfo)
	APIRequest("P", "POST", "https://api.test.com", nil, nil)
	APIResponse("P", 200, `{"status":"ok"}`, nil)
	APIResponse("P", 500, "", errors.New("boom"))
	if got := read(); len(got) != 0 {
		t.Errorf("got %d records at info level, want 0: %v", len(got), got)
	}
}

func TestAPIResponse_StatusSelectsMessageAndBodyIsRedacted(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantMsg   string
		wantBody  any // nil means absent
		wantLevel string
	}{
		{"2xx json pretty-printed", 200, `{"status":"ok","api_key":"` + fakeOpenAIKey + `"}`,
			"🟢 API Response", "{\n  \"api_key\": \"sk-1...[REDACTED]\",\n  \"status\": \"ok\"\n}", "DEBUG"},
		{"3xx", 302, "moved", "🟡 API Response", "moved", "DEBUG"},
		{"4xx", 429, `{"error":"rate"}`, "🔴 API Response", "{\n  \"error\": \"rate\"\n}", "DEBUG"},
		{"non-json redacted as-is", 200, "key " + fakeGoogleKey, "🟢 API Response", "key AIza...[REDACTED]", "DEBUG"},
		{"empty body omitted", 204, "", "🟢 API Response", nil, "DEBUG"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := captureJSON(t, slog.LevelDebug)
			APIResponse("P", tt.status, tt.body, nil)
			rec := onlyRecord(t, read())
			assertAttrs(t, rec, map[string]any{
				"msg": tt.wantMsg, "level": tt.wantLevel, "provider": "P", "status_code": float64(tt.status),
			})
			body, ok := rec["body"]
			if tt.wantBody == nil {
				if ok {
					t.Errorf("body = %v, want absent", body)
				}
				return
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestAPIResponse_ErrorLogsAtErrorLevelWithoutBody(t *testing.T) {
	read := captureJSON(t, slog.LevelDebug)
	APIResponse("P", 500, `{"x":1}`, errors.New("connection failed"))
	rec := onlyRecord(t, read())
	assertAttrs(t, rec, map[string]any{
		"level": "ERROR", "msg": "🔴 API Response Error", "error": "connection failed", "status_code": float64(500),
	})
	if _, ok := rec["body"]; ok {
		t.Errorf("record has body, want none on error: %v", rec)
	}
}

func TestFormatFromEnv(t *testing.T) {
	tests := []struct{ in, want string }{
		{"json", FormatJSON}, {"JSON", FormatJSON}, {"Json", FormatJSON},
		{"text", FormatText}, {"", FormatText}, {"xml", FormatText},
	}
	for _, tt := range tests {
		if got := formatFromEnv(tt.in); got != tt.want {
			t.Errorf("formatFromEnv(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSetOutput_NilRestoresStderr(t *testing.T) {
	saveLoggerState(t)
	var buf bytes.Buffer
	SetOutput(&buf)
	SetOutput(nil)
	Info("after reset")
	if logOutput != os.Stderr {
		t.Errorf("logOutput = %v, want os.Stderr", logOutput)
	}
	if buf.Len() != 0 {
		t.Errorf("buffer received %q after SetOutput(nil)", buf.String())
	}
}

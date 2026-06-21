package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

const interactiveFixtureConfig = "../engine/testdata/interactive/config.arena.yaml"

// newTestServer builds a Server backed by the interactive fixture engine with
// mock provider mode enabled. Mirrors the pattern in engine/interactive_test.go.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	eng, err := engine.NewEngineFromConfigFile(filepath.Clean(interactiveFixtureConfig))
	if err != nil {
		t.Fatalf("NewEngineFromConfigFile: %v", err)
	}
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("EnableMockProviderMode: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	adapter := NewEventAdapter()
	return NewServer(adapter, eng, nil, "")
}

func TestInteractiveOptions(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/interactive/options", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Agents []struct {
			TaskType string `json:"taskType"`
		} `json:"agents"`
		Providers []string `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) == 0 || got.Agents[0].TaskType != "basic" {
		t.Fatalf("want agent basic, got %+v", got.Agents)
	}
	if len(got.Providers) == 0 {
		t.Fatalf("want providers, got %+v", got.Providers)
	}
}

func TestInteractiveSession_MissingVars(t *testing.T) {
	srv := newTestServer(t)
	body := `{"agent":"basic","provider":"mock","variables":{}}`
	req := httptest.NewRequest("POST", "/api/interactive/session", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	var got struct {
		SessionID   string   `json:"sessionId"`
		MissingVars []string `json:"missingVars"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.MissingVars) != 1 || got.MissingVars[0] != "company" {
		t.Fatalf("want missing [company], got %+v (status %d)", got.MissingVars, rec.Code)
	}
	if got.SessionID != "" {
		t.Fatal("session must not be created when vars are missing")
	}
}

func TestInteractiveSession_CreateSuccess(t *testing.T) {
	srv := newTestServer(t)
	body := `{"agent":"basic","provider":"mock","variables":{"company":"Acme"}}`
	req := httptest.NewRequest("POST", "/api/interactive/session", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SessionID == "" {
		t.Fatal("want a sessionId, got empty")
	}
}

func TestInteractiveMessage_Roundtrip(t *testing.T) {
	srv := newTestServer(t)

	// Step 1: create session.
	sessBody := `{"agent":"basic","provider":"mock","variables":{"company":"Acme"}}`
	sessReq := httptest.NewRequest("POST", "/api/interactive/session", strings.NewReader(sessBody))
	sessRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(sessRec, sessReq)
	if sessRec.Code != 200 {
		t.Fatalf("create session status %d: %s", sessRec.Code, sessRec.Body.String())
	}
	var sessGot struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(sessRec.Body.Bytes(), &sessGot); err != nil {
		t.Fatal(err)
	}

	// Step 2: send a message.
	msgBodyStr := fmt.Sprintf(`{"sessionId":%q,"text":"Hello there"}`, sessGot.SessionID)
	msgReq := httptest.NewRequest("POST", "/api/interactive/message", strings.NewReader(msgBodyStr))
	msgRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(msgRec, msgReq)
	if msgRec.Code != 200 {
		t.Fatalf("message status %d: %s", msgRec.Code, msgRec.Body.String())
	}
	var msgGot struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(msgRec.Body.Bytes(), &msgGot); err != nil {
		t.Fatal(err)
	}
	if !msgGot.OK {
		t.Fatal("want ok:true, got false")
	}

	// Step 3: verify the transcript grew (at least user + assistant).
	sess, ok := srv.interactive.get(sessGot.SessionID)
	if !ok {
		t.Fatal("session not in registry after message")
	}
	msgs, err := sess.Messages(context.Background())
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("want at least 2 messages in transcript, got %d", len(msgs))
	}
}

func TestInteractiveMessage_UnknownSession(t *testing.T) {
	srv := newTestServer(t)
	body := `{"sessionId":"no-such-session","text":"hi"}`
	req := httptest.NewRequest("POST", "/api/interactive/message", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("want 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInteractiveOptions_NoEngine(t *testing.T) {
	// NewServer with nil engine — interactive endpoints should return 503.
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/interactive/options", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Fatalf("want 503 when no engine, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInteractiveSession_NoEngine(t *testing.T) {
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil, "")
	req := httptest.NewRequest("POST", "/api/interactive/session", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Fatalf("want 503 when no engine, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInteractiveMessage_NoEngine(t *testing.T) {
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil, "")
	req := httptest.NewRequest("POST", "/api/interactive/message", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Fatalf("want 503 when no engine, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInteractiveSession_BadJSON(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/interactive/session", strings.NewReader("not-json"))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400 for bad JSON, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v — body: %s", err, rec.Body.String())
	}
	if got["error"] != "bad request" {
		t.Fatalf("want error=bad request in JSON body, got %v", got)
	}
}

func TestInteractiveMessage_BadJSON(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/interactive/message", strings.NewReader("not-json"))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400 for bad JSON, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v — body: %s", err, rec.Body.String())
	}
	if got["error"] != "bad request" {
		t.Fatalf("want error=bad request in JSON body, got %v", got)
	}
}

func TestInteractiveSession_UnknownTaskType(t *testing.T) {
	srv := newTestServer(t)
	body := `{"agent":"no-such-agent","provider":"mock","variables":{}}`
	req := httptest.NewRequest("POST", "/api/interactive/session", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400 for unknown task type, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInteractiveSession_UnknownProvider(t *testing.T) {
	srv := newTestServer(t)
	body := `{"agent":"basic","provider":"no-such-provider","variables":{"company":"Acme"}}`
	req := httptest.NewRequest("POST", "/api/interactive/session", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400 for unknown provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

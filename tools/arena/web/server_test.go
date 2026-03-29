package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// mockEngine implements engineRunner for unit tests.
type mockEngine struct {
	plan    *engine.RunPlan
	planErr error
	cfg     *config.Config
}

func (m *mockEngine) GenerateRunPlan(_, _, _, _ []string) (*engine.RunPlan, error) {
	return m.plan, m.planErr
}

func (m *mockEngine) ExecuteRuns(_ context.Context, _ *engine.RunPlan, _ int) ([]string, error) {
	return nil, nil
}

func (m *mockEngine) GetConfig() *config.Config {
	return m.cfg
}

func TestSSEEndpoint(t *testing.T) {
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil) // no engine or statestore needed for SSE test

	// Start test server
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Connect SSE client
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}

	// Send an event through the adapter (give SSE handler time to register)
	time.Sleep(50 * time.Millisecond)
	adapter.HandleEvent(&events.Event{
		Type:        events.EventType("arena.run.started"),
		Timestamp:   time.Now(),
		ExecutionID: "run-sse-test",
		Data: events.CustomEventData{
			EventName: "run_started",
			Data: map[string]interface{}{
				"scenario": "greeting",
			},
		},
	})

	// Read the SSE line
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			var got SSEEvent
			if err := json.Unmarshal([]byte(payload), &got); err != nil {
				t.Fatalf("unmarshal SSE data: %v", err)
			}
			if got.Type != "arena.run.started" {
				t.Errorf("type = %q, want arena.run.started", got.Type)
			}
			if got.ExecutionID != "run-sse-test" {
				t.Errorf("executionId = %q, want run-sse-test", got.ExecutionID)
			}
			return // Success
		}
	}
	t.Fatal("did not receive SSE event")
}

func TestListResultsNilStore(t *testing.T) {
	adapter := NewEventAdapter()
	// nil store — should return empty JSON array
	srv := NewServer(adapter, nil, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/results") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /api/results: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var ids []string
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("got %d results, want 0", len(ids))
	}
}

func TestListResultsEmpty(t *testing.T) {
	adapter := NewEventAdapter()
	store := statestore.NewArenaStateStore()
	srv := NewServer(adapter, nil, store)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/results") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /api/results: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var ids []string
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("got %d results, want 0", len(ids))
	}
}

func TestGetResultNotFound(t *testing.T) {
	adapter := NewEventAdapter()
	store := statestore.NewArenaStateStore()
	srv := NewServer(adapter, nil, store)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/results/nonexistent") //nolint:noctx
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestStartRunNoEngine(t *testing.T) {
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/run", "application/json", strings.NewReader("{}")) //nolint:noctx
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestStartRunPlanError(t *testing.T) {
	adapter := NewEventAdapter()
	mock := &mockEngine{planErr: fmt.Errorf("invalid scenario")}
	srv := newServerWithRunner(adapter, mock, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/run", "application/json", strings.NewReader("{}")) //nolint:noctx
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestStartRunNoCombinations(t *testing.T) {
	adapter := NewEventAdapter()
	mock := &mockEngine{plan: &engine.RunPlan{Combinations: nil}}
	srv := newServerWithRunner(adapter, mock, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/run", "application/json", strings.NewReader("{}")) //nolint:noctx
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestStartRunSuccess(t *testing.T) {
	adapter := NewEventAdapter()
	mock := &mockEngine{plan: &engine.RunPlan{
		Combinations: []engine.RunCombination{{ScenarioID: "greeting", ProviderID: "openai"}},
	}}
	srv := newServerWithRunner(adapter, mock, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/run", "application/json", strings.NewReader("{}")) //nolint:noctx
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "started" {
		t.Errorf("status = %v, want started", body["status"])
	}
}

func TestGetConfigWithEngine(t *testing.T) {
	adapter := NewEventAdapter()
	cfg := &config.Config{}
	mock := &mockEngine{cfg: cfg}
	srv := newServerWithRunner(adapter, mock, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/config") //nolint:noctx
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestGetConfigNoEngine(t *testing.T) {
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/config") //nolint:noctx
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestListResultsWithData(t *testing.T) {
	adapter := NewEventAdapter()
	store := statestore.NewArenaStateStore()

	// Seed a run with metadata so it appears in ListRunIDs
	ctx := context.Background()
	err := store.SaveMetadata(ctx, "run-abc", &statestore.RunMetadata{
		RunID:      "run-abc",
		ScenarioID: "greeting",
		ProviderID: "openai",
	})
	if err != nil {
		t.Fatalf("SaveMetadata: %v", err)
	}

	srv := NewServer(adapter, nil, store)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/results") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /api/results: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var ids []string
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(ids) != 1 || ids[0] != "run-abc" {
		t.Errorf("got ids %v, want [run-abc]", ids)
	}
}

func TestGetResultFound(t *testing.T) {
	adapter := NewEventAdapter()
	store := statestore.NewArenaStateStore()

	ctx := context.Background()
	err := store.SaveMetadata(ctx, "run-xyz", &statestore.RunMetadata{
		RunID:      "run-xyz",
		ScenarioID: "greeting",
		ProviderID: "openai",
	})
	if err != nil {
		t.Fatalf("SaveMetadata: %v", err)
	}

	srv := NewServer(adapter, nil, store)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/results/run-xyz") //nolint:noctx
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result statestore.RunResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.RunID != "run-xyz" {
		t.Errorf("RunID = %q, want run-xyz", result.RunID)
	}
}

func TestGetResultNoStore(t *testing.T) {
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/results/anything") //nolint:noctx
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestSSEHeaders(t *testing.T) {
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil && ctx.Err() == nil {
		t.Fatalf("SSE connect: %v", err)
	}
	if resp == nil {
		return
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
}

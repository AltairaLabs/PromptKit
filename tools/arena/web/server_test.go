package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// mockEngine implements engineRunner for unit tests.
type mockEngine struct {
	plan      *engine.RunPlan
	planErr   error
	cfg       *arenaconfig.Config
	providers []engine.ProviderInfo
	scenarios []engine.ScenarioInfo
}

func (m *mockEngine) GenerateRunPlan(_, _, _, _ []string) (*engine.RunPlan, error) {
	return m.plan, m.planErr
}

func (m *mockEngine) ExecuteRuns(_ context.Context, _ *engine.RunPlan, _ int) ([]string, error) {
	return nil, nil
}

func (m *mockEngine) GetConfig() *arenaconfig.Config {
	return m.cfg
}

func (m *mockEngine) ListProviders() []engine.ProviderInfo {
	return m.providers
}

func (m *mockEngine) ListScenarios() []engine.ScenarioInfo {
	return m.scenarios
}

func (m *mockEngine) RegisterRunCompletedHook(_ engine.RunCompletedHook) {}

func TestSSEEndpoint(t *testing.T) {
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil, "") // no engine or statestore needed for SSE test

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
	srv := NewServer(adapter, nil, nil, "")

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
	srv := NewServer(adapter, nil, store, "")

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
	srv := NewServer(adapter, nil, store, "")

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
	srv := NewServer(adapter, nil, nil, "")

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
	srv := newServerWithRunner(adapter, mock, nil, "")

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
	srv := newServerWithRunner(adapter, mock, nil, "")

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
	srv := newServerWithRunner(adapter, mock, nil, "")

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

// persistingMockEngine returns a fixed runID from ExecuteRuns and seeds the
// state store with matching metadata. The RunCompletedHook is invoked
// after metadata is saved so the server's per-run persistence path can
// observe the same end state the real engine produces.
type persistingMockEngine struct {
	plan      *engine.RunPlan
	store     *statestore.ArenaStateStore
	runID     string
	hooks     []engine.RunCompletedHook
	preReturn func() // optional: invoked just before ExecuteRuns returns
}

func (m *persistingMockEngine) GenerateRunPlan(_, _, _, _ []string) (*engine.RunPlan, error) {
	return m.plan, nil
}

func (m *persistingMockEngine) ExecuteRuns(ctx context.Context, _ *engine.RunPlan, _ int) ([]string, error) {
	_ = m.store.SaveMetadata(ctx, m.runID, &statestore.RunMetadata{
		RunID:      m.runID,
		ScenarioID: "greeting",
		ProviderID: "openai",
	})
	for _, h := range m.hooks {
		h(m.runID, nil)
	}
	if m.preReturn != nil {
		m.preReturn()
	}
	return []string{m.runID}, nil
}

func (m *persistingMockEngine) GetConfig() *arenaconfig.Config       { return nil }
func (m *persistingMockEngine) ListProviders() []engine.ProviderInfo { return nil }
func (m *persistingMockEngine) ListScenarios() []engine.ScenarioInfo { return nil }
func (m *persistingMockEngine) RegisterRunCompletedHook(h engine.RunCompletedHook) {
	m.hooks = append(m.hooks, h)
}

// TestStartRun_PersistsResultsToDisk locks in the invariant that runs started
// via POST /api/run are written to <outputDir>/<runID>.json. Without this,
// recorded playback breaks after a server restart — the state store is
// in-memory and the file loader only sees runs that were saved to disk.
func TestStartRun_PersistsResultsToDisk(t *testing.T) {
	tmpDir := t.TempDir()
	store := statestore.NewArenaStateStore()
	runID := "run-persist-1"

	mock := &persistingMockEngine{
		plan: &engine.RunPlan{
			Combinations: []engine.RunCombination{{ScenarioID: "greeting", ProviderID: "openai"}},
		},
		store: store,
		runID: runID,
	}

	adapter := NewEventAdapter()
	srv := newServerWithRunner(adapter, mock, store, tmpDir)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/run", "application/json", strings.NewReader("{}")) //nolint:noctx
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	expected := filepath.Join(tmpDir, runID+".json")
	deadline := time.Now().Add(2 * time.Second)
	var data []byte
	for time.Now().Before(deadline) {
		if b, statErr := os.ReadFile(expected); statErr == nil {
			data = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if data == nil {
		t.Fatalf("expected %s to be written, but it was not", expected)
	}

	var got engine.RunResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal persisted JSON: %v", err)
	}
	if got.RunID != runID {
		t.Errorf("RunID = %q, want %q", got.RunID, runID)
	}
	if got.ScenarioID != "greeting" {
		t.Errorf("ScenarioID = %q, want %q", got.ScenarioID, "greeting")
	}
}

// TestStartRun_PersistsEachRunBeforeExecuteRunsReturns locks in the
// per-run persistence invariant: the JSON for a completed run must be on
// disk by the time ExecuteRuns is finishing, not buffered until after.
// Without this, killing the server mid-batch strands completed runs in
// the in-memory state store with no on-disk record.
func TestStartRun_PersistsEachRunBeforeExecuteRunsReturns(t *testing.T) {
	tmpDir := t.TempDir()
	store := statestore.NewArenaStateStore()
	runID := "run-per-hook-1"

	expected := filepath.Join(tmpDir, runID+".json")
	// preReturn runs on the server's background goroutine, right after the
	// RunCompletedHook fires and before ExecuteRuns returns. Closing the
	// channel from inside preReturn lets the test body read sawOnDiskBeforeReturn
	// only after the check has actually run — polling for file existence
	// instead is racy because os.WriteFile's O_CREATE makes the path visible
	// in the middle of the hook's write, before the hook returns and preReturn
	// gets to fire.
	var sawOnDiskBeforeReturn atomic.Bool
	preReturnDone := make(chan struct{})

	mock := &persistingMockEngine{
		plan: &engine.RunPlan{
			Combinations: []engine.RunCombination{{ScenarioID: "greeting", ProviderID: "openai"}},
		},
		store: store,
		runID: runID,
		preReturn: func() {
			if _, err := os.Stat(expected); err == nil {
				sawOnDiskBeforeReturn.Store(true)
			}
			close(preReturnDone)
		},
	}

	adapter := NewEventAdapter()
	srv := newServerWithRunner(adapter, mock, store, tmpDir)
	// t.TempDir registers its RemoveAll cleanup first; t.Cleanup is LIFO,
	// so registering WaitBackgroundRuns here runs it BEFORE RemoveAll. The
	// server's handleStartRun goroutine writes to tmpDir AFTER ExecuteRuns
	// returns (the persistRunResults fallback), so without this wait the
	// goroutine races with RemoveAll and trips "directory not empty".
	t.Cleanup(srv.WaitBackgroundRuns)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/run", "application/json", strings.NewReader("{}")) //nolint:noctx
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	select {
	case <-preReturnDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("ExecuteRuns did not reach preReturn within 2s")
	}
	if !sawOnDiskBeforeReturn.Load() {
		t.Errorf("expected %s to exist before ExecuteRuns returned, but it didn't", expected)
	}
}

// TestPersistRunResults_NoOpBranches exercises the bail-out branches of
// persistRunResults so a missing store or unset outputDir can't surprise
// us by panicking — they should silently skip work.
func TestPersistRunResults_NoOpBranches(t *testing.T) {
	ctx := context.Background()
	t.Run("nil store skips", func(t *testing.T) {
		srv := &Server{outputDir: t.TempDir()}
		srv.persistRunResults(ctx, []string{"run-a"})
	})
	t.Run("empty outputDir skips", func(t *testing.T) {
		srv := &Server{stateStore: statestore.NewArenaStateStore()}
		srv.persistRunResults(ctx, []string{"run-a"})
	})
	t.Run("empty runIDs skips", func(t *testing.T) {
		srv := &Server{stateStore: statestore.NewArenaStateStore(), outputDir: t.TempDir()}
		srv.persistRunResults(ctx, nil)
	})
	t.Run("empty runID entry is skipped", func(t *testing.T) {
		dir := t.TempDir()
		srv := &Server{stateStore: statestore.NewArenaStateStore(), outputDir: dir}
		srv.persistRunResults(ctx, []string{""})
		// No file should have been written for an empty ID.
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("expected empty dir, got %d entries", len(entries))
		}
	})
	t.Run("missing run in store is skipped", func(t *testing.T) {
		dir := t.TempDir()
		srv := &Server{stateStore: statestore.NewArenaStateStore(), outputDir: dir}
		srv.persistRunResults(ctx, []string{"never-seeded"})
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("expected empty dir, got %d entries", len(entries))
		}
	})
}

func TestGetConfigWithEngine(t *testing.T) {
	adapter := NewEventAdapter()
	cfg := &arenaconfig.Config{}
	mock := &mockEngine{cfg: cfg}
	srv := newServerWithRunner(adapter, mock, nil, "")

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
	srv := NewServer(adapter, nil, nil, "")

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

	srv := NewServer(adapter, nil, store, "")
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

	srv := NewServer(adapter, nil, store, "")
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
	srv := NewServer(adapter, nil, nil, "")

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
	srv := NewServer(adapter, nil, nil, "")

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

func TestClearResults(t *testing.T) {
	adapter := NewEventAdapter()
	store := statestore.NewArenaStateStore()

	// Seed a run
	ctx := context.Background()
	_ = store.SaveMetadata(ctx, "run-clear", &statestore.RunMetadata{
		RunID:      "run-clear",
		ScenarioID: "test",
		ProviderID: "mock",
	})

	// Create a temp dir with a JSON file
	tmpDir := t.TempDir()
	_ = os.WriteFile(tmpDir+"/run-clear.json", []byte(`{}`), 0600)
	_ = os.WriteFile(tmpDir+"/index.json", []byte(`{}`), 0600)

	srv := NewServer(adapter, nil, store, tmpDir)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Verify results exist
	ids, _ := store.ListRunIDs(ctx)
	if len(ids) != 1 {
		t.Fatalf("expected 1 run before clear, got %d", len(ids))
	}

	// DELETE /api/results
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, ts.URL+"/api/results", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify state store is empty
	ids, _ = store.ListRunIDs(ctx)
	if len(ids) != 0 {
		t.Errorf("expected 0 runs after clear, got %d", len(ids))
	}

	// Verify files are deleted
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("file %s should have been deleted", e.Name())
		}
	}
}

func TestClearResultsNoStore(t *testing.T) {
	adapter := NewEventAdapter()
	srv := NewServer(adapter, nil, nil, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, ts.URL+"/api/results", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestLoadResultsIntoStore(t *testing.T) {
	store := statestore.NewArenaStateStore()

	// Empty dir — should return 0
	emptyDir := t.TempDir()
	n := LoadResultsIntoStore(emptyDir, store)
	if n != 0 {
		t.Errorf("empty dir: got %d, want 0", n)
	}

	// Nonexistent dir — should return 0
	n = LoadResultsIntoStore("/nonexistent/path", store)
	if n != 0 {
		t.Errorf("nonexistent dir: got %d, want 0", n)
	}

	// Dir with valid results
	dir := t.TempDir()
	idx := `{"run_ids":["run-1","run-2"]}`
	run1 := `{"RunID":"run-1","ScenarioID":"s1","ProviderID":"p1","Region":"default","Messages":[{"role":"user","content":"hi"}],"Cost":{"total_cost_usd":0.01},"Duration":1000000000}`
	run2 := `{"RunID":"run-2","ScenarioID":"s2","ProviderID":"p2","Region":"default","Messages":[],"Cost":{"total_cost_usd":0},"Duration":500000000,"Error":"boom"}`
	_ = os.WriteFile(dir+"/index.json", []byte(idx), 0600)
	_ = os.WriteFile(dir+"/run-1.json", []byte(run1), 0600)
	_ = os.WriteFile(dir+"/run-2.json", []byte(run2), 0600)

	n = LoadResultsIntoStore(dir, store)
	if n != 2 {
		t.Errorf("got %d loaded, want 2", n)
	}

	ctx := context.Background()
	ids, _ := store.ListRunIDs(ctx)
	if len(ids) != 2 {
		t.Errorf("store has %d runs, want 2", len(ids))
	}

	result, err := store.GetResult(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetResult run-1: %v", err)
	}
	if result.ScenarioID != "s1" {
		t.Errorf("ScenarioID = %q, want s1", result.ScenarioID)
	}
}

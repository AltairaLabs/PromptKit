package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// newTestExecutor creates an executor with short TTL/intervals for testing.
// The caller must call Close on the returned executor.
func newTestExecutor(opts ...ExecutorOption) *Executor {
	defaults := []ExecutorOption{
		WithClientTTL(50 * time.Millisecond),
		WithMaxClients(DefaultMaxClients),
	}
	return NewExecutor(append(defaults, opts...)...)
}

func TestExecutor_Close_StopsCleanup(t *testing.T) {
	e := NewExecutor()
	if err := e.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Double close should be safe.
	if err := e.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestExecutor_Close_ClearsCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcResult(w, req.ID, okTask("task-close"))
	}))
	defer srv.Close()

	e := NewExecutor()
	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify client is cached.
	e.mu.RLock()
	count := len(e.clients)
	e.mu.RUnlock()
	if count != 1 {
		t.Fatalf("cached %d clients before Close, want 1", count)
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify cache is cleared.
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.clients != nil {
		t.Errorf("clients map should be nil after Close, got %v", e.clients)
	}
}

func TestExecutor_TTLEviction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcResult(w, req.ID, okTask("task-ttl"))
	}))
	defer srv.Close()

	// Use a controllable clock.
	now := time.Now()
	mu := sync.Mutex{}

	getNow := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	setNow := func(t time.Time) {
		mu.Lock()
		defer mu.Unlock()
		now = t
	}

	e := NewExecutor(
		WithClientTTL(10*time.Minute),
		WithNoRetry(),
	)
	e.nowFunc = getNow
	defer e.Close()

	desc := &tools.ToolDescriptor{
		Name:      "test-tool",
		A2AConfig: &tools.A2AConfig{AgentURL: srv.URL},
	}

	_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Client should be cached.
	e.mu.RLock()
	count := len(e.clients)
	e.mu.RUnlock()
	if count != 1 {
		t.Fatalf("cached %d clients, want 1", count)
	}

	// Advance time past TTL and trigger eviction.
	setNow(now.Add(11 * time.Minute))
	e.evictStale()

	// Client should be evicted.
	e.mu.RLock()
	count = len(e.clients)
	e.mu.RUnlock()
	if count != 0 {
		t.Errorf("cached %d clients after TTL expiry, want 0", count)
	}
}

func TestExecutor_TTLEviction_PreservesRecentClients(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcResult(w, req.ID, okTask("task-1"))
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcResult(w, req.ID, okTask("task-2"))
	}))
	defer srv2.Close()

	start := time.Now()
	now := start
	mu := sync.Mutex{}

	getNow := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	setNow := func(t time.Time) {
		mu.Lock()
		defer mu.Unlock()
		now = t
	}

	e := NewExecutor(
		WithClientTTL(10*time.Minute),
		WithNoRetry(),
	)
	e.nowFunc = getNow
	defer e.Close()

	desc1 := &tools.ToolDescriptor{
		Name:      "tool-1",
		A2AConfig: &tools.A2AConfig{AgentURL: srv1.URL},
	}
	desc2 := &tools.ToolDescriptor{
		Name:      "tool-2",
		A2AConfig: &tools.A2AConfig{AgentURL: srv2.URL},
	}

	// Create first client at t=0.
	_, _ = e.Execute(context.Background(), desc1, json.RawMessage(`{"query":"hello"}`))

	// Advance time by 6 minutes, then create second client.
	setNow(start.Add(6 * time.Minute))
	_, _ = e.Execute(context.Background(), desc2, json.RawMessage(`{"query":"hello"}`))

	// Advance to 11 minutes from start — first client is stale (11min > 10min TTL),
	// second is fresh (5min < 10min TTL).
	setNow(start.Add(11 * time.Minute))
	e.evictStale()

	e.mu.RLock()
	count := len(e.clients)
	_, hasSrv1 := e.clients[srv1.URL]
	_, hasSrv2 := e.clients[srv2.URL]
	e.mu.RUnlock()

	if count != 1 {
		t.Errorf("cached %d clients, want 1", count)
	}
	if hasSrv1 {
		t.Error("stale client for srv1 should have been evicted")
	}
	if !hasSrv2 {
		t.Error("recent client for srv2 should still be cached")
	}
}

func TestExecutor_MaxClientsEvictsLRU(t *testing.T) {
	servers := make([]*httptest.Server, 3)
	for i := range servers {
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := decodeRPC(r)
			rpcResult(w, req.ID, okTask("task-max"))
		}))
		defer servers[i].Close()
	}

	now := time.Now()
	mu := sync.Mutex{}

	getNow := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	advance := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(d)
	}

	e := NewExecutor(
		WithMaxClients(2),
		WithClientTTL(1*time.Hour),
		WithNoRetry(),
	)
	e.nowFunc = getNow
	defer e.Close()

	// Add first two clients at different times.
	desc0 := &tools.ToolDescriptor{
		Name:      "tool-0",
		A2AConfig: &tools.A2AConfig{AgentURL: servers[0].URL},
	}
	_, _ = e.Execute(context.Background(), desc0, json.RawMessage(`{"query":"hello"}`))
	advance(1 * time.Minute)

	desc1 := &tools.ToolDescriptor{
		Name:      "tool-1",
		A2AConfig: &tools.A2AConfig{AgentURL: servers[1].URL},
	}
	_, _ = e.Execute(context.Background(), desc1, json.RawMessage(`{"query":"hello"}`))
	advance(1 * time.Minute)

	// Adding a third client should evict the LRU (servers[0]).
	desc2 := &tools.ToolDescriptor{
		Name:      "tool-2",
		A2AConfig: &tools.A2AConfig{AgentURL: servers[2].URL},
	}
	_, _ = e.Execute(context.Background(), desc2, json.RawMessage(`{"query":"hello"}`))

	e.mu.RLock()
	count := len(e.clients)
	_, has0 := e.clients[servers[0].URL]
	_, has1 := e.clients[servers[1].URL]
	_, has2 := e.clients[servers[2].URL]
	e.mu.RUnlock()

	if count != 2 {
		t.Errorf("cached %d clients, want 2", count)
	}
	if has0 {
		t.Error("LRU client for server[0] should have been evicted")
	}
	if !has1 {
		t.Error("client for server[1] should still be cached")
	}
	if !has2 {
		t.Error("new client for server[2] should be cached")
	}
}

func TestExecutor_MaxClientsEvictsLRU_WithRecentAccess(t *testing.T) {
	servers := make([]*httptest.Server, 3)
	for i := range servers {
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := decodeRPC(r)
			rpcResult(w, req.ID, okTask("task-lru"))
		}))
		defer servers[i].Close()
	}

	now := time.Now()
	mu := sync.Mutex{}

	getNow := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	advance := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(d)
	}

	e := NewExecutor(
		WithMaxClients(2),
		WithClientTTL(1*time.Hour),
		WithNoRetry(),
	)
	e.nowFunc = getNow
	defer e.Close()

	desc0 := &tools.ToolDescriptor{
		Name:      "tool-0",
		A2AConfig: &tools.A2AConfig{AgentURL: servers[0].URL},
	}
	desc1 := &tools.ToolDescriptor{
		Name:      "tool-1",
		A2AConfig: &tools.A2AConfig{AgentURL: servers[1].URL},
	}

	// Add two clients.
	_, _ = e.Execute(context.Background(), desc0, json.RawMessage(`{"query":"hello"}`))
	advance(1 * time.Minute)
	_, _ = e.Execute(context.Background(), desc1, json.RawMessage(`{"query":"hello"}`))
	advance(1 * time.Minute)

	// Re-access server[0] so it becomes more recent than server[1].
	_, _ = e.Execute(context.Background(), desc0, json.RawMessage(`{"query":"hello"}`))
	advance(1 * time.Minute)

	// Adding a third client should evict server[1] (the actual LRU).
	desc2 := &tools.ToolDescriptor{
		Name:      "tool-2",
		A2AConfig: &tools.A2AConfig{AgentURL: servers[2].URL},
	}
	_, _ = e.Execute(context.Background(), desc2, json.RawMessage(`{"query":"hello"}`))

	e.mu.RLock()
	_, has0 := e.clients[servers[0].URL]
	_, has1 := e.clients[servers[1].URL]
	_, has2 := e.clients[servers[2].URL]
	e.mu.RUnlock()

	if !has0 {
		t.Error("recently accessed client for server[0] should still be cached")
	}
	if has1 {
		t.Error("LRU client for server[1] should have been evicted")
	}
	if !has2 {
		t.Error("new client for server[2] should be cached")
	}
}

func TestExecutor_WithClientTTL(t *testing.T) {
	e := NewExecutor(WithClientTTL(5 * time.Minute))
	defer e.Close()
	if e.clientTTL != 5*time.Minute {
		t.Errorf("clientTTL = %v, want 5m", e.clientTTL)
	}
}

func TestExecutor_WithMaxClients(t *testing.T) {
	e := NewExecutor(WithMaxClients(50))
	defer e.Close()
	if e.maxClients != 50 {
		t.Errorf("maxClients = %d, want 50", e.maxClients)
	}
}

func TestExecutor_DefaultCacheSettings(t *testing.T) {
	e := NewExecutor()
	defer e.Close()
	if e.clientTTL != DefaultClientTTL {
		t.Errorf("clientTTL = %v, want %v", e.clientTTL, DefaultClientTTL)
	}
	if e.maxClients != DefaultMaxClients {
		t.Errorf("maxClients = %d, want %d", e.maxClients, DefaultMaxClients)
	}
}

func TestExecutor_GetOrCreateClient_UpdatesLastUsed(t *testing.T) {
	now := time.Now()
	mu := sync.Mutex{}

	getNow := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	e := NewExecutor(WithNoRetry())
	e.nowFunc = getNow
	defer e.Close()

	// Create a client.
	_ = e.getOrCreateClient("http://example.com")

	e.mu.RLock()
	entry := e.clients["http://example.com"]
	initialTime := entry.lastUsed
	e.mu.RUnlock()

	// Advance time and access again.
	mu.Lock()
	now = now.Add(5 * time.Minute)
	mu.Unlock()

	_ = e.getOrCreateClient("http://example.com")

	e.mu.RLock()
	updatedTime := e.clients["http://example.com"].lastUsed
	e.mu.RUnlock()

	if !updatedTime.After(initialTime) {
		t.Errorf("lastUsed was not updated: initial=%v, updated=%v", initialTime, updatedTime)
	}
}

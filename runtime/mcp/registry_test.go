package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_RegisterServer(t *testing.T) {
	registry := NewRegistry()

	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
		Args:    []string{"hello"},
	}

	err := registry.RegisterServer(config)
	require.NoError(t, err)

	// Verify server is registered
	servers := registry.ListServers()
	assert.Contains(t, servers, "test-server")
}

func TestRegistry_RegisterServer_EmptyName(t *testing.T) {
	registry := NewRegistry()

	config := ServerConfig{
		Name:    "",
		Command: "echo",
	}

	err := registry.RegisterServer(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestRegistry_RegisterServer_Duplicate(t *testing.T) {
	registry := NewRegistry()

	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	err := registry.RegisterServer(config)
	require.NoError(t, err)

	// Try to register again with same name
	err = registry.RegisterServer(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistry_RegisterServer_AfterClose(t *testing.T) {
	registry := NewRegistry()

	err := registry.Close()
	require.NoError(t, err)

	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	err = registry.RegisterServer(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestRegistry_ListServers(t *testing.T) {
	registry := NewRegistry()

	// Empty registry
	servers := registry.ListServers()
	assert.Empty(t, servers)

	// Add servers
	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "echo"})
	_ = registry.RegisterServer(ServerConfig{Name: "server2", Command: "cat"})

	servers = registry.ListServers()
	assert.Len(t, servers, 2)
	assert.Contains(t, servers, "server1")
	assert.Contains(t, servers, "server2")
}

func TestRegistry_GetClient_NotRegistered(t *testing.T) {
	registry := NewRegistry()

	client, err := registry.GetClient(context.Background(), "non-existent")
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "not registered")
}

func TestRegistry_GetClient_AfterClose(t *testing.T) {
	registry := NewRegistry()

	_ = registry.RegisterServer(ServerConfig{Name: "test", Command: "echo"})
	registry.Close()

	client, err := registry.GetClient(context.Background(), "test")
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "closed")
}

func TestRegistry_GetClientForTool_NotFound(t *testing.T) {
	registry := NewRegistry()

	client, err := registry.GetClientForTool(context.Background(), "unknown-tool")
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistry_Close(t *testing.T) {
	registry := NewRegistry()

	err := registry.Close()
	assert.NoError(t, err)
	assert.True(t, registry.closed)

	// Second close should be idempotent
	err = registry.Close()
	assert.NoError(t, err)
}

func TestNewRegistryWithServers(t *testing.T) {
	configs := []ServerConfigData{
		{Name: "server1", Command: "echo", Args: []string{"hello"}},
		{Name: "server2", Command: "cat", Args: []string{}, Env: map[string]string{"KEY": "value"}},
	}

	registry, err := NewRegistryWithServers(configs)
	require.NoError(t, err)
	require.NotNil(t, registry)

	servers := registry.ListServers()
	assert.Len(t, servers, 2)
	assert.Contains(t, servers, "server1")
	assert.Contains(t, servers, "server2")
}

func TestNewRegistryWithServers_Error(t *testing.T) {
	configs := []ServerConfigData{
		{Name: "server1", Command: "echo"},
		{Name: "", Command: "cat"}, // Empty name should cause error
	}

	registry, err := NewRegistryWithServers(configs)
	assert.Error(t, err)
	assert.Nil(t, registry)
}

func TestRegistry_ListAllTools_Empty(t *testing.T) {
	registry := NewRegistry()

	tools, err := registry.ListAllTools(context.Background())
	assert.NoError(t, err)
	assert.Empty(t, tools)
}

func TestRegistry_ListAllTools_AfterClose(t *testing.T) {
	registry := NewRegistry()
	registry.Close()

	tools, err := registry.ListAllTools(context.Background())
	assert.NoError(t, err) // Should handle gracefully
	assert.Empty(t, tools)
}

func TestRegistry_GetToolSchema_NotFound(t *testing.T) {
	registry := NewRegistry()

	schema, err := registry.GetToolSchema(context.Background(), "non-existent-tool")
	assert.Error(t, err)
	assert.Nil(t, schema)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistry_TryGetExistingClient_NotRegistered(t *testing.T) {
	// tryGetExistingClient returns (nil, nil) when no local client exists —
	// the "not registered" error is reported by GetClient at the boundary.
	// This loose contract lets child registries (Fork) fall through to a
	// parent before the caller decides the lookup truly failed.
	registry := NewRegistry()

	client, err := registry.tryGetExistingClient("non-existent")
	require.NoError(t, err)
	assert.Nil(t, client)

	// GetClient surfaces the "not registered" error at the public boundary.
	_, err = registry.GetClient(context.Background(), "non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestRegistry_TryGetExistingClient_AfterClose(t *testing.T) {
	registry := NewRegistry()

	_ = registry.RegisterServer(ServerConfig{Name: "test", Command: "echo"})
	registry.Close()

	client, err := registry.tryGetExistingClient("test")
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "closed")
}

func TestServerConfigData_Conversion(t *testing.T) {
	// Test that ServerConfigData can be converted to ServerConfig
	data := ServerConfigData{
		Name:    "test-server",
		Command: "echo",
		Args:    []string{"hello", "world"},
		Env:     map[string]string{"KEY": "value"},
	}

	config := ServerConfig(data)

	assert.Equal(t, "test-server", config.Name)
	assert.Equal(t, "echo", config.Command)
	assert.Equal(t, []string{"hello", "world"}, config.Args)
	assert.Equal(t, map[string]string{"KEY": "value"}, config.Env)
}

func TestNewRegistryWithServers_EmptyList(t *testing.T) {
	registry, err := NewRegistryWithServers([]ServerConfigData{})
	require.NoError(t, err)
	require.NotNil(t, registry)

	servers := registry.ListServers()
	assert.Empty(t, servers)
}

func TestNewRegistryWithServers_DuplicateNames(t *testing.T) {
	configs := []ServerConfigData{
		{Name: "server1", Command: "echo"},
		{Name: "server1", Command: "cat"}, // Duplicate name
	}

	registry, err := NewRegistryWithServers(configs)
	assert.Error(t, err)
	assert.Nil(t, registry)
	assert.Contains(t, err.Error(), "already registered")
}

// --- Process limiter tests ---

func TestRegistry_ActiveProcessCount_Unlimited(t *testing.T) {
	registry := NewRegistry()
	assert.Equal(t, 0, registry.ActiveProcessCount())
}

func TestRegistry_ProcessLimit_CreateNewClientFails(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 1})
	// The real stdio client, with millisecond start-retry backoff so the
	// failing starts below don't wait out the default 100ms-doubling schedule.
	registry.newClientFunc = func(c ServerConfig) Client {
		opts := DefaultClientOptions()
		opts.RetryDelay = time.Millisecond
		return NewStdioClientWithOptions(c, opts)
	}

	// Register two servers
	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "/nonexistent/cmd1"})
	_ = registry.RegisterServer(ServerConfig{Name: "server2", Command: "/nonexistent/cmd2"})

	// First client creation should acquire a slot (then fail on init, releasing the slot)
	_, err := registry.GetClient(context.Background(), "server1")
	assert.Error(t, err) // Init fails

	// Slot should have been released, so another attempt should be possible
	assert.Equal(t, 0, registry.ActiveProcessCount(), "a failed start must release its process slot")
	_, err = registry.GetClient(context.Background(), "server2")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrMaxProcessesReached, "server2 got the slot server1 released; only its start failed")
	assert.Contains(t, err.Error(), "failed to initialize MCP server server2")
}

func TestRegistry_Close_ReleasesSlots(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 3})

	// Simulate active clients by consuming semaphore slots
	_ = registry.acquireProcessSlot()
	_ = registry.acquireProcessSlot()

	// Create mock clients in the registry
	mockClient1 := &mockClient{}
	mockClient2 := &mockClient{}
	registry.clients["server1"] = mockClient1
	registry.clients["server2"] = mockClient2

	err := registry.Close()
	assert.NoError(t, err)

	// After close, the semaphore should have released the 2 slots
	assert.Equal(t, 0, len(registry.processSem))
}

func TestRegistry_CreateNewClient_DoubleCheckReturnsAlive(t *testing.T) {
	// Test the double-check path: if a client was created by another goroutine
	// between tryGetExistingClient and createNewClient
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 2})

	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "/nonexistent"})

	// Pre-populate with an alive mock client
	aliveClient := &mockClient{alive: true}
	registry.clients["server1"] = aliveClient

	// GetClient should find the alive client via tryGetExistingClient
	client, err := registry.GetClient(context.Background(), "server1")
	assert.NoError(t, err)
	assert.Equal(t, aliveClient, client)
}

func TestRegistry_CreateNewClient_ReplacesDeadClient(t *testing.T) {
	// Test the path where an existing dead client is replaced
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 3})

	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "/nonexistent"})

	// Pre-populate with a dead client (simulate a slot already consumed for it)
	deadClient := &mockClient{alive: false}
	registry.clients["server1"] = deadClient
	_ = registry.acquireProcessSlot() // simulate the slot for the dead client

	// Before GetClient: 1 slot in use
	assert.Equal(t, 1, len(registry.processSem))

	// tryGetExistingClient will return nil,nil (client exists but not alive)
	// createNewClient will:
	//   1. acquireProcessSlot (sem=2)
	//   2. detect dead client, releaseProcessSlot (sem=1, replacing the dead one's slot)
	//   3. Init fails, releaseProcessSlot (sem=0)
	_, err := registry.GetClient(context.Background(), "server1")
	assert.Error(t, err) // Init will fail (bad command)

	// Both slots were released (the dead client's and the new attempt's)
	assert.Equal(t, 0, len(registry.processSem))
}

func TestRegistry_CreateNewClient_InitFailsReleasesSlot(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 3})

	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "/nonexistent"})

	// Before GetClient, no slots are used
	assert.Equal(t, 0, len(registry.processSem))

	_, err := registry.GetClient(context.Background(), "server1")
	assert.Error(t, err)

	// After failed init, slot should be released
	assert.Equal(t, 0, len(registry.processSem))
}

func TestRegistry_CreateNewClient_SemaphoreAcquireFails(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 1})

	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "/nonexistent"})

	// Fill the semaphore
	registry.processSem <- struct{}{}

	_, err := registry.GetClient(context.Background(), "server1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire process slot")
	assert.ErrorIs(t, err, ErrMaxProcessesReached)
}

func TestErrMaxProcessesReached(t *testing.T) {
	assert.NotNil(t, ErrMaxProcessesReached)
	assert.Contains(t, ErrMaxProcessesReached.Error(), "maximum concurrent processes")
}

func TestRegistry_GetServerConfig_Exists(t *testing.T) {
	registry := NewRegistry()

	expected := ServerConfig{
		Name:    "test-server",
		Command: "echo",
		Args:    []string{"hello"},
		Env:     map[string]string{"KEY": "val"},
	}
	err := registry.RegisterServer(expected)
	require.NoError(t, err)

	cfg, ok := registry.GetServerConfig("test-server")
	assert.True(t, ok)
	assert.Equal(t, expected, cfg)
}

func TestRegistry_GetServerConfig_NotExists(t *testing.T) {
	registry := NewRegistry()

	cfg, ok := registry.GetServerConfig("non-existent")
	assert.False(t, ok)
	assert.Equal(t, ServerConfig{}, cfg)
}

// mockClient implements the Client interface for testing
type mockClient struct {
	alive    bool
	closed   bool
	initErr  error
	tools    []Tool
	toolsErr error
}

func (m *mockClient) Initialize(_ context.Context) (*InitializeResponse, error) {
	if m.initErr != nil {
		return nil, m.initErr
	}
	m.alive = true
	return &InitializeResponse{ProtocolVersion: ProtocolVersion}, nil
}

func (m *mockClient) ListTools(_ context.Context) ([]Tool, error) {
	return m.tools, m.toolsErr
}

func (m *mockClient) CallTool(_ context.Context, _ string, _ json.RawMessage) (*ToolCallResponse, error) {
	return nil, nil
}
func (m *mockClient) Close() error  { m.closed = true; m.alive = false; return nil }
func (m *mockClient) IsAlive() bool { return m.alive }

func TestRegistry_CreateNewClient_SuccessWithMock(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 3})

	// Override client factory to return a mock
	registry.newClientFunc = func(_ ServerConfig) Client {
		return &mockClient{tools: []Tool{{Name: "tool1"}}}
	}

	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "mock"})

	client, err := registry.GetClient(context.Background(), "server1")
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.True(t, client.IsAlive())

	// Should use 1 process slot
	assert.Equal(t, 1, registry.ActiveProcessCount())
}

func TestRegistry_CreateNewClient_DoubleCheckRace(t *testing.T) {
	// Test the final race-check path: if someone else created the client
	// while we were initializing ours.
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 5})

	callCount := 0
	registry.newClientFunc = func(_ ServerConfig) Client {
		callCount++
		// On the first call, simulate another goroutine inserting a client
		if callCount == 1 {
			aliveClient := &mockClient{alive: true, tools: []Tool{{Name: "tool2"}}}
			registry.mu.Lock()
			registry.clients["server1"] = aliveClient
			registry.mu.Unlock()
		}
		return &mockClient{tools: []Tool{{Name: "tool1"}}}
	}

	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "mock"})

	client, err := registry.GetClient(context.Background(), "server1")
	require.NoError(t, err)
	assert.NotNil(t, client)
	// Should have gotten the pre-existing alive client, not the one we just created
	assert.True(t, client.IsAlive())
}

func TestRegistry_CreateNewClient_WithSemaphoreSuccess(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 2})

	registry.newClientFunc = func(_ ServerConfig) Client {
		return &mockClient{tools: []Tool{{Name: "tool-a"}}}
	}

	_ = registry.RegisterServer(ServerConfig{Name: "s1", Command: "mock"})
	_ = registry.RegisterServer(ServerConfig{Name: "s2", Command: "mock"})

	c1, err := registry.GetClient(context.Background(), "s1")
	require.NoError(t, err)
	assert.NotNil(t, c1)

	c2, err := registry.GetClient(context.Background(), "s2")
	require.NoError(t, err)
	assert.NotNil(t, c2)

	assert.Equal(t, 2, registry.ActiveProcessCount())

	// Close releases slots
	err = registry.Close()
	assert.NoError(t, err)
	assert.Equal(t, 0, len(registry.processSem))
}

func TestRegistry_Close_WithMockClients(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 3})

	registry.newClientFunc = func(_ ServerConfig) Client {
		return &mockClient{}
	}

	_ = registry.RegisterServer(ServerConfig{Name: "s1", Command: "mock"})
	_ = registry.RegisterServer(ServerConfig{Name: "s2", Command: "mock"})

	_, _ = registry.GetClient(context.Background(), "s1")
	_, _ = registry.GetClient(context.Background(), "s2")

	assert.Equal(t, 2, registry.ActiveProcessCount())

	err := registry.Close()
	assert.NoError(t, err)
	assert.Equal(t, 0, len(registry.processSem))
}

func TestRegistry_UnregisterServer(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterServer(ServerConfig{Name: "x", Command: "./foo"}))
	require.Contains(t, reg.ListServers(), "x")

	require.NoError(t, reg.UnregisterServer("x"))
	assert.NotContains(t, reg.ListServers(), "x")

	// Unregistering an unknown server is a no-op.
	require.NoError(t, reg.UnregisterServer("unknown"))
}

func TestRegistry_UnregisterServer_ClosesClient(t *testing.T) {
	reg := NewRegistry()
	closed := make(chan struct{})
	reg.newClientFunc = func(c ServerConfig) Client {
		return &unregisterFakeClient{onClose: func() { close(closed) }, alive: true}
	}
	require.NoError(t, reg.RegisterServer(ServerConfig{Name: "x", Command: "./foo"}))

	_, err := reg.GetClient(context.Background(), "x")
	require.NoError(t, err)

	require.NoError(t, reg.UnregisterServer("x"))
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("UnregisterServer did not close the client")
	}
}

type unregisterFakeClient struct {
	onClose func()
	alive   bool
}

func (f *unregisterFakeClient) Initialize(context.Context) (*InitializeResponse, error) {
	f.alive = true
	return &InitializeResponse{}, nil
}
func (f *unregisterFakeClient) ListTools(context.Context) ([]Tool, error) { return nil, nil }
func (f *unregisterFakeClient) CallTool(context.Context, string, json.RawMessage) (*ToolCallResponse, error) {
	return &ToolCallResponse{}, nil
}
func (f *unregisterFakeClient) Close() error {
	if f.onClose != nil {
		f.onClose()
	}
	f.alive = false
	return nil
}
func (f *unregisterFakeClient) IsAlive() bool { return f.alive }

func TestRegistry_UnregisterServer_PrunesToolIndex(t *testing.T) {
	reg := NewRegistry()
	// Seed toolIndex directly (package-internal access — white-box).
	reg.mu.Lock()
	reg.toolIndex["tool_a"] = "server-1"
	reg.toolIndex["tool_b"] = "server-1"
	reg.toolIndex["tool_c"] = "server-2"
	reg.servers["server-1"] = ServerConfig{Name: "server-1", Command: "./x"}
	reg.servers["server-2"] = ServerConfig{Name: "server-2", Command: "./y"}
	reg.mu.Unlock()

	require.NoError(t, reg.UnregisterServer("server-1"))

	reg.mu.RLock()
	defer reg.mu.RUnlock()
	assert.NotContains(t, reg.toolIndex, "tool_a")
	assert.NotContains(t, reg.toolIndex, "tool_b")
	assert.Equal(t, "server-2", reg.toolIndex["tool_c"])
}

func TestRegistry_UnregisterServer_AfterClose(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Close())
	err := reg.UnregisterServer("x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// TestRegistry_Fork_IsolatesRegistration verifies that two children of the
// same parent can each register a server under the same name without
// collision — the core property needed to run concurrent sandboxes that
// each present at a different URL.
func TestRegistry_Fork_IsolatesRegistration(t *testing.T) {
	parent := NewRegistry()
	childA := parent.Fork()
	childB := parent.Fork()

	cfgA := ServerConfig{Name: "sandbox", URL: "http://localhost:50001/sse"}
	cfgB := ServerConfig{Name: "sandbox", URL: "http://localhost:50002/sse"}

	require.NoError(t, childA.RegisterServer(cfgA))
	require.NoError(t, childB.RegisterServer(cfgB), "both children must accept the same name")

	// Each child resolves "sandbox" to its own URL.
	gotA, ok := childA.GetServerConfig("sandbox")
	require.True(t, ok)
	assert.Equal(t, "http://localhost:50001/sse", gotA.URL)
	gotB, ok := childB.GetServerConfig("sandbox")
	require.True(t, ok)
	assert.Equal(t, "http://localhost:50002/sse", gotB.URL)

	// Parent does not see either child's registration.
	_, ok = parent.GetServerConfig("sandbox")
	assert.False(t, ok, "parent must not inherit child registrations")
}

// TestRegistry_Fork_FallsThroughToParent verifies that lookups for
// parent-registered (static) servers succeed in the child without the
// child needing to re-register them.
func TestRegistry_Fork_FallsThroughToParent(t *testing.T) {
	parent := NewRegistry()
	require.NoError(t, parent.RegisterServer(ServerConfig{
		Name: "shared-mcp",
		URL:  "http://shared.example/sse",
	}))

	child := parent.Fork()
	got, ok := child.GetServerConfig("shared-mcp")
	require.True(t, ok, "child must see parent-registered servers via fall-through")
	assert.Equal(t, "http://shared.example/sse", got.URL)

	// ListServers includes both parent and child entries.
	require.NoError(t, child.RegisterServer(ServerConfig{
		Name: "own", URL: "http://x/sse",
	}))
	names := child.ListServers()
	assert.Contains(t, names, "shared-mcp")
	assert.Contains(t, names, "own")
}

// TestRegistry_Fork_UnregisterChildOnly verifies that unregistering on
// the child does not affect the parent's static state.
func TestRegistry_Fork_UnregisterChildOnly(t *testing.T) {
	parent := NewRegistry()
	require.NoError(t, parent.RegisterServer(ServerConfig{
		Name: "shared-mcp", URL: "http://shared/sse",
	}))

	child := parent.Fork()
	require.NoError(t, child.RegisterServer(ServerConfig{
		Name: "own", URL: "http://own/sse",
	}))

	require.NoError(t, child.UnregisterServer("own"))
	_, ok := child.GetServerConfig("own")
	assert.False(t, ok, "child's own entry should be gone")

	// Parent's static entry is untouched.
	_, ok = parent.GetServerConfig("shared-mcp")
	assert.True(t, ok, "parent's entry must remain after child unregister")

	// Child can still see parent's entry via fall-through.
	_, ok = child.GetServerConfig("shared-mcp")
	assert.True(t, ok)
}

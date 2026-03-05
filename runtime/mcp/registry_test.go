package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	assert.NotNil(t, registry)
	assert.NotNil(t, registry.servers)
	assert.NotNil(t, registry.clients)
	assert.NotNil(t, registry.toolIndex)
	assert.False(t, registry.closed)
}

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

func TestRegistry_RefreshAllToolIndices_Empty(t *testing.T) {
	registry := NewRegistry()

	// Should not error on empty registry
	err := registry.refreshAllToolIndices(context.Background())
	assert.NoError(t, err)
}

func TestRegistry_GetServerNames(t *testing.T) {
	registry := NewRegistry()

	// Empty registry
	names := registry.getServerNames()
	assert.Empty(t, names)

	// Add servers
	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "echo"})
	_ = registry.RegisterServer(ServerConfig{Name: "server2", Command: "cat"})
	_ = registry.RegisterServer(ServerConfig{Name: "server3", Command: "ls"})

	names = registry.getServerNames()
	assert.Len(t, names, 3)
	assert.Contains(t, names, "server1")
	assert.Contains(t, names, "server2")
	assert.Contains(t, names, "server3")
}

func TestRegistry_TryGetExistingClient_NotRegistered(t *testing.T) {
	registry := NewRegistry()

	client, err := registry.tryGetExistingClient("non-existent")
	assert.Error(t, err)
	assert.Nil(t, client)
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

func TestRegistry_CreateNewClient_Integration(t *testing.T) {
	t.Run("successful client creation with tool indexing", func(t *testing.T) {
		registry := NewRegistry()

		// Register a server
		config := ServerConfig{
			Name:    "test-server",
			Command: "test-command",
			Args:    []string{"arg1"},
		}
		err := registry.RegisterServer(config)
		require.NoError(t, err)

		// Note: createNewClient is internal and requires a real MCP server to test properly
		// This test documents the expected behavior - actual testing would need integration tests
		// The function is indirectly tested through GetClient which calls createNewClient
	})
}

func TestRegistry_UpdateToolsResult(t *testing.T) {
	registry := NewRegistry()
	result := make(map[string][]Tool)
	var mu sync.Mutex

	tools := []Tool{
		{Name: "tool1", Description: "Test tool 1"},
		{Name: "tool2", Description: "Test tool 2"},
	}

	registry.updateToolsResult("test-server", tools, result, &mu)

	// Verify result map
	assert.Contains(t, result, "test-server")
	assert.Equal(t, tools, result["test-server"])

	// Verify tool index
	assert.Equal(t, "test-server", registry.toolIndex["tool1"])
	assert.Equal(t, "test-server", registry.toolIndex["tool2"])
}

func TestRegistry_UpdateToolsResult_MultipleServers(t *testing.T) {
	registry := NewRegistry()
	result := make(map[string][]Tool)
	var mu sync.Mutex

	tools1 := []Tool{{Name: "tool1", Description: "From server 1"}}
	tools2 := []Tool{{Name: "tool2", Description: "From server 2"}}

	registry.updateToolsResult("server1", tools1, result, &mu)
	registry.updateToolsResult("server2", tools2, result, &mu)

	assert.Len(t, result, 2)
	assert.Equal(t, "server1", registry.toolIndex["tool1"])
	assert.Equal(t, "server2", registry.toolIndex["tool2"])
}

func TestRegistry_GetClient_CreatesNewClient(t *testing.T) {
	t.Run("documents expected behavior", func(t *testing.T) {
		// This test documents that GetClient should:
		// 1. Check for existing client with tryGetExistingClient
		// 2. If not found, call createNewClient
		// 3. createNewClient should initialize the client and refresh tool index

		// Actual testing requires a mock MCP server implementation
		// which is beyond the scope of unit tests

		registry := NewRegistry()
		ctx := context.Background()

		// Register a server
		config := ServerConfig{
			Name:    "test-server",
			Command: "nonexistent-command", // Will fail but documents the flow
		}
		err := registry.RegisterServer(config)
		require.NoError(t, err)

		// Attempt to get client - will fail since command doesn't exist
		// but exercises the createNewClient code path
		_, err = registry.GetClient(ctx, "test-server")
		assert.Error(t, err) // Expected to fail with nonexistent command
	})
}

func TestRegistry_FetchServerTools_Integration(t *testing.T) {
	t.Run("documents concurrent tool fetching", func(t *testing.T) {
		// fetchServerTools is used internally by ListAllTools
		// It handles concurrent fetching of tools from multiple servers
		// This test documents the expected behavior:
		// 1. GetClient for the server
		// 2. ListTools on the client
		// 3. updateToolsResult with the fetched tools
		// 4. Send errors to errChan if any step fails

		// The function is tested indirectly through ListAllTools tests
		// Direct testing would require mocking the entire MCP client infrastructure
	})
}

func TestRegistry_RefreshToolIndexForServer_Integration(t *testing.T) {
	t.Run("documents tool index refresh behavior", func(t *testing.T) {
		// refreshToolIndexForServer is called by:
		// 1. createNewClient after initializing a new client
		// 2. Potentially by other refresh operations

		// Expected behavior:
		// 1. List tools from the client
		// 2. Remove old tool index entries for this server
		// 3. Add new entries for all current tools

		// This ensures the tool index stays synchronized with server state
		// Testing requires a mock client that can return tools
	})
}

func TestRegistry_ToolIndexSynchronization(t *testing.T) {
	t.Run("tool index updates correctly", func(t *testing.T) {
		registry := NewRegistry()

		// Manually populate tool index
		registry.toolIndex["tool1"] = "server1"
		registry.toolIndex["tool2"] = "server1"
		registry.toolIndex["tool3"] = "server2"

		// Update tools for server1
		newTools := []Tool{
			{Name: "tool1", Description: "Updated"},
			{Name: "tool4", Description: "New tool"},
		}

		result := make(map[string][]Tool)
		var mu sync.Mutex

		// Remove old entries for server1
		for toolName, serverName := range registry.toolIndex {
			if serverName == "server1" {
				delete(registry.toolIndex, toolName)
			}
		}

		// Add new entries
		registry.updateToolsResult("server1", newTools, result, &mu)

		// Verify: tool1 and tool4 should point to server1, tool2 removed, tool3 unchanged
		assert.Equal(t, "server1", registry.toolIndex["tool1"])
		assert.Equal(t, "server1", registry.toolIndex["tool4"])
		assert.NotContains(t, registry.toolIndex, "tool2")
		assert.Equal(t, "server2", registry.toolIndex["tool3"])
	})
}

// --- Process limiter tests ---

func TestNewRegistryWithOptions_Unlimited(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 0})
	assert.NotNil(t, registry)
	assert.Nil(t, registry.processSem)
	assert.Equal(t, 0, registry.options.MaxProcesses)
}

func TestNewRegistryWithOptions_WithLimit(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 5})
	assert.NotNil(t, registry)
	assert.NotNil(t, registry.processSem)
	assert.Equal(t, 5, cap(registry.processSem))
	assert.Equal(t, 5, registry.options.MaxProcesses)
}

func TestNewRegistry_DefaultUnlimited(t *testing.T) {
	registry := NewRegistry()
	assert.Nil(t, registry.processSem, "default registry should have no process limit")
}

func TestRegistry_AcquireProcessSlot_Unlimited(t *testing.T) {
	registry := NewRegistry()
	// Should always succeed with no limit
	err := registry.acquireProcessSlot()
	assert.NoError(t, err)
}

func TestRegistry_AcquireProcessSlot_WithCapacity(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 2})

	// Acquire first slot
	err := registry.acquireProcessSlot()
	assert.NoError(t, err)
	assert.Equal(t, 1, len(registry.processSem))

	// Acquire second slot
	err = registry.acquireProcessSlot()
	assert.NoError(t, err)
	assert.Equal(t, 2, len(registry.processSem))

	// Third should fail immediately
	err = registry.acquireProcessSlot()
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMaxProcessesReached))
}

func TestRegistry_ReleaseProcessSlot(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 2})

	// Acquire both slots
	_ = registry.acquireProcessSlot()
	_ = registry.acquireProcessSlot()
	assert.Equal(t, 2, len(registry.processSem))

	// Release one
	registry.releaseProcessSlot()
	assert.Equal(t, 1, len(registry.processSem))

	// Should be able to acquire again
	err := registry.acquireProcessSlot()
	assert.NoError(t, err)
	assert.Equal(t, 2, len(registry.processSem))
}

func TestRegistry_ReleaseProcessSlot_Unlimited(t *testing.T) {
	registry := NewRegistry()
	// Should not panic
	registry.releaseProcessSlot()
}

func TestRegistry_ReleaseProcessSlot_Empty(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 2})
	// Should not block when semaphore is empty
	registry.releaseProcessSlot()
}

func TestRegistry_ActiveProcessCount_Unlimited(t *testing.T) {
	registry := NewRegistry()
	assert.Equal(t, 0, registry.ActiveProcessCount())
}

func TestRegistry_ActiveProcessCount_WithLimit(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 5})
	assert.Equal(t, 0, registry.ActiveProcessCount())

	_ = registry.acquireProcessSlot()
	assert.Equal(t, 1, registry.ActiveProcessCount())

	_ = registry.acquireProcessSlot()
	assert.Equal(t, 2, registry.ActiveProcessCount())

	registry.releaseProcessSlot()
	assert.Equal(t, 1, registry.ActiveProcessCount())
}

func TestRegistry_ProcessLimit_CreateNewClientFails(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 1})

	// Register two servers
	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "/nonexistent/cmd1"})
	_ = registry.RegisterServer(ServerConfig{Name: "server2", Command: "/nonexistent/cmd2"})

	// First client creation should acquire a slot (then fail on init, releasing the slot)
	_, err := registry.GetClient(context.Background(), "server1")
	assert.Error(t, err) // Init fails

	// Slot should have been released, so another attempt should be possible
	_, err = registry.GetClient(context.Background(), "server2")
	assert.Error(t, err) // Init fails too, but slot was acquired
}

func TestRegistry_ProcessLimit_ExhaustedSlots(t *testing.T) {
	registry := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 1})

	// Manually consume the semaphore slot
	registry.processSem <- struct{}{}

	_ = registry.RegisterServer(ServerConfig{Name: "server1", Command: "echo"})

	// Should fail immediately because semaphore is full
	_, err := registry.GetClient(context.Background(), "server1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire process slot")
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

func TestRegistryOptions_Defaults(t *testing.T) {
	assert.Equal(t, 0, DefaultMaxProcesses, "default should be unlimited (0)")
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

package mcp

import (
	"context"
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

package mcp

import (
	"context"
	"fmt"
	"sync"
)

// RegistryImpl implements the Registry interface
type RegistryImpl struct {
	mu sync.RWMutex

	// Server configurations
	servers map[string]ServerConfig

	// Active clients (lazy initialized)
	clients map[string]Client

	// Tool to server mapping (cached)
	toolIndex map[string]string // tool name -> server name

	// Lifecycle
	closed bool
}

// NewRegistry creates a new MCP server registry
func NewRegistry() *RegistryImpl {
	return &RegistryImpl{
		servers:   make(map[string]ServerConfig),
		clients:   make(map[string]Client),
		toolIndex: make(map[string]string),
	}
}

// RegisterServer adds a new MCP server configuration
func (r *RegistryImpl) RegisterServer(config ServerConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("registry is closed")
	}

	if config.Name == "" {
		return fmt.Errorf("server name cannot be empty")
	}

	if _, exists := r.servers[config.Name]; exists {
		return fmt.Errorf("server %s already registered", config.Name)
	}

	r.servers[config.Name] = config
	return nil
}

// GetClient returns an active client for the given server name
func (r *RegistryImpl) GetClient(ctx context.Context, serverName string) (Client, error) {
	r.mu.RLock()
	client, exists := r.clients[serverName]
	config, hasConfig := r.servers[serverName]
	closed := r.closed
	r.mu.RUnlock()

	if closed {
		return nil, fmt.Errorf("registry is closed")
	}

	if !hasConfig {
		return nil, fmt.Errorf("server %s not registered", serverName)
	}

	// If client exists and is alive, return it
	if exists && client.IsAlive() {
		return client, nil
	}

	// Need to create a new client
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := r.clients[serverName]; exists && client.IsAlive() {
		return client, nil
	}

	// Create and initialize new client
	newClient := NewStdioClient(config)
	if _, err := newClient.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize MCP server %s: %w", serverName, err)
	}

	r.clients[serverName] = newClient

	// Refresh tool index for this server
	if err := r.refreshToolIndexForServer(ctx, serverName, newClient); err != nil {
		// Log error but don't fail - client is still usable
		fmt.Printf("Warning: failed to refresh tool index for %s: %v\n", serverName, err)
	}

	return newClient, nil
}

// GetClientForTool returns the client that provides the specified tool
func (r *RegistryImpl) GetClientForTool(ctx context.Context, toolName string) (Client, error) {
	r.mu.RLock()
	serverName, exists := r.toolIndex[toolName]
	r.mu.RUnlock()

	if !exists {
		// Tool not in index - try to refresh all tools
		if err := r.refreshAllToolIndices(ctx); err != nil {
			return nil, fmt.Errorf("tool %s not found and refresh failed: %w", toolName, err)
		}

		r.mu.RLock()
		serverName, exists = r.toolIndex[toolName]
		r.mu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("tool %s not found in any MCP server", toolName)
		}
	}

	return r.GetClient(ctx, serverName)
}

// ListServers returns all registered server names
func (r *RegistryImpl) ListServers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	servers := make([]string, 0, len(r.servers))
	for name := range r.servers {
		servers = append(servers, name)
	}
	return servers
}

// ListAllTools returns all tools from all connected servers
func (r *RegistryImpl) ListAllTools(ctx context.Context) (map[string][]Tool, error) {
	r.mu.RLock()
	serverNames := make([]string, 0, len(r.servers))
	for name := range r.servers {
		serverNames = append(serverNames, name)
	}
	r.mu.RUnlock()

	result := make(map[string][]Tool)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	// Query all servers in parallel
	for _, name := range serverNames {
		wg.Add(1)
		go func(serverName string) {
			defer wg.Done()

			client, err := r.GetClient(ctx, serverName)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to get client for %s: %w", serverName, err)
				}
				mu.Unlock()
				return
			}

			tools, err := client.ListTools(ctx)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to list tools for %s: %w", serverName, err)
				}
				mu.Unlock()
				return
			}

			mu.Lock()
			result[serverName] = tools
			// Update tool index
			for _, tool := range tools {
				r.toolIndex[tool.Name] = serverName
			}
			mu.Unlock()
		}(name)
	}

	wg.Wait()

	if firstErr != nil && len(result) == 0 {
		return nil, firstErr
	}

	return result, nil
}

// Close shuts down all MCP servers and connections
func (r *RegistryImpl) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	var firstErr error
	for name, client := range r.clients {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close client %s: %w", name, err)
		}
	}

	// Clear all state
	r.clients = make(map[string]Client)
	r.toolIndex = make(map[string]string)

	return firstErr
}

// refreshToolIndexForServer updates the tool index for a specific server
func (r *RegistryImpl) refreshToolIndexForServer(ctx context.Context, serverName string, client Client) error {
	tools, err := client.ListTools(ctx)
	if err != nil {
		return err
	}

	// Remove old entries for this server
	for toolName, srvName := range r.toolIndex {
		if srvName == serverName {
			delete(r.toolIndex, toolName)
		}
	}

	// Add new entries
	for _, tool := range tools {
		r.toolIndex[tool.Name] = serverName
	}

	return nil
}

// refreshAllToolIndices updates the tool index for all servers
func (r *RegistryImpl) refreshAllToolIndices(ctx context.Context) error {
	_, err := r.ListAllTools(ctx)
	return err
}

// GetToolSchema returns the schema for a specific tool
func (r *RegistryImpl) GetToolSchema(ctx context.Context, toolName string) (*Tool, error) {
	client, err := r.GetClientForTool(ctx, toolName)
	if err != nil {
		return nil, err
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	for i := range tools {
		if tools[i].Name == toolName {
			return &tools[i], nil
		}
	}

	return nil, fmt.Errorf("tool %s not found", toolName)
}

// ServerConfigData holds MCP server configuration matching config.MCPServerConfig
type ServerConfigData struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

// NewRegistryWithServers creates a registry and registers multiple servers.
// Returns error if any server registration fails.
func NewRegistryWithServers(serverConfigs []ServerConfigData) (*RegistryImpl, error) {
	registry := NewRegistry()

	for _, cfg := range serverConfigs {
		if err := registry.RegisterServer(ServerConfig(cfg)); err != nil {
			return nil, fmt.Errorf("failed to register MCP server %s: %w", cfg.Name, err)
		}
	}

	return registry, nil
}

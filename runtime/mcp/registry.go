package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// DefaultMaxProcesses is the default maximum number of concurrent MCP processes.
// 0 means unlimited.
const DefaultMaxProcesses = 0

var (
	// ErrMaxProcessesReached is returned when the concurrent process limit has been reached.
	ErrMaxProcessesReached = errors.New("mcp: maximum concurrent processes reached")
)

// RegistryOptions configures the MCP registry behavior.
type RegistryOptions struct {
	// MaxProcesses limits the number of concurrent MCP server processes.
	// 0 means unlimited (no limit enforced).
	MaxProcesses int
}

// RegistryImpl implements the Registry interface
type RegistryImpl struct {
	mu sync.RWMutex

	// parent is non-nil when this registry was produced by Fork. Lookups
	// for servers/clients/tool ownership fall through to the parent when
	// not found locally, so per-conversation children can resolve static
	// (parent-registered) servers without re-registering or re-dialing.
	// Registration is always local to this registry.
	parent *RegistryImpl

	// Server configurations
	servers map[string]ServerConfig

	// Active clients (lazy initialized)
	clients map[string]Client

	// Tool to server mapping (cached)
	toolIndex map[string]string // tool name -> server name

	// Lifecycle
	closed bool

	// Options
	options RegistryOptions

	// Process limiter — buffered channel used as a counting semaphore.
	// nil when MaxProcesses is 0 (unlimited).
	processSem chan struct{}

	// newClientFunc creates a new MCP client. Defaults to newStdioClientAdapter.
	// Can be overridden in tests to inject mock clients.
	newClientFunc func(config ServerConfig) Client
}

// NewRegistry creates a new MCP server registry with default options (unlimited processes).
func NewRegistry() *RegistryImpl {
	return NewRegistryWithOptions(RegistryOptions{MaxProcesses: DefaultMaxProcesses})
}

// newClientAdapter dispatches on ServerConfig.Transport() and returns the
// appropriate Client. If config.TimeoutMs is set, it overrides the default
// request timeout on whichever adapter is selected.
//
// TransportUnknown (neither URL nor Command set) falls through to the stdio
// adapter; the caller will see a clear error from StdioClient.Initialize.
// Upstream validation should prevent this state from ever reaching here.
func newClientAdapter(config *ServerConfig) Client {
	opts := DefaultClientOptions()
	if config.TimeoutMs > 0 {
		opts.RequestTimeout = time.Duration(config.TimeoutMs) * time.Millisecond
	}
	switch config.Transport() {
	case TransportStreamableHTTP:
		return NewStreamableClientWithOptions(*config, opts)
	case TransportSSE:
		return NewSSEClientWithOptions(*config, opts)
	case TransportStdio, TransportUnknown:
		// TransportUnknown falls through to stdio so the caller will see a
		// clear error from StdioClient.Initialize. Upstream validation should
		// prevent the unknown case from ever reaching here.
		return NewStdioClientWithOptions(*config, opts)
	}
	return NewStdioClientWithOptions(*config, opts)
}

// NewRegistryWithOptions creates a new MCP server registry with custom options.
func NewRegistryWithOptions(opts RegistryOptions) *RegistryImpl {
	r := &RegistryImpl{
		servers:   make(map[string]ServerConfig),
		clients:   make(map[string]Client),
		toolIndex: make(map[string]string),
		options:   opts,
		// newClientFunc signature takes ServerConfig by value (matches the
		// stdio-era API); adapt to the pointer-based dispatcher.
		newClientFunc: func(c ServerConfig) Client { return newClientAdapter(&c) },
	}
	if opts.MaxProcesses > 0 {
		r.processSem = make(chan struct{}, opts.MaxProcesses)
	}
	return r
}

// Fork returns a child registry that shares this registry's static
// servers and clients via lookup fall-through, but has its own
// per-instance dynamic state. Children can register their own servers
// (including names that exist in the parent) without colliding; this
// is the mechanism that lets concurrent runs each open their own
// session-scoped MCP source while sharing static client connections.
//
// Registration on the child is local-only: the parent never sees the
// child's entries. Lookups (GetServerConfig, GetClient, GetClientForTool,
// ListServers) try local state first, then fall through to the parent.
// UnregisterServer is local-only.
//
// Cycle safety is the caller's responsibility; in practice forks only
// chain one level deep (engine → run).
func (r *RegistryImpl) Fork() *RegistryImpl {
	return &RegistryImpl{
		parent:        r,
		servers:       make(map[string]ServerConfig),
		clients:       make(map[string]Client),
		toolIndex:     make(map[string]string),
		options:       r.options,
		newClientFunc: r.newClientFunc,
		// processSem is intentionally not shared: child opens are subject
		// to the same global limit only when the parent's semaphore is
		// reachable via parent-level operations. Children of an unlimited
		// parent are also unlimited.
	}
}

// RegisterServer adds a new MCP server configuration. Child registries
// (produced by Fork) accept names that exist in the parent — registration
// is local-only.
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

// UnregisterServer closes the client if one exists, removes the server
// from the registry, and prunes its tool-index entries. Unknown names
// are no-ops; already-closed clients are not an error.
func (r *RegistryImpl) UnregisterServer(name string) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("registry is closed")
	}
	client, hadClient := r.clients[name]
	delete(r.clients, name)
	delete(r.servers, name)
	for tool, owner := range r.toolIndex {
		if owner == name {
			delete(r.toolIndex, tool)
		}
	}
	r.mu.Unlock()
	if hadClient {
		_ = client.Close()
		r.releaseProcessSlot()
	}
	return nil
}

// GetClient returns an active client for the given server name. For
// child registries (Fork), if the name resolves only to the parent,
// the parent's client is returned — preserving connection sharing for
// static servers across all per-run forks.
func (r *RegistryImpl) GetClient(ctx context.Context, serverName string) (Client, error) {
	// Check if client exists and is alive (local first).
	if client, err := r.tryGetExistingClient(serverName); err != nil {
		return nil, err
	} else if client != nil {
		return client, nil
	}

	// If the name is registered locally, create a new local client.
	r.mu.RLock()
	_, hasLocal := r.servers[serverName]
	r.mu.RUnlock()
	if hasLocal {
		return r.createNewClient(ctx, serverName)
	}

	// Fall through to parent. Parent owns the client; child shares it.
	if r.parent != nil {
		return r.parent.GetClient(ctx, serverName)
	}
	return nil, fmt.Errorf("server %s not registered", serverName)
}

// tryGetExistingClient attempts to return an existing alive client from
// this registry's local state. Returns (nil, nil) when no local client
// exists (caller decides whether to create one or fall through to a
// parent registry). Errors are reserved for the closed-registry case.
func (r *RegistryImpl) tryGetExistingClient(serverName string) (Client, error) {
	r.mu.RLock()
	client, exists := r.clients[serverName]
	closed := r.closed
	r.mu.RUnlock()

	if closed {
		return nil, fmt.Errorf("registry is closed")
	}

	// If client exists and is alive, return it
	if exists && client.IsAlive() {
		return client, nil
	}

	return nil, nil // No existing client, caller decides next step
}

// createNewClient creates and initializes a new client.
// If a process limit is configured, it acquires a semaphore slot before creating the process.
func (r *RegistryImpl) createNewClient(ctx context.Context, serverName string) (Client, error) {
	// Acquire process semaphore before creating a new process
	if err := r.acquireProcessSlot(); err != nil {
		return nil, fmt.Errorf("failed to acquire process slot for %s: %w", serverName, err)
	}

	r.mu.Lock()

	// Double-check after acquiring write lock — another goroutine may have created it
	if client, exists := r.clients[serverName]; exists && client.IsAlive() {
		r.mu.Unlock()
		r.releaseProcessSlot() // We didn't need the slot after all
		return client, nil
	}

	// If we're replacing a dead client, release its slot conceptually
	// (the dead client's slot is already consumed; we acquired a new one above)
	if _, exists := r.clients[serverName]; exists {
		// Old client was dead — release the extra slot since we're replacing, not adding
		r.releaseProcessSlot()
	}

	config := r.servers[serverName]
	r.mu.Unlock()

	// Create and initialize new client (outside the lock to avoid holding it during I/O)
	newClient := r.newClientFunc(config)
	if _, err := newClient.Initialize(ctx); err != nil {
		r.releaseProcessSlot()
		return nil, fmt.Errorf("failed to initialize MCP server %s: %w", serverName, err)
	}

	r.mu.Lock()
	// Final check — if someone else beat us, close our client and use theirs
	if client, exists := r.clients[serverName]; exists && client.IsAlive() {
		r.mu.Unlock()
		_ = newClient.Close()
		r.releaseProcessSlot()
		return client, nil
	}
	r.clients[serverName] = newClient

	// Refresh tool index for this server
	if err := r.refreshToolIndexForServer(ctx, serverName, newClient); err != nil {
		// Log error but don't fail - client is still usable
		logger.Warn("Failed to refresh tool index", "server", serverName, "error", err)
	}
	r.mu.Unlock()

	return newClient, nil
}

// acquireProcessSlot attempts to acquire a process slot from the semaphore.
// Returns nil immediately if no process limit is configured.
// Returns ErrMaxProcessesReached if all slots are occupied.
func (r *RegistryImpl) acquireProcessSlot() error {
	if r.processSem == nil {
		return nil // No limit configured
	}

	select {
	case r.processSem <- struct{}{}:
		return nil
	default:
		// Semaphore is full — return error immediately instead of blocking
		return ErrMaxProcessesReached
	}
}

// releaseProcessSlot releases a process slot back to the semaphore.
// No-op if no process limit is configured.
func (r *RegistryImpl) releaseProcessSlot() {
	if r.processSem == nil {
		return
	}
	select {
	case <-r.processSem:
	default:
		// Should not happen, but don't block
	}
}

// ActiveProcessCount returns the number of active MCP processes.
// Returns -1 if no process limit is configured.
func (r *RegistryImpl) ActiveProcessCount() int {
	if r.processSem == nil {
		r.mu.RLock()
		count := len(r.clients)
		r.mu.RUnlock()
		return count
	}
	return len(r.processSem)
}

// GetClientForTool returns the client that provides the specified tool.
// Child registries (Fork) check their own tool index first; if the tool
// is not owned by a locally-registered server, the lookup falls through
// to the parent's tool index.
func (r *RegistryImpl) GetClientForTool(ctx context.Context, toolName string) (Client, error) {
	r.mu.RLock()
	serverName, exists := r.toolIndex[toolName]
	r.mu.RUnlock()

	if exists {
		return r.GetClient(ctx, serverName)
	}

	// Try refreshing local tool index before falling through.
	if err := r.refreshAllToolIndices(ctx); err != nil {
		return nil, fmt.Errorf("tool %s not found and refresh failed: %w", toolName, err)
	}
	r.mu.RLock()
	serverName, exists = r.toolIndex[toolName]
	r.mu.RUnlock()
	if exists {
		return r.GetClient(ctx, serverName)
	}

	// Fall through to parent — the tool may be owned by a parent-registered
	// (static) server.
	if r.parent != nil {
		return r.parent.GetClientForTool(ctx, toolName)
	}
	return nil, fmt.Errorf("tool %s not found in any MCP server", toolName)
}

// ListServers returns all registered server names. For child registries
// (produced by Fork), this is the union of local and parent entries —
// child names override parent names with the same key.
func (r *RegistryImpl) ListServers() []string {
	r.mu.RLock()
	seen := make(map[string]struct{}, len(r.servers))
	for name := range r.servers {
		seen[name] = struct{}{}
	}
	r.mu.RUnlock()

	if r.parent != nil {
		for _, name := range r.parent.ListServers() {
			seen[name] = struct{}{}
		}
	}

	servers := make([]string, 0, len(seen))
	for name := range seen {
		servers = append(servers, name)
	}
	return servers
}

// GetServerConfig returns the configuration for a registered server.
// Child registries fall through to the parent when the name is not
// registered locally.
func (r *RegistryImpl) GetServerConfig(serverName string) (ServerConfig, bool) {
	r.mu.RLock()
	cfg, ok := r.servers[serverName]
	r.mu.RUnlock()
	if ok {
		return cfg, true
	}
	if r.parent != nil {
		return r.parent.GetServerConfig(serverName)
	}
	return ServerConfig{}, false
}

// ListAllTools returns all tools from all connected servers
func (r *RegistryImpl) ListAllTools(ctx context.Context) (map[string][]Tool, error) {
	serverNames := r.getServerNames()

	result := make(map[string][]Tool)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(serverNames))

	// Query all servers in parallel
	for _, name := range serverNames {
		wg.Add(1)
		go func(serverName string) {
			defer wg.Done()
			r.fetchServerTools(ctx, serverName, result, &mu, errChan)
		}(name)
	}

	wg.Wait()
	close(errChan)

	// Collect first error if any
	var firstErr error
	for err := range errChan {
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil && len(result) == 0 {
		return nil, firstErr
	}

	return result, nil
}

// getServerNames returns a copy of all registered server names
func (r *RegistryImpl) getServerNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	serverNames := make([]string, 0, len(r.servers))
	for name := range r.servers {
		serverNames = append(serverNames, name)
	}
	return serverNames
}

// fetchServerTools fetches tools from a server and updates the result map
func (r *RegistryImpl) fetchServerTools(
	ctx context.Context,
	serverName string,
	result map[string][]Tool,
	mu *sync.Mutex,
	errChan chan<- error,
) {
	client, err := r.GetClient(ctx, serverName)
	if err != nil {
		errChan <- fmt.Errorf("failed to get client for %s: %w", serverName, err)
		return
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		errChan <- fmt.Errorf("failed to list tools for %s: %w", serverName, err)
		return
	}

	r.updateToolsResult(serverName, tools, result, mu)
}

// updateToolsResult updates the result map and tool index.
// It acquires both the external result mutex and r.mu to protect the shared toolIndex.
func (r *RegistryImpl) updateToolsResult(serverName string, tools []Tool, result map[string][]Tool, mu *sync.Mutex) {
	mu.Lock()
	result[serverName] = tools
	mu.Unlock()

	r.mu.Lock()
	for _, tool := range tools {
		r.toolIndex[tool.Name] = serverName
	}
	r.mu.Unlock()
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
	clientCount := len(r.clients)
	for name, client := range r.clients {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close client %s: %w", name, err)
		}
	}

	// Release all process slots
	for i := 0; i < clientCount; i++ {
		r.releaseProcessSlot()
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

// ServerConfigData holds MCP server configuration matching config.MCPServerConfig.
// Kept in field-for-field sync with ServerConfig; adding a field here that
// isn't on ServerConfig (or vice versa) breaks the direct conversion used in
// NewRegistryWithServers.
type ServerConfigData struct {
	Name          string
	Command       string
	Args          []string
	Env           map[string]string
	WorkingDir    string
	URL           string
	Headers       map[string]string
	TransportName Transport
	TimeoutMs     int
	ToolFilter    *ToolFilter
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

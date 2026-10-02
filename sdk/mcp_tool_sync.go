package sdk

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// Retry schedule for a server whose tools could not be listed: the delay
// starts at mcpRetryBase and grows by mcpRetryBackoff up to mcpRetryMax.
// Each attempt is bounded by mcpRetryAttemptTimeout.
const (
	mcpRetryBase           = time.Second
	mcpRetryBackoff        = 2
	mcpRetryMax            = time.Minute
	mcpRetryAttemptTimeout = 30 * time.Second
)

// mcpToolSync keeps the mcp__ tool descriptors in step with the servers'
// tool lists: when a server reports a change, and when a server whose tools
// could not be listed answers a retry. A conversation and its forks share
// one MCP registry but each has its own tool registry, so every one that
// has registered MCP tools is updated.
type mcpToolSync struct {
	servers   mcp.Registry
	retryBase time.Duration
	retryMax  time.Duration

	mu         sync.Mutex
	registries map[*tools.Registry]struct{}
	retrying   map[string]bool
	stop       chan struct{}
	stopped    bool
}

func newMCPToolSync(servers mcp.Registry) *mcpToolSync {
	return &mcpToolSync{
		servers:    servers,
		retryBase:  mcpRetryBase,
		retryMax:   mcpRetryMax,
		registries: make(map[*tools.Registry]struct{}),
		retrying:   make(map[string]bool),
		stop:       make(chan struct{}),
	}
}

// retry lists serverName's tools again in the background until the server
// answers, then registers them in every tracked registry. The pipeline is
// built once, so without this a server that could not be listed when the
// conversation opened would have no tools for the conversation's life.
func (s *mcpToolSync) retry(serverName string) {
	s.mu.Lock()
	if s.stopped || s.retrying[serverName] {
		s.mu.Unlock()
		return
	}
	s.retrying[serverName] = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.retrying, serverName)
			s.mu.Unlock()
		}()
		delay := s.retryBase
		for attempt := 1; ; attempt++ {
			timer := time.NewTimer(delay)
			select {
			case <-s.stop:
				timer.Stop()
				return
			case <-timer.C:
			}
			serverTools, err := s.list(serverName)
			if err == nil {
				logger.Info("mcp tools registered after an earlier failure to list them",
					"server", serverName, "attempt", attempt, "tools", len(serverTools))
				s.toolsChanged(serverName, serverTools)
				return
			}
			logger.Debug("mcp tools still cannot be listed; retrying", "server", serverName, "attempt", attempt, "error", err)
			delay = min(delay*mcpRetryBackoff, s.retryMax)
		}
	}()
}

// list asks one server for its tools.
func (s *mcpToolSync) list(serverName string) ([]mcp.Tool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpRetryAttemptTimeout)
	defer cancel()
	client, err := s.servers.GetClient(ctx, serverName)
	if err != nil {
		return nil, err
	}
	return client.ListTools(ctx)
}

// close stops retries, when the MCP registry is closed.
func (s *mcpToolSync) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped {
		s.stopped = true
		close(s.stop)
	}
}

// track adds a tool registry whose MCP tools have been registered.
func (s *mcpToolSync) track(reg *tools.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registries[reg] = struct{}{}
}

// untrack stops updating a tool registry, when its conversation closes.
func (s *mcpToolSync) untrack(reg *tools.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.registries, reg)
}

// toolsChanged is the MCP registry's OnToolsChanged: it replaces the
// server's descriptors in every tracked tool registry. The provider stage
// reads the registry on each build, so the next turn offers the new set.
func (s *mcpToolSync) toolsChanged(serverName string, serverTools []mcp.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for reg := range s.registries {
		registerMCPServerTools(reg, s.servers, serverName, serverTools)
	}
}

// mcpToolName is the name a server's tool is registered under.
func mcpToolName(serverName, toolName string) string {
	return fmt.Sprintf("mcp__%s__%s", serverName, toolName)
}

// registerMCPServerTools makes reg's descriptors for serverName match
// serverTools, after the server's ToolFilter: new tools are registered,
// changed ones replaced, and ones the server no longer lists removed.
func registerMCPServerTools(reg *tools.Registry, servers mcp.Registry, serverName string, serverTools []mcp.Tool) {
	var filter *mcp.ToolFilter
	if cfg, ok := servers.GetServerConfig(serverName); ok {
		filter = cfg.ToolFilter
	}

	listed := make(map[string]bool, len(serverTools))
	for i := range serverTools {
		tool := &serverTools[i]
		if filter != nil && !filter.Includes(tool.Name) {
			continue
		}
		name := mcpToolName(serverName, tool.Name)
		listed[name] = true
		// The runtime MCPExecutor strips the namespace and looks up the
		// owning server via mcp.Registry.toolIndex.
		_ = reg.Register(&tools.ToolDescriptor{
			Name:        name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			// The executor checks structured results against it.
			OutputSchema: tool.OutputSchema,
			Mode:         "mcp",
		})
	}

	prefix := mcpToolName(serverName, "")
	var gone []string
	reg.IterateTools(func(name string, _ *tools.ToolDescriptor) {
		if strings.HasPrefix(name, prefix) && !listed[name] {
			gone = append(gone, name)
		}
	})
	for _, name := range gone {
		reg.Unregister(name)
	}
}

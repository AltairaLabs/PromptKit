package sdk

import (
	"fmt"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// mcpToolSync keeps the mcp__ tool descriptors in step with the servers'
// tool lists when a server reports a change. A conversation and its forks
// share one MCP registry but each has its own tool registry, so every one
// that has registered MCP tools is updated.
type mcpToolSync struct {
	servers mcp.Registry

	mu         sync.Mutex
	registries map[*tools.Registry]struct{}
}

func newMCPToolSync(servers mcp.Registry) *mcpToolSync {
	return &mcpToolSync{servers: servers, registries: make(map[*tools.Registry]struct{})}
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

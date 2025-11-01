// Package mcp provides Model Context Protocol (MCP) configuration and registry setup for Arena.
//
// This package bridges Arena's configuration system with the runtime MCP registry,
// allowing test scenarios to use MCP servers for external tool integration.
//
// It handles:
//   - Loading MCP server configurations from Arena config files
//   - Creating and populating MCP registries
//   - Validating MCP server definitions
//
// Example usage:
//
//	registry, err := mcp.NewRegistryFromConfig(arenaConfig)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Use registry with pipeline...
package mcp

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
)

// NewRegistryFromConfig creates a registry from a config object.
// It registers all MCP servers defined in the configuration.
// Returns an empty registry if no servers are configured.
func NewRegistryFromConfig(cfg *config.Config) (*mcp.RegistryImpl, error) {
	registry := mcp.NewRegistry()

	for _, serverCfg := range cfg.MCPServers {
		if err := registry.RegisterServer(mcp.ServerConfig{
			Name:    serverCfg.Name,
			Command: serverCfg.Command,
			Args:    serverCfg.Args,
			Env:     serverCfg.Env,
		}); err != nil {
			return nil, fmt.Errorf("failed to register MCP server %s: %w", serverCfg.Name, err)
		}
	}

	return registry, nil
}

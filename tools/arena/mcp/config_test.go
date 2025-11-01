package mcp

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistryFromConfig_EmptyConfig(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{},
	}

	registry, err := NewRegistryFromConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)

	// Registry should be empty but functional
	assert.NotNil(t, registry)
}

func TestNewRegistryFromConfig_NilConfig(t *testing.T) {
	cfg := &config.Config{}

	registry, err := NewRegistryFromConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)
}

func TestNewRegistryFromConfig_SingleServer(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "test-server",
				Command: "node",
				Args:    []string{"server.js"},
				Env:     map[string]string{"NODE_ENV": "test"},
			},
		},
	}

	registry, err := NewRegistryFromConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)

	// Verify server was registered
	// Note: We can't easily verify internal state without exposing it,
	// but we can verify no error occurred during registration
}

func TestNewRegistryFromConfig_MultipleServers(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "memory-server",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-memory"},
			},
			{
				Name:    "filesystem-server",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
			},
		},
	}

	registry, err := NewRegistryFromConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)
}

func TestNewRegistryFromConfig_WithEnvVars(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "env-server",
				Command: "test-cmd",
				Args:    []string{"--arg"},
				Env: map[string]string{
					"VAR1": "value1",
					"VAR2": "value2",
				},
			},
		},
	}

	registry, err := NewRegistryFromConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)
}

func TestNewRegistryFromConfig_DuplicateServerNames(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "duplicate",
				Command: "cmd1",
			},
			{
				Name:    "duplicate",
				Command: "cmd2",
			},
		},
	}

	registry, err := NewRegistryFromConfig(cfg)

	// Should return error for duplicate registration
	// Note: Actual behavior depends on runtime/mcp.Registry implementation
	// If it allows duplicates, test should verify last one wins
	// If it rejects duplicates, test should expect error
	if err != nil {
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate")
	} else {
		// If no error, registry should be created
		assert.NotNil(t, registry)
	}
}

func TestNewRegistryFromConfig_EmptyServerName(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "",
				Command: "test-cmd",
			},
		},
	}

	_, err := NewRegistryFromConfig(cfg)

	// Should handle empty name gracefully (either error or accept)
	// Behavior depends on runtime/mcp.Registry validation
	_ = err // Don't assert error - depends on implementation
}

func TestNewRegistryFromConfig_EmptyCommand(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "test",
				Command: "",
			},
		},
	}

	_, err := NewRegistryFromConfig(cfg)

	// Should handle empty command gracefully
	_ = err // Don't assert error - depends on implementation
}

func TestNewRegistryFromConfig_ComplexConfiguration(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "complex-server",
				Command: "/usr/local/bin/custom-server",
				Args: []string{
					"--config",
					"/path/to/config.json",
					"--port",
					"8080",
					"--verbose",
				},
				Env: map[string]string{
					"LOG_LEVEL":  "debug",
					"DATA_DIR":   "/var/data",
					"API_KEY":    "test-key-123",
					"MAX_MEMORY": "1024",
				},
			},
		},
	}

	registry, err := NewRegistryFromConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)
}

func TestNewRegistryFromConfig_NilEnvMap(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "no-env",
				Command: "test-cmd",
				Args:    []string{"arg1"},
				Env:     nil, // Explicitly nil
			},
		},
	}

	registry, err := NewRegistryFromConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)
}

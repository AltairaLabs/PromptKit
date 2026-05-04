package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticMCPEndpointResolver(t *testing.T) {
	r := &StaticMCPEndpointResolver{
		URL:     "http://gateway",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}
	ep := r.Resolve("anything")
	assert.Equal(t, "http://gateway", ep.URL)
	assert.Equal(t, "Bearer x", ep.Headers["Authorization"])
}

func TestMapMCPEndpointResolver(t *testing.T) {
	r := &MapMCPEndpointResolver{
		Endpoints: map[string]MCPEndpoint{
			"codegen": {URL: "http://cg.svc:8080"},
			"fs":      {URL: "http://fs.svc:8080", Headers: map[string]string{"X-Tenant": "acme"}},
		},
	}
	assert.Equal(t, "http://cg.svc:8080", r.Resolve("codegen").URL)
	assert.Equal(t, "acme", r.Resolve("fs").Headers["X-Tenant"])
	assert.Empty(t, r.Resolve("unknown").URL, "unknown name returns zero endpoint")

	var nilResolver *MapMCPEndpointResolver
	assert.Empty(t, nilResolver.Resolve("anything").URL, "nil resolver is safe")
}

func TestNewMCPServerByName(t *testing.T) {
	b := NewMCPServerByName("codegen")
	cfg := b.Build()
	assert.Equal(t, "codegen", cfg.Name)
	assert.Empty(t, cfg.Command, "name-only builder must not set Command")
	assert.Empty(t, cfg.URL, "name-only builder must not set URL")
}

func TestResolveMCPEndpoint_StaticURLPassesThrough(t *testing.T) {
	cfg := &mcp.ServerConfig{Name: "x", URL: "http://existing"}
	r := &StaticMCPEndpointResolver{URL: "http://resolver-shouldnt-fire"}
	require.NoError(t, resolveMCPEndpoint(cfg, r))
	assert.Equal(t, "http://existing", cfg.URL, "resolver must not overwrite a static URL")
}

func TestResolveMCPEndpoint_StdioCommandPassesThrough(t *testing.T) {
	cfg := &mcp.ServerConfig{Name: "x", Command: "npx"}
	r := &StaticMCPEndpointResolver{URL: "http://resolver-shouldnt-fire"}
	require.NoError(t, resolveMCPEndpoint(cfg, r))
	assert.Empty(t, cfg.URL)
	assert.Equal(t, "npx", cfg.Command)
}

func TestResolveMCPEndpoint_NameOnlyFillsURLAndHeaders(t *testing.T) {
	cfg := &mcp.ServerConfig{Name: "codegen"}
	r := &StaticMCPEndpointResolver{
		URL:     "http://sandbox.svc:8080",
		Headers: map[string]string{"Authorization": "Bearer t"},
	}
	require.NoError(t, resolveMCPEndpoint(cfg, r))
	assert.Equal(t, "http://sandbox.svc:8080", cfg.URL)
	assert.Equal(t, "Bearer t", cfg.Headers["Authorization"])
}

func TestResolveMCPEndpoint_NameOnlyMergesIntoExistingHeaders(t *testing.T) {
	cfg := &mcp.ServerConfig{
		Name:    "codegen",
		Headers: map[string]string{"X-Existing": "preserved"},
	}
	r := &StaticMCPEndpointResolver{
		URL:     "http://sandbox.svc:8080",
		Headers: map[string]string{"Authorization": "Bearer t"},
	}
	require.NoError(t, resolveMCPEndpoint(cfg, r))
	assert.Equal(t, "preserved", cfg.Headers["X-Existing"])
	assert.Equal(t, "Bearer t", cfg.Headers["Authorization"])
}

func TestResolveMCPEndpoint_NameOnlyWithoutResolverErrors(t *testing.T) {
	cfg := &mcp.ServerConfig{Name: "codegen"}
	err := resolveMCPEndpoint(cfg, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithMCPEndpoints")
}

func TestResolveMCPEndpoint_ResolverReturningEmptyURLErrors(t *testing.T) {
	cfg := &mcp.ServerConfig{Name: "codegen"}
	r := &MapMCPEndpointResolver{Endpoints: map[string]MCPEndpoint{}} // unknown name
	err := resolveMCPEndpoint(cfg, r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no URL")
}

func TestWithMCPEndpoints_StoresResolverOnConfig(t *testing.T) {
	r := &StaticMCPEndpointResolver{URL: "http://x"}
	c := &config{}
	require.NoError(t, WithMCPEndpoints(r)(c))
	assert.Same(t, r, c.mcpEndpointResolver)
}

func TestInitMCPRegistry_NameOnlyWithResolver(t *testing.T) {
	conv := &Conversation{}
	cfg := &config{
		mcpServers: []mcp.ServerConfig{
			{Name: "codegen"}, // name-only, will be resolved
		},
		mcpEndpointResolver: &StaticMCPEndpointResolver{URL: "http://resolved"},
	}
	require.NoError(t, initMCPRegistry(conv, cfg))
	require.NotNil(t, conv.mcpRegistry)
	got, ok := conv.mcpRegistry.(*mcp.RegistryImpl).GetServerConfig("codegen")
	require.True(t, ok)
	assert.Equal(t, "http://resolved", got.URL)
}

func TestInitMCPRegistry_NameOnlyWithoutResolverErrors(t *testing.T) {
	conv := &Conversation{}
	cfg := &config{
		mcpServers: []mcp.ServerConfig{{Name: "codegen"}},
	}
	err := initMCPRegistry(conv, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP server \"codegen\"")
}

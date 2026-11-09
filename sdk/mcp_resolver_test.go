package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	gosdk "github.com/modelcontextprotocol/go-sdk/mcp"
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

// writeMCPTestPack writes a minimal pack file usable by Open() in tests
// that need the full conversation init path (e.g. covering the
// initMCPRegistry hoist into Open and OpenDuplex).
func writeMCPTestPack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	packFile := filepath.Join(dir, "test.pack.json")
	packContent := `{
        "name": "test-pack",
        "version": "v1",
        "prompts": {
            "main": {"system_template": "You are a helpful assistant."}
        }
    }`
	require.NoError(t, os.WriteFile(packFile, []byte(packContent), 0o644))
	return packFile
}

// TestOpen_NameOnlyMCPWithoutResolverErrors covers the Open() path
// where initMCPRegistry now runs before pipeline construction and
// surfaces a clear error when a name-only server has no resolver.
func TestOpen_NameOnlyMCPWithoutResolverErrors(t *testing.T) {
	packFile := writeMCPTestPack(t)
	provider := mock.NewProvider("mock", "mock-model", false)

	_, err := Open(packFile, "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithMCPServer(NewMCPServerByName("codegen")),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP server \"codegen\"")
}

// TestOpen_NameOnlyMCPWithResolverSucceeds drives the caller's path for a
// name-only MCP server: the resolver's URL and headers are what Open connects
// with, and the server's tools land in the conversation's tool registry under
// the server's name.
func TestOpen_NameOnlyMCPWithResolverSucceeds(t *testing.T) {
	server := gosdk.NewServer(&gosdk.Implementation{Name: "codegen", Version: "1"}, nil)
	gosdk.AddTool(server, &gosdk.Tool{Name: "generate"},
		func(context.Context, *gosdk.CallToolRequest, struct{}) (*gosdk.CallToolResult, any, error) {
			return &gosdk.CallToolResult{}, nil, nil
		})
	var mu sync.Mutex
	var auths []string
	handler := gosdk.NewStreamableHTTPHandler(func(*http.Request) *gosdk.Server { return server }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conv, err := Open(writeMCPTestPack(t), "main",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithMCPServer(NewMCPServerByName("codegen")),
		WithMCPEndpoints(&StaticMCPEndpointResolver{
			URL:     srv.URL,
			Headers: map[string]string{"Authorization": "Bearer t"},
		}),
	)
	require.NoError(t, err)
	defer conv.Close()

	assert.Equal(t, []string{"mcp__codegen__generate"}, mcpNames(conv.ToolRegistry()))
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, auths, "Open reached the resolved URL")
	for _, a := range auths {
		assert.Equal(t, "Bearer t", a, "every request carries the resolver's headers")
	}
}

// TestOpenDuplex_NameOnlyMCPWithoutResolverErrors covers the duplex
// path's MCP-init hoist. With no resolver configured, OpenDuplex must
// surface the MCP error before the streaming-support check — the
// reorder keeps Open() and OpenDuplex() validating the same surface.
func TestOpenDuplex_NameOnlyMCPWithoutResolverErrors(t *testing.T) {
	packFile := writeMCPTestPack(t)
	provider := mock.NewProvider("mock", "mock-model", false)

	_, err := OpenDuplex(packFile, "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithMCPServer(NewMCPServerByName("codegen")),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP server \"codegen\"",
		"MCP-init error must surface before the streaming-support check")
}

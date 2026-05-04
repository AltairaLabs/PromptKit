package sdk

// MCPEndpoint is the live coordinates a host hands the SDK so an MCP
// server declared by name can be reached. URL is the base URL the
// runtime/mcp client will dial; Headers are attached to every request
// (typical use: bearer auth, tenancy hints).
type MCPEndpoint struct {
	URL     string
	Headers map[string]string
}

// MCPEndpointResolver maps an MCP server name to a live endpoint.
//
// The SDK calls Resolve at conversation open for any server declared by
// name (without a static URL or stdio Command) so the host — Omnia, a
// dev-only docker shim, a custom orchestrator — can decide where the
// server actually lives. Pack/agent authors stay oblivious to
// provisioning concerns.
//
// Mirrors the A2A [EndpointResolver] interface: pack declares by name,
// host injects the lookup, SDK plugs the result into descriptors at
// construction time.
//
// An empty URL signals "no endpoint available" — the SDK surfaces a
// clear error to the caller. Resolvers that genuinely have no opinion
// for an unknown name should return [MCPEndpoint]{}.
type MCPEndpointResolver interface {
	Resolve(serverName string) MCPEndpoint
}

// StaticMCPEndpointResolver returns the same endpoint for every name.
// Useful when all MCP traffic flows through a single gateway or when a
// dev loop runs one local sandbox.
type StaticMCPEndpointResolver struct {
	URL     string
	Headers map[string]string
}

// Resolve returns the static endpoint for any server name.
func (r *StaticMCPEndpointResolver) Resolve(_ string) MCPEndpoint {
	return MCPEndpoint{URL: r.URL, Headers: r.Headers}
}

// MapMCPEndpointResolver maps each MCP server name to a specific
// endpoint. Names not in the map resolve to [MCPEndpoint]{}, which the
// SDK treats as an error.
type MapMCPEndpointResolver struct {
	Endpoints map[string]MCPEndpoint
}

// Resolve returns the endpoint for the given name, or the zero value
// when the name is not in the map.
func (r *MapMCPEndpointResolver) Resolve(serverName string) MCPEndpoint {
	if r == nil || r.Endpoints == nil {
		return MCPEndpoint{}
	}
	return r.Endpoints[serverName]
}

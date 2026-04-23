// Package mcpsource defines the MCPSource interface and registry for
// host-pluggable MCP server provisioning. An MCPSource is responsible for
// standing up an MCP endpoint (starting a container, reaching into a remote
// service, etc.) and returning its URL + Headers. Arena opens sources at
// scope boundaries (run / scenario / session) and feeds the result into
// the existing runtime/mcp registry via its URL/Headers fields.
package mcpsource

import (
	"context"
	"fmt"
	"io"
)

// Scope is the lifecycle boundary at which a source is opened and closed.
type Scope string

// Known scope values. ScopeRun opens once per arena run; ScopeScenario once
// per scenario; ScopeSession once per session (repetition) within a scenario.
const (
	ScopeRun      Scope = "run"
	ScopeScenario Scope = "scenario"
	ScopeSession  Scope = "session"
)

// Valid reports whether s is one of the known scope values.
func (s Scope) Valid() bool {
	switch s {
	case ScopeRun, ScopeScenario, ScopeSession:
		return true
	}
	return false
}

// ParseScope converts a YAML string to a Scope, erroring on unknown values.
func ParseScope(raw string) (Scope, error) {
	s := Scope(raw)
	if !s.Valid() {
		return "", fmt.Errorf("mcpsource: unknown scope %q (want run|scenario|session)", raw)
	}
	return s, nil
}

// MCPConn carries the coordinates arena needs to construct an mcp.Client.
// Fed into mcp.ServerConfig{URL, Headers} in the existing code path.
type MCPConn struct {
	URL     string
	Headers map[string]string
}

// MCPSource opens an MCP endpoint with caller-supplied args.
//
// Implementations validate their own args schema; arena treats args as
// opaque and only expands scenario template variables before calling Open.
//
// The returned io.Closer is called exactly once when the scope ends. A
// source that needs no teardown returns io.NopCloser(nil).
type MCPSource interface {
	Open(ctx context.Context, args map[string]any) (MCPConn, io.Closer, error)
}

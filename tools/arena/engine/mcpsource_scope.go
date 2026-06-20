package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
)

// discoveryTimeout bounds the per-server tools/list call made after a
// source opens. The MCP client itself may also enforce a shorter timeout
// based on the server's TimeoutMs.
const discoveryTimeout = 30 * time.Second

// openEntry tracks one open (scope, instance, server-name) triple so
// CloseAll can find and tear it down.
type openEntry struct {
	serverName  string
	closer      io.Closer
	containerID string // backing container ID when the source runs one (else "")
}

// mcpSourceScope manages per-scope open/close of source-backed MCP entries.
// The zero value is not usable; use newMCPSourceScope.
type mcpSourceScope struct {
	registry mcp.Registry
	// toolRegistry, when non-nil, receives MCP tool descriptors discovered
	// for each server opened by this scope. Production wires this through
	// newMCPSourceScopeWithTools; lifecycle-only unit tests pass nil.
	toolRegistry *tools.Registry

	mu    sync.Mutex
	opens map[string][]openEntry // keyed by "<scope>|<instanceID>"
}

func newMCPSourceScope(registry mcp.Registry) *mcpSourceScope {
	return newMCPSourceScopeWithTools(registry, nil)
}

func newMCPSourceScopeWithTools(registry mcp.Registry, toolReg *tools.Registry) *mcpSourceScope {
	return &mcpSourceScope{
		registry:     registry,
		toolRegistry: toolReg,
		opens:        map[string][]openEntry{},
	}
}

func scopeKey(scope mcpsource.Scope, instanceID string) string {
	return string(scope) + "|" + instanceID
}

// OpenAll opens every entry whose Scope matches. Scenario variables are
// expanded into SourceArgs, and (if any skill sources are provided) mount
// entries are injected. Partial failures roll back any opens already
// performed in this call.
func (m *mcpSourceScope) OpenAll(
	ctx context.Context,
	scope mcpsource.Scope,
	instanceID string,
	scenarioVars map[string]string,
	skillSources []prompt.SkillSourceConfig,
	entries []config.MCPServerConfig,
) error {
	var opened []openEntry
	rollback := func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = opened[i].closer.Close()
			_ = m.registry.UnregisterServer(opened[i].serverName)
		}
	}

	mounts := buildSkillMounts(skillSources)

	for i := range entries {
		entry := entries[i] // local copy; we may mutate SourceArgs below
		if entry.Source == "" || string(scope) != entry.Scope {
			continue
		}
		prepareEntryArgs(&entry, scenarioVars, mounts)
		oe, err := m.openOne(ctx, &entry)
		if err != nil {
			rollback()
			return err
		}
		opened = append(opened, oe)
	}

	if len(opened) == 0 {
		return nil
	}
	m.mu.Lock()
	key := scopeKey(scope, instanceID)
	m.opens[key] = append(m.opens[key], opened...)
	m.mu.Unlock()
	return nil
}

// prepareEntryArgs expands scenario templates in entry.SourceArgs and
// injects skill mounts. Mutates entry in place (caller passes a local copy).
func prepareEntryArgs(entry *config.MCPServerConfig, scenarioVars map[string]string, mounts []map[string]any) {
	args := expandArgs(entry.SourceArgs, scenarioVars)
	if args == nil && len(mounts) > 0 {
		args = map[string]any{}
	}
	entry.SourceArgs = args
	injectMountsIntoArgs(entry, mounts)
}

// openOne looks up the named source, calls Open, registers the resulting
// URL+Headers with the MCP registry, and (when a toolRegistry is wired)
// discovers and registers the server's tools so the runtime can dispatch
// to them.
func (m *mcpSourceScope) openOne(ctx context.Context, entry *config.MCPServerConfig) (openEntry, error) {
	src, ok := mcpsource.LookupMCPSource(entry.Source)
	if !ok {
		return openEntry{}, fmt.Errorf(
			"mcp server %q: unknown source %q (registered: %s)",
			entry.Name, entry.Source, strings.Join(mcpsource.ListMCPSources(), ", "),
		)
	}
	conn, closer, err := src.Open(ctx, entry.SourceArgs)
	if err != nil {
		return openEntry{}, fmt.Errorf("mcp server %q: source %q Open failed: %w", entry.Name, entry.Source, err)
	}
	serverCfg := mcp.ServerConfig{
		Name:      entry.Name,
		URL:       conn.URL,
		Headers:   conn.Headers,
		TimeoutMs: entry.TimeoutMs,
	}
	if entry.ToolFilter != nil {
		serverCfg.ToolFilter = &mcp.ToolFilter{
			Allowlist: entry.ToolFilter.Allowlist,
			Blocklist: entry.ToolFilter.Blocklist,
		}
	}
	if err := m.registry.RegisterServer(serverCfg); err != nil {
		_ = closer.Close()
		return openEntry{}, fmt.Errorf("mcp server %q: register after Open failed: %w", entry.Name, err)
	}
	if err := m.discoverAndRegisterServerTools(ctx, entry.Name); err != nil {
		_ = m.registry.UnregisterServer(entry.Name)
		_ = closer.Close()
		return openEntry{}, fmt.Errorf("mcp server %q: discover tools after Open failed: %w", entry.Name, err)
	}
	return openEntry{serverName: entry.Name, closer: closer, containerID: conn.ContainerID}, nil
}

// containerIDs returns the backing container ID for every currently-open source
// that has one, keyed by server name. Surfaced into SessionEvent metadata so
// session hooks can reach the sandbox container. Returns nil when none.
func (m *mcpSourceScope) containerIDs() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for _, entries := range m.opens {
		for _, e := range entries {
			if e.containerID != "" {
				out[e.serverName] = e.containerID
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// discoverAndRegisterServerTools lists tools from the just-registered MCP
// server and adds each as a ToolDescriptor in the tools.Registry under the
// qualified name "mcp__<server>__<tool>". This mirrors the construction-time
// discovery done by discoverAndRegisterMCPTools, but for one server at a
// time so it can run after a source-backed open.
//
// When the toolRegistry is nil (lifecycle-only tests) discovery is skipped.
func (m *mcpSourceScope) discoverAndRegisterServerTools(ctx context.Context, serverName string) error {
	if m.toolRegistry == nil {
		return nil
	}

	// Bound the lookup so a slow server can't stall scope open.
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	client, err := m.registry.GetClient(ctx, serverName)
	if err != nil {
		return fmt.Errorf("get MCP client: %w", err)
	}

	mcpTools, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("list MCP tools: %w", err)
	}

	// Source-backed servers register tools under their raw MCP name
	// (e.g. "Read") rather than the namespaced "mcp__<server>__<tool>"
	// form used by static MCP discovery in builder_integration.go. The
	// design goal is that a sandbox is "just another MCP server" from the
	// pack author's perspective — allowed_tools and assertions reference
	// "Read", not "mcp__sandbox__Read".
	//
	// Routing still works because MCPExecutor.callMCPTool runs each tool
	// name through mcpRawToolName (a no-op when there's no namespace) and
	// looks up the owning server via mcp.Registry.toolIndex, which is
	// keyed by raw tool name.
	for _, t := range mcpTools {
		if filter, fok := m.lookupToolFilter(serverName); fok && filter != nil && !filter.Includes(t.Name) {
			continue
		}
		desc := &tools.ToolDescriptor{
			Name:         t.Name,
			Description:  t.Description,
			InputSchema:  t.InputSchema,
			OutputSchema: mcpOutputSchema,
			Mode:         "mcp",
		}
		if err := m.toolRegistry.Register(desc); err != nil {
			return fmt.Errorf("register tool %q: %w", t.Name, err)
		}
	}
	logger.Info("Discovered MCP tools (source-backed)", "server", serverName, "tools", len(mcpTools))
	return nil
}

// lookupToolFilter returns the configured tool filter for a server, if any.
func (m *mcpSourceScope) lookupToolFilter(serverName string) (*mcp.ToolFilter, bool) {
	cfg, ok := m.registry.GetServerConfig(serverName)
	if !ok {
		return nil, false
	}
	return cfg.ToolFilter, true
}

// mcpOutputSchema is the generic shape MCP tool calls return — kept here
// so the source-backed discovery path matches the construction-time path
// in builder_integration.go.
var mcpOutputSchema = json.RawMessage(`{
    "type": "object",
    "properties": {
        "content": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "type": {"type": "string"},
                    "text": {"type": "string"},
                    "data": {"type": "string"},
                    "mimeType": {"type": "string"},
                    "uri": {"type": "string"}
                },
                "required": ["type"]
            }
        },
        "isError": {"type": "boolean"}
    },
    "required": ["content"]
}`)

// CloseAll tears down every entry opened for (scope, instanceID). Close
// errors are collected and returned; registry entries are removed regardless.
func (m *mcpSourceScope) CloseAll(scope mcpsource.Scope, instanceID string) []error {
	m.mu.Lock()
	key := scopeKey(scope, instanceID)
	entries := m.opens[key]
	delete(m.opens, key)
	m.mu.Unlock()

	var errs []error
	for i := len(entries) - 1; i >= 0; i-- {
		if err := entries[i].closer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %q: %w", entries[i].serverName, err))
		}
		_ = m.registry.UnregisterServer(entries[i].serverName)
	}
	return errs
}

// --- Args templating (Task 5 responsibilities) ---

func expandArgs(args map[string]any, vars map[string]string) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = expandArgValue(v, vars)
	}
	return out
}

func expandArgValue(v any, vars map[string]string) any {
	switch x := v.(type) {
	case string:
		return expandString(x, vars)
	case map[string]any:
		return expandArgs(x, vars)
	case []any:
		out := make([]any, len(x))
		for i, elem := range x {
			out[i] = expandArgValue(elem, vars)
		}
		return out
	default:
		return v
	}
}

func expandString(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{scenario."+k+"}}", v)
	}
	return s
}

// --- Skill-mount injection (Task 9 responsibilities) ---

// buildSkillMounts converts skill sources to mount entries suitable for a
// MCPSource args map. Each skill dir is mounted at /skills/<name> read-only.
// Sources without a name or directory are skipped (inline skills produce no
// mount — they're passed to the runtime via a different path).
func buildSkillMounts(sources []prompt.SkillSourceConfig) []map[string]any {
	if len(sources) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(sources))
	for i := range sources {
		s := sources[i]
		dir := s.EffectiveDir()
		if dir == "" || s.Name == "" {
			continue
		}
		out = append(out, map[string]any{
			"source":   dir, // absolute host path
			"target":   "/skills/" + s.Name,
			"readonly": true,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// injectMountsIntoArgs adds mounts to entry.SourceArgs under the "mounts"
// key, preserving existing args. Empty mounts is a no-op (the key is not
// added), so sources that don't understand mounts don't see it.
func injectMountsIntoArgs(entry *config.MCPServerConfig, mounts []map[string]any) {
	if len(mounts) == 0 {
		return
	}
	if entry.SourceArgs == nil {
		entry.SourceArgs = map[string]any{}
	}
	entry.SourceArgs["mounts"] = mounts
}

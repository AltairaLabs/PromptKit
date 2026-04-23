package engine

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
)

// openEntry tracks one open (scope, instance, server-name) triple so
// CloseAll can find and tear it down.
type openEntry struct {
	serverName string
	closer     io.Closer
}

// mcpSourceScope manages per-scope open/close of source-backed MCP entries.
// The zero value is not usable; use newMCPSourceScope.
type mcpSourceScope struct {
	registry mcp.Registry

	mu    sync.Mutex
	opens map[string][]openEntry // keyed by "<scope>|<instanceID>"
}

func newMCPSourceScope(registry mcp.Registry) *mcpSourceScope {
	return &mcpSourceScope{
		registry: registry,
		opens:    map[string][]openEntry{},
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

// openOne looks up the named source, calls Open, and registers the
// resulting URL+Headers with the MCP registry.
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
	if err := m.registry.RegisterServer(mcp.ServerConfig{
		Name:      entry.Name,
		URL:       conn.URL,
		Headers:   conn.Headers,
		TimeoutMs: entry.TimeoutMs,
	}); err != nil {
		_ = closer.Close()
		return openEntry{}, fmt.Errorf("mcp server %q: register after Open failed: %w", entry.Name, err)
	}
	return openEntry{serverName: entry.Name, closer: closer}, nil
}

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

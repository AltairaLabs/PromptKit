package stage

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// buildToolRegistry registers the given tool names with a trivial object schema.
func buildToolRegistry(t *testing.T, names ...string) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	for _, n := range names {
		if err := reg.Register(&tools.ToolDescriptor{
			Name: n, Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`),
		}); err != nil {
			t.Fatalf("register %s: %v", n, err)
		}
	}
	return reg
}

// sortedDescriptorNames returns the descriptor names, sorted, for set comparison.
func sortedDescriptorNames(s *ProviderStage, allowed []string, excluded map[string]bool) []string {
	names := descriptorNames(s.collectProviderDescriptors(allowed, excluded))
	sort.Strings(names)
	return names
}

// MCP tools are system-namespaced but must be surfaced only when named in
// allowed_tools (issue #1578). Capability tools (a2a/workflow/memory/skill/media)
// stay auto-surfaced. Wildcard allowed_tools entries expand against the registry.
func TestCollectProviderDescriptors_AllowedTools(t *testing.T) {
	// Registry: two MCP servers (kb, fs), one capability tool, one user tool.
	const (
		kbCreate = "mcp__kb__create"
		kbRecall = "mcp__kb__recall"
		fsRead   = "mcp__fs__read"
		a2aTool  = "a2a__agent__forecast" // implicit capability
		userTool = "get_weather"          // plain user tool
	)
	newStage := func() *ProviderStage {
		return &ProviderStage{
			toolRegistry: buildToolRegistry(t, kbCreate, kbRecall, fsRead, a2aTool, userTool),
		}
	}

	tests := []struct {
		name     string
		allowed  []string
		excluded map[string]bool
		want     []string
	}{
		{
			name:    "no allowed_tools surfaces only implicit capability tools",
			allowed: nil,
			want:    []string{a2aTool},
		},
		{
			name:    "exact MCP name surfaces just that tool plus implicit",
			allowed: []string{kbCreate},
			want:    []string{a2aTool, kbCreate},
		},
		{
			name:    "server wildcard surfaces that server only",
			allowed: []string{"mcp__kb__*"},
			want:    []string{a2aTool, kbCreate, kbRecall},
		},
		{
			name:    "namespace wildcard surfaces all MCP tools",
			allowed: []string{"mcp__*"},
			want:    []string{a2aTool, fsRead, kbCreate, kbRecall},
		},
		{
			name:     "excluded tool is dropped from wildcard expansion",
			allowed:  []string{"mcp__kb__*"},
			excluded: map[string]bool{kbRecall: true},
			want:     []string{a2aTool, kbCreate},
		},
		{
			name:    "user tool surfaces only when explicitly allowed",
			allowed: []string{userTool},
			want:    []string{a2aTool, userTool},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sortedDescriptorNames(newStage(), tt.allowed, tt.excluded)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// Wildcard expansion walks the registry via IterateTools (randomized map order),
// so it carries the same cache-stability risk as the auto-surface loop: the tools
// array is part of the provider's cached prefix, and a varying order busts prompt
// caching (see reference: tool order breaks prompt caching). The shared sort keeps
// it stable; this guards against a future change to the wildcard path losing it.
func TestCollectProviderDescriptors_WildcardDeterministic(t *testing.T) {
	reg := buildToolRegistry(t,
		"mcp__kb__zebra", "mcp__kb__alpha", "mcp__kb__mike", "mcp__kb__bravo",
		"mcp__kb__yankee", "mcp__kb__delta", "mcp__kb__oscar", "mcp__kb__charlie",
	)
	s := &ProviderStage{toolRegistry: reg}

	first := descriptorNames(s.collectProviderDescriptors([]string{"mcp__kb__*"}, nil))
	if len(first) != 8 {
		t.Fatalf("expected 8 descriptors, got %d: %v", len(first), first)
	}
	if !sort.StringsAreSorted(first) {
		t.Fatalf("wildcard-expanded descriptors must be sorted (cache stability), got %v", first)
	}
	for i := range 100 {
		got := descriptorNames(s.collectProviderDescriptors([]string{"mcp__kb__*"}, nil))
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("non-deterministic wildcard order on call %d:\n got %v\nwant %v", i, got, first)
		}
	}
}

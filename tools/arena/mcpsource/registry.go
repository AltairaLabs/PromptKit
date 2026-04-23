package mcpsource

import (
	"fmt"
	"sort"
	"sync"
)

var (
	regMu      sync.RWMutex
	registered = map[string]MCPSource{}
)

// RegisterMCPSource adds a named source implementation. Called from init()
// blocks in source packages (e.g., mcpsource/docker). Panics on duplicate
// registration to surface programmer errors at startup.
func RegisterMCPSource(name string, src MCPSource) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := registered[name]; exists {
		panic(fmt.Sprintf("mcpsource: duplicate registration for %q", name))
	}
	registered[name] = src
}

// LookupMCPSource returns the source registered under name, if any.
func LookupMCPSource(name string) (MCPSource, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	s, ok := registered[name]
	return s, ok
}

// ListMCPSources returns the names of every registered source, sorted.
// Used for "unknown source" error messages.
func ListMCPSources() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	names := make([]string, 0, len(registered))
	for n := range registered {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

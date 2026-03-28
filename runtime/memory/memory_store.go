package memory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// InMemoryStore is a test/development memory store. Substring matching
// for retrieval, no vector search. Not for production use.
type InMemoryStore struct {
	mu       sync.RWMutex
	memories map[string][]*Memory // keyed by scope hash
	nextID   int
}

// NewInMemoryStore creates a new in-memory store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		memories: make(map[string][]*Memory),
	}
}

// Save stores a memory. Generates an ID if empty.
func (s *InMemoryStore) Save(_ context.Context, m *Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m.ID == "" {
		s.nextID++
		m.ID = fmt.Sprintf("mem_%d", s.nextID)
	}
	now := time.Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.AccessedAt = now

	key := scopeKey(m.Scope)
	s.memories[key] = append(s.memories[key], m)
	logger.Debug("memory saved", "id", m.ID, "type", m.Type, "scope", key)
	return nil
}

// Retrieve searches memories by substring matching on content.
func (s *InMemoryStore) Retrieve(
	_ context.Context, scope map[string]string, query string, opts RetrieveOptions,
) ([]*Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := scopeKey(scope)
	var results []*Memory
	queryLower := strings.ToLower(query)

	for _, m := range s.memories[key] {
		if !matchesFilters(m, opts.Types, opts.MinConfidence) {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(m.Content), queryLower) {
			m.AccessedAt = time.Now()
			results = append(results, m)
		}
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// List returns memories matching the scope and filters.
func (s *InMemoryStore) List(
	_ context.Context, scope map[string]string, opts ListOptions,
) ([]*Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := scopeKey(scope)
	all := s.memories[key]

	var results []*Memory
	for _, m := range all {
		if len(opts.Types) > 0 && !containsType(opts.Types, m.Type) {
			continue
		}
		results = append(results, m)
	}

	if opts.Offset > 0 && opts.Offset < len(results) {
		results = results[opts.Offset:]
	} else if opts.Offset >= len(results) {
		return nil, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Delete removes a specific memory by ID.
func (s *InMemoryStore) Delete(_ context.Context, scope map[string]string, memoryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := scopeKey(scope)
	mems := s.memories[key]
	for i, m := range mems {
		if m.ID == memoryID {
			s.memories[key] = append(mems[:i], mems[i+1:]...)
			return nil
		}
	}
	return nil // not found is not an error
}

// DeleteAll removes all memories for a scope.
func (s *InMemoryStore) DeleteAll(_ context.Context, scope map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.memories, scopeKey(scope))
	return nil
}

// scopeKey creates a deterministic hash from scope map for map keying.
func scopeKey(scope map[string]string) string {
	if len(scope) == 0 {
		return "_global"
	}
	h := sha256.New()
	// Sort keys for determinism
	keys := make([]string, 0, len(scope))
	for k := range scope {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%s=%s;", k, scope[k])
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func matchesFilters(m *Memory, types []string, minConfidence float64) bool {
	if len(types) > 0 && !containsType(types, m.Type) {
		return false
	}
	if minConfidence > 0 && m.Confidence < minConfidence {
		return false
	}
	return true
}

func containsType(types []string, t string) bool {
	for _, typ := range types {
		if typ == t {
			return true
		}
	}
	return false
}

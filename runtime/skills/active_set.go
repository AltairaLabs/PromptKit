package skills

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ActiveSet is one conversation's set of active skills, plus the filter
// restricting what that conversation may activate.
//
// It is the *caller's* state. An [Executor] is a catalog and a policy — what
// skills exist, what the pack ceiling is, what activating one would grant — and
// holds no conversation state of its own, because a single Executor is
// registered by name into a tools.Registry that keeps exactly one executor per
// name. A host running concurrent conversations over one registry gives each
// conversation its own ActiveSet and passes it per call; see [WithActiveSet]
// for the tool-dispatch path.
//
// An ActiveSet is safe for concurrent use.
type ActiveSet struct {
	mu     sync.RWMutex
	active map[string]activeEntry
	filter string
}

// activeEntry is an active skill plus the path filters match against. The path
// is resolved once, at activation, so an ActiveSet can re-apply a filter
// without consulting the Executor that produced it.
type activeEntry struct {
	skill   *Skill
	relPath string
}

// NewActiveSet returns an empty, unfiltered ActiveSet.
func NewActiveSet() *ActiveSet {
	return &ActiveSet{active: make(map[string]activeEntry)}
}

// Filter returns the glob or path restricting which skills this set may
// activate. Empty means all.
func (s *ActiveSet) Filter() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.filter
}

// SetFilter restricts which skills this set may activate. Empty means all;
// "none" (case-insensitive) disables all; otherwise the filter is a glob or a
// skill directory path (see matchesFilter). Skills already active that no
// longer match are deactivated, and their names returned, sorted.
//
// Only this set is affected — a workflow state narrowing one conversation's
// skills cannot narrow another's.
func (s *ActiveSet) SetFilter(glob string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.filter = glob

	var deactivated []string
	for name, entry := range s.active {
		if !matchesFilter(entry.relPath, glob) {
			delete(s.active, name)
			deactivated = append(deactivated, name)
		}
	}
	sort.Strings(deactivated)
	return deactivated
}

// Names returns the active skill names, sorted.
func (s *ActiveSet) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.active))
	for name := range s.active {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Instructions returns the instructions of the named skill when it is active
// in this set, and whether it is. It only reads the set: unlike
// [Executor.ActivateIn], it never activates a skill that is not active.
func (s *ActiveSet) Instructions(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.active[name]
	if !ok {
		return "", false
	}
	return entry.skill.Instructions, true
}

// Len returns the number of active skills.
func (s *ActiveSet) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.active)
}

// matchesFilter reports whether a skill path satisfies a filter.
// An empty filter admits everything; "none" (case-insensitive) admits nothing.
//
// A filter is either a glob, matched against the skill's path with
// filepath.Match, or — as the PromptPack spec defines a workflow state's
// `skills` — a path to a skill directory or SKILL.md file, which admits the
// skills at or under that directory. A relative path resolves the way skill
// sources do (see [Registry.ListForDir]), so "./skills/billing" in a state
// names the same directory as "./skills/billing" in the pack's skills list.
func matchesFilter(relPath, filter string) bool {
	if filter == "" {
		return true
	}
	if strings.EqualFold(filter, "none") {
		return false
	}
	if matched, _ := filepath.Match(filter, relPath); matched {
		return true
	}
	if relPath == "" || strings.ContainsAny(filter, "*?[") {
		return false
	}
	return pathUnder(relPath, filter)
}

// pathUnder reports whether skillPath is dir, or lies beneath it by whole
// path segments. dir may name a skill's SKILL.md file, meaning its directory.
func pathUnder(skillPath, dir string) bool {
	if filepath.Base(dir) == skillMDFile {
		dir = filepath.Dir(dir)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(skillPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

type activeSetKey struct{}

// WithActiveSet attaches a conversation's ActiveSet to the context. The
// [ToolExecutor] reads it on each Execute and activates into it, falling back
// to the Executor's own set when absent.
//
// This is what lets a single skill executor, registered once into a shared
// tools.Registry, serve concurrent conversations without their activations
// reaching each other. It mirrors tools.WithMCPRegistry, which exists for the
// same reason.
func WithActiveSet(ctx context.Context, set *ActiveSet) context.Context {
	if set == nil {
		return ctx
	}
	return context.WithValue(ctx, activeSetKey{}, set)
}

// ActiveSetFromContext returns the ActiveSet attached by [WithActiveSet],
// or nil when the context carries none.
func ActiveSetFromContext(ctx context.Context) *ActiveSet {
	if ctx == nil {
		return nil
	}
	set, _ := ctx.Value(activeSetKey{}).(*ActiveSet)
	return set
}

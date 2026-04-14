package skills

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/selection"
)

type skillFilterKey struct{}

// WithSkillFilter returns a context with the given skill filter glob pattern.
// The ToolExecutor reads this to apply per-run filtering in concurrent scenarios.
func WithSkillFilter(ctx context.Context, filter string) context.Context {
	return context.WithValue(ctx, skillFilterKey{}, filter)
}

// SkillFilterFromContext returns the skill filter from context, or "" if not set.
func SkillFilterFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(skillFilterKey{}).(string); ok {
		return v
	}
	return ""
}

// Executor manages the skill activation lifecycle and provides
// functions that implement the skill__ namespaced tools.
type Executor struct {
	registry    *Registry
	selector    SkillSelector
	newSelector selection.Selector
	active      map[string]*Skill // currently active skills, keyed by name
	packTools   []string          // all tools declared in the pack (the ceiling)
	packSet     map[string]bool   // pre-built set from packTools for O(1) membership checks
	maxActive   int               // max concurrent active skills (0 = unlimited)
	filter      string            // glob pattern restricting activatable skills
	configDir   string            // base directory for computing relative skill paths
	mu          sync.RWMutex
}

// ExecutorConfig configures the skill executor.
type ExecutorConfig struct {
	Registry    *Registry
	Selector    SkillSelector      // nil = ModelDrivenSelector
	NewSelector selection.Selector // optional external selector for narrowing the skill index
	PackTools   []string           // all tools declared in the pack
	MaxActive   int                // 0 = unlimited
	ConfigDir   string             // base directory for computing relative skill paths in filters
}

// NewExecutor creates a new Executor from the given configuration.
// If Selector is nil, a ModelDrivenSelector is used.
func NewExecutor(cfg ExecutorConfig) *Executor {
	sel := cfg.Selector
	if sel == nil {
		sel = NewModelDrivenSelector()
	}
	// Pre-build the packSet map once so intersectPackTools avoids
	// rebuilding it on every call.
	packSet := make(map[string]bool, len(cfg.PackTools))
	for _, t := range cfg.PackTools {
		packSet[t] = true
	}
	return &Executor{
		registry:    cfg.Registry,
		selector:    sel,
		newSelector: cfg.NewSelector,
		active:      make(map[string]*Skill),
		packTools:   cfg.PackTools,
		packSet:     packSet,
		maxActive:   cfg.MaxActive,
		configDir:   cfg.ConfigDir,
	}
}

// SetNewSelector replaces the external selector after construction.
// It is safe to call between Send()s; callers typically wire this at
// capability-registration time based on RuntimeConfig.
func (e *Executor) SetNewSelector(sel selection.Selector) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.newSelector = sel
}

// Activate loads a skill's instructions and returns them.
// It extends the active tool set with the skill's allowed-tools (capped by pack tools).
// If the skill is already active, it returns the instructions again (idempotent).
// Returns error if skill not found or at max active limit.
func (e *Executor) Activate(name string) (instructions string, addedTools []string, retErr error) {
	return e.ActivateWithFilter(name, e.filter)
}

// ActivateWithFilter is like Activate but applies the given filter instead of the
// executor's default filter. This supports per-run filtering in concurrent scenarios.
func (e *Executor) ActivateWithFilter(name, filter string) (instructions string, addedTools []string, retErr error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// If already active, return instructions idempotently.
	if s, ok := e.active[name]; ok {
		tools := e.intersectPackTools(s.AllowedTools)
		return s.Instructions, tools, nil
	}

	// Load skill to get its path for filter check.
	skill, loadErr := e.registry.Load(name)
	if loadErr != nil {
		return "", nil, fmt.Errorf("activating skill %q: %w", name, loadErr)
	}

	// Check filter.
	if !e.matchesFilterWith(skill, filter) {
		return "", nil, fmt.Errorf(
			"skill %q is not available in the current state (filter: %q)",
			name, filter,
		)
	}

	// Check max active limit.
	if e.maxActive > 0 && len(e.active) >= e.maxActive {
		return "", nil, fmt.Errorf(
			"cannot activate skill %q: max active limit (%d) reached",
			name, e.maxActive,
		)
	}

	tools := e.intersectPackTools(skill.AllowedTools)
	e.active[name] = skill

	return skill.Instructions, tools, nil
}

// SetFilter sets a glob pattern that restricts which skills can be activated.
// Empty string means all skills are available. "none" (case-insensitive) disables all.
// Returns names of skills that were deactivated because they no longer match.
func (e *Executor) SetFilter(glob string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.filter = glob

	var deactivated []string
	for name, skill := range e.active {
		if !e.matchesFilterWith(skill, glob) {
			delete(e.active, name)
			deactivated = append(deactivated, name)
		}
	}
	return deactivated
}

// matchesFilterWith checks whether a skill's relative path matches the given filter.
// Must be called with e.mu held.
func (e *Executor) matchesFilterWith(skill *Skill, filter string) bool {
	if filter == "" {
		return true
	}
	if strings.EqualFold(filter, "none") {
		return false
	}
	relPath := skill.Path
	if e.configDir != "" {
		if rel, err := filepath.Rel(e.configDir, skill.Path); err == nil {
			relPath = rel
		}
	}
	matched, _ := filepath.Match(filter, relPath)
	return matched
}

// Deactivate removes a skill from the active set.
// Returns the tools that should be removed from the active tool set.
// A tool is only removed if no other active skill also needs it.
func (e *Executor) Deactivate(name string) (removedTools []string, retErr error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	skill, ok := e.active[name]
	if !ok {
		return nil, fmt.Errorf("skill %q is not active", name)
	}

	// Compute the skill's effective tools (intersection with pack).
	skillTools := e.intersectPackTools(skill.AllowedTools)

	// Remove from active map first so we don't count it.
	delete(e.active, name)

	// Build set of tools still needed by remaining active skills.
	stillNeeded := make(map[string]bool)
	for _, other := range e.active {
		for _, t := range e.intersectPackTools(other.AllowedTools) {
			stillNeeded[t] = true
		}
	}

	// Only remove tools not needed by any remaining active skill.
	var removed []string
	for _, t := range skillTools {
		if !stillNeeded[t] {
			removed = append(removed, t)
		}
	}
	sort.Strings(removed)
	return removed, nil
}

// ReadResource reads a file from within a skill's directory.
// Delegates to the underlying registry.
func (e *Executor) ReadResource(skillName, path string) ([]byte, error) {
	return e.registry.ReadResource(skillName, path)
}

// SkillIndex returns the Phase 1 skill index string for inclusion in
// the skill__activate tool description. It is a thin wrapper over
// SkillIndexFiltered with no query, so it preserves historical
// behavior when no external selector is configured.
func (e *Executor) SkillIndex(skillsDir string) string {
	return e.SkillIndexFiltered(context.Background(), "", skillsDir)
}

// SkillIndexFiltered returns the skill index string, optionally
// narrowed by the configured external Selector. When no selector is
// configured, or the selector returns an error or an empty result,
// the full eligible set is returned — PromptKit never crashes a
// conversation because selection failed.
//
// query is the current-turn context the selector may rank against;
// when empty, the selector is skipped entirely (there's nothing to
// rank against, so all eligible skills surface).
func (e *Executor) SkillIndexFiltered(ctx context.Context, query, skillsDir string) string {
	var skills []SkillMetadata
	if skillsDir != "" {
		skills = e.registry.ListForDir(skillsDir)
	} else {
		skills = e.registry.List()
	}

	if len(skills) == 0 {
		return "No skills available."
	}

	skills = e.applyNewSelector(ctx, query, skills)

	var sb strings.Builder
	sb.WriteString("Available skills:")
	for _, s := range skills {
		sb.WriteString("\n- ")
		sb.WriteString(s.Name)
		sb.WriteString(": ")
		sb.WriteString(s.Description)
	}
	return sb.String()
}

// applyNewSelector narrows the metadata slice to the IDs returned by
// the configured external selector. Falls through to the input slice
// on any failure path (nil selector, empty query, selector error,
// empty result).
func (e *Executor) applyNewSelector(ctx context.Context, query string, skills []SkillMetadata) []SkillMetadata {
	e.mu.RLock()
	sel := e.newSelector
	e.mu.RUnlock()
	if sel == nil || query == "" {
		return skills
	}

	candidates := make([]selection.Candidate, 0, len(skills))
	for _, s := range skills {
		candidates = append(candidates, selection.Candidate{
			ID:          s.Name,
			Name:        s.Name,
			Description: s.Description,
		})
	}

	ids, err := sel.Select(ctx, selection.Query{Text: query, Kind: "skill"}, candidates)
	if err != nil || len(ids) == 0 {
		return skills
	}

	keep := make(map[string]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}
	out := make([]SkillMetadata, 0, len(ids))
	for _, s := range skills {
		if keep[s.Name] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return skills
	}
	return out
}

// ActiveSkills returns the names of currently active skills, sorted.
func (e *Executor) ActiveSkills() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	names := make([]string, 0, len(e.active))
	for name := range e.active {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ActiveTools returns the aggregate set of tools added by all active skills
// (each capped by pack tools). The result is deduplicated and sorted.
func (e *Executor) ActiveTools() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	seen := make(map[string]bool)
	for _, skill := range e.active {
		for _, t := range e.intersectPackTools(skill.AllowedTools) {
			seen[t] = true
		}
	}

	tools := make([]string, 0, len(seen))
	for t := range seen {
		tools = append(tools, t)
	}
	sort.Strings(tools)
	return tools
}

// intersectPackTools returns elements of skillTools that also appear in packTools.
// Uses the pre-built packSet for O(1) membership checks instead of rebuilding
// the map on every call.
// Must be called with e.mu held (read or write).
func (e *Executor) intersectPackTools(skillTools []string) []string {
	if len(e.packSet) == 0 || len(skillTools) == 0 {
		return nil
	}

	var result []string
	for _, t := range skillTools {
		if e.packSet[t] {
			result = append(result, t)
		}
	}
	sort.Strings(result)
	return result
}

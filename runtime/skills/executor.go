package skills

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Executor manages the skill activation lifecycle and provides
// functions that implement the skill__ namespaced tools.
type Executor struct {
	registry  *Registry
	selector  SkillSelector
	active    map[string]*Skill // currently active skills, keyed by name
	packTools []string          // all tools declared in the pack (the ceiling)
	maxActive int               // max concurrent active skills (0 = unlimited)
	mu        sync.RWMutex
}

// ExecutorConfig configures the skill executor.
type ExecutorConfig struct {
	Registry  *Registry
	Selector  SkillSelector // nil = ModelDrivenSelector
	PackTools []string      // all tools declared in the pack
	MaxActive int           // 0 = unlimited
}

// NewExecutor creates a new Executor from the given configuration.
// If Selector is nil, a ModelDrivenSelector is used.
func NewExecutor(cfg ExecutorConfig) *Executor {
	sel := cfg.Selector
	if sel == nil {
		sel = NewModelDrivenSelector()
	}
	return &Executor{
		registry:  cfg.Registry,
		selector:  sel,
		active:    make(map[string]*Skill),
		packTools: cfg.PackTools,
		maxActive: cfg.MaxActive,
	}
}

// Activate loads a skill's instructions and returns them.
// It extends the active tool set with the skill's allowed-tools (capped by pack tools).
// If the skill is already active, it returns the instructions again (idempotent).
// Returns error if skill not found or at max active limit.
func (e *Executor) Activate(name string) (instructions string, addedTools []string, retErr error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// If already active, return instructions idempotently.
	if s, ok := e.active[name]; ok {
		tools := e.intersectPackTools(s.AllowedTools)
		return s.Instructions, tools, nil
	}

	// Check max active limit.
	if e.maxActive > 0 && len(e.active) >= e.maxActive {
		return "", nil, fmt.Errorf(
			"cannot activate skill %q: max active limit (%d) reached",
			name, e.maxActive,
		)
	}

	// Load the skill from the registry.
	skill, loadErr := e.registry.Load(name)
	if loadErr != nil {
		return "", nil, fmt.Errorf("activating skill %q: %w", name, loadErr)
	}

	tools := e.intersectPackTools(skill.AllowedTools)
	e.active[name] = skill

	return skill.Instructions, tools, nil
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
// the skill__activate tool description. This is the list of available
// skills with their names and descriptions.
// If skillsDir is non-empty, filters to skills under that directory.
func (e *Executor) SkillIndex(skillsDir string) string {
	var skills []SkillMetadata
	if skillsDir != "" {
		skills = e.registry.ListForDir(skillsDir)
	} else {
		skills = e.registry.List()
	}

	if len(skills) == 0 {
		return "No skills available."
	}

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
// Must be called with e.mu held (read or write).
func (e *Executor) intersectPackTools(skillTools []string) []string {
	if len(e.packTools) == 0 || len(skillTools) == 0 {
		return nil
	}

	packSet := make(map[string]bool, len(e.packTools))
	for _, t := range e.packTools {
		packSet[t] = true
	}

	var result []string
	for _, t := range skillTools {
		if packSet[t] {
			result = append(result, t)
		}
	}
	sort.Strings(result)
	return result
}

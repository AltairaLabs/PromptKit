package skills

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/selection"
)

type skillFilterKey struct{}

// WithSkillFilter returns a context with the given skill filter glob pattern.
//
// Deprecated: use [WithActiveSet] and [ActiveSet.SetFilter]. A filter is one
// half of a conversation's skill state and belongs with the other half; this
// pair only ever covered the filter, and never had a producer anywhere. It is
// retained so existing callers keep compiling and will be removed in v3.
// See AltairaLabs/PromptKit#2011.
func WithSkillFilter(ctx context.Context, filter string) context.Context {
	return context.WithValue(ctx, skillFilterKey{}, filter)
}

// SkillFilterFromContext returns the skill filter from context, or "" if not set.
//
// Deprecated: use [ActiveSetFromContext] and [ActiveSet.Filter].
// See AltairaLabs/PromptKit#2011.
func SkillFilterFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(skillFilterKey{}).(string); ok {
		return v
	}
	return ""
}

// Executor is the skill catalog and the activation policy: what skills
// exist, what the pack ceiling is, whether a filter admits a given skill, and
// what activating one grants.
//
// It holds no per-conversation state. Activation state lives in an [ActiveSet]
// the caller owns and passes in — see [Executor.ActivateIn] — because one
// Executor is registered by name into a tools.Registry that keeps exactly one
// executor per name, so a host running concurrent conversations over a shared
// registry would otherwise share their active skills. The `own` set below
// backs the deprecated stateful methods only.
type Executor struct {
	registry    *Registry
	selector    SkillSelector
	newSelector selection.Selector
	own         *ActiveSet      // backs the deprecated stateful methods only
	packTools   []string        // all tools declared in the pack (the ceiling)
	packSet     map[string]bool // pre-built set from packTools for O(1) membership checks
	maxActive   int             // max concurrent active skills per set (0 = unlimited)
	configDir   string          // base directory for computing relative skill paths
	mu          sync.RWMutex    // guards newSelector only; everything else is immutable
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
		own:         NewActiveSet(),
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

// Activation is the result of activating a skill: the instructions to hand the
// model, and the pack tools the skill adds beyond the prompt's baseline.
type Activation struct {
	Instructions string
	AddedTools   []string
}

// ActivateIn activates a skill into the caller's ActiveSet and returns its
// instructions and the tools it grants (capped by the pack's tools).
//
// Activation is idempotent: activating an already-active skill returns its
// instructions again. It fails when the skill does not exist, when the set's
// filter does not admit it, or when the set is already at the configured
// max-active limit — a limit that applies per set, so one conversation filling
// its quota cannot lock another out.
//
// A nil set activates into the Executor's own set, which is the deprecated
// stateful behavior; pass a real set.
func (e *Executor) ActivateIn(set *ActiveSet, name string) (Activation, error) {
	if set == nil {
		set = e.own
	}

	set.mu.Lock()
	defer set.mu.Unlock()

	// If already active, return instructions idempotently.
	if entry, ok := set.active[name]; ok {
		return Activation{
			Instructions: entry.skill.Instructions,
			AddedTools:   e.intersectPackTools(entry.skill.AllowedTools),
		}, nil
	}

	skill, err := e.registry.Load(name)
	if err != nil {
		return Activation{}, fmt.Errorf("activating skill %q: %w", name, err)
	}

	relPath := e.relPath(skill)
	if !matchesFilter(relPath, set.filter) {
		return Activation{}, fmt.Errorf(
			"skill %q is not available in the current state (filter: %q)",
			name, set.filter,
		)
	}

	if e.maxActive > 0 && len(set.active) >= e.maxActive {
		return Activation{}, fmt.Errorf(
			"cannot activate skill %q: max active limit (%d) reached",
			name, e.maxActive,
		)
	}

	set.active[name] = activeEntry{skill: skill, relPath: relPath}

	return Activation{
		Instructions: skill.Instructions,
		AddedTools:   e.intersectPackTools(skill.AllowedTools),
	}, nil
}

// DeactivateIn removes a skill from the caller's ActiveSet and returns the
// tools to drop. A tool is only dropped when no skill still active in that same
// set needs it.
func (e *Executor) DeactivateIn(set *ActiveSet, name string) (removedTools []string, retErr error) {
	if set == nil {
		set = e.own
	}

	set.mu.Lock()
	defer set.mu.Unlock()

	entry, ok := set.active[name]
	if !ok {
		return nil, fmt.Errorf("skill %q is not active", name)
	}

	skillTools := e.intersectPackTools(entry.skill.AllowedTools)

	// Remove from the active map first so we don't count it as still needed.
	delete(set.active, name)

	stillNeeded := make(map[string]bool)
	for _, other := range set.active {
		for _, t := range e.intersectPackTools(other.skill.AllowedTools) {
			stillNeeded[t] = true
		}
	}

	var removed []string
	for _, t := range skillTools {
		if !stillNeeded[t] {
			removed = append(removed, t)
		}
	}
	sort.Strings(removed)
	return removed, nil
}

// ToolsFor returns the aggregate set of tools granted by the skills active in
// the given set, each capped by the pack's tools. Deduplicated and sorted.
func (e *Executor) ToolsFor(set *ActiveSet) []string {
	if set == nil {
		set = e.own
	}

	set.mu.RLock()
	defer set.mu.RUnlock()

	seen := make(map[string]bool)
	for _, entry := range set.active {
		for _, t := range e.intersectPackTools(entry.skill.AllowedTools) {
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

// SkillsIn returns the names of the skills active in the given set, sorted.
func (e *Executor) SkillsIn(set *ActiveSet) []string {
	if set == nil {
		set = e.own
	}
	return set.Names()
}

// relPath returns the path filters match a skill against: relative to the
// executor's ConfigDir when one is configured, absolute otherwise.
func (e *Executor) relPath(skill *Skill) string {
	if e.configDir == "" {
		return skill.Path
	}
	rel, err := filepath.Rel(e.configDir, skill.Path)
	if err != nil {
		return skill.Path
	}
	return rel
}

// Activate loads a skill's instructions and returns them.
//
// Deprecated: use [Executor.ActivateIn] with the conversation's own
// [ActiveSet]. This form activates into state held on the Executor, which is
// shared by every conversation dispatching through the same tools.Registry
// entry. It will be removed in v3. See AltairaLabs/PromptKit#2011.
func (e *Executor) Activate(name string) (instructions string, addedTools []string, retErr error) {
	act, err := e.ActivateIn(e.own, name)
	return act.Instructions, act.AddedTools, err
}

// ActivateWithFilter is like Activate but applies the given filter instead of
// the executor's default filter.
//
// Deprecated: use [Executor.ActivateIn] with an [ActiveSet] whose filter was
// set by [ActiveSet.SetFilter]. Passing a filter per call covered only half of
// the per-conversation state and left the active set shared. It will be
// removed in v3. See AltairaLabs/PromptKit#2011.
func (e *Executor) ActivateWithFilter(
	name, filter string,
) (instructions string, addedTools []string, retErr error) {
	set := NewActiveSet()
	set.SetFilter(filter)
	// Preserve the historical semantics: activation lands in the executor's own
	// set, but admission is judged against the caller's filter.
	e.own.mu.RLock()
	prior := e.own.filter
	e.own.mu.RUnlock()

	e.own.mu.Lock()
	e.own.filter = filter
	e.own.mu.Unlock()

	act, err := e.ActivateIn(e.own, name)

	e.own.mu.Lock()
	e.own.filter = prior
	e.own.mu.Unlock()

	return act.Instructions, act.AddedTools, err
}

// SetFilter sets a glob pattern that restricts which skills can be activated.
//
// Deprecated: use [ActiveSet.SetFilter] on the conversation's own set. This
// form mutates state shared by every conversation dispatching through the same
// tools.Registry entry. It will be removed in v3.
// See AltairaLabs/PromptKit#2011.
func (e *Executor) SetFilter(glob string) []string {
	return e.own.SetFilter(glob)
}

// Deactivate removes a skill from the active set.
//
// Deprecated: use [Executor.DeactivateIn] with the conversation's own
// [ActiveSet]. It will be removed in v3. See AltairaLabs/PromptKit#2011.
func (e *Executor) Deactivate(name string) (removedTools []string, retErr error) {
	return e.DeactivateIn(e.own, name)
}

// OwnActiveSet returns the set backing the deprecated stateful methods.
//
// Deprecated: exists so a host migrating off [Executor.Activate] can adopt the
// set it was already implicitly using. New code should construct its own with
// [NewActiveSet]. It will be removed in v3.
func (e *Executor) OwnActiveSet() *ActiveSet { return e.own }

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
//
// Deprecated: use [Executor.SkillsIn] with the conversation's own [ActiveSet].
// It will be removed in v3. See AltairaLabs/PromptKit#2011.
func (e *Executor) ActiveSkills() []string {
	return e.own.Names()
}

// ActiveTools returns the aggregate set of tools added by all active skills
// (each capped by pack tools). The result is deduplicated and sorted.
//
// Deprecated: use [Executor.ToolsFor] with the conversation's own [ActiveSet].
// Wiring this into ProviderConfig.ToolGrants from a shared executor grants one
// conversation's skill tools to every other conversation — a permission leak,
// which is what made this the urgent half of AltairaLabs/PromptKit#2011. It
// will be removed in v3.
func (e *Executor) ActiveTools() []string {
	return e.ToolsFor(e.own)
}

// intersectPackTools returns elements of skillTools that also appear in packTools.
// Uses the pre-built packSet for O(1) membership checks instead of rebuilding
// the map on every call. packSet is immutable after construction, so this needs
// no lock.
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

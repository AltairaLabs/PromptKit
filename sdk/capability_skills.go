package sdk

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/selection"
	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// SkillsCapability provides skill activation/deactivation tools to conversations.
// Skills are loaded from directories or inline definitions and can be dynamically
// activated by the LLM via the skill__activate tool.
type SkillsCapability struct {
	sources     []skills.SkillSource
	selector    skills.SkillSelector
	newSelector selection.Selector
	maxActive   int
	executor    *skills.Executor
	packTools   []string
	// preload names the skills to activate into every conversation's own
	// ActiveSet. They used to be activated into the executor at Init, which
	// made them one shared set.
	preload []string

	// initMu serializes Init: a capability shared across opens via
	// WithCapability can be initialized by concurrent opens.
	initMu sync.Mutex
}

// SkillsOption configures a SkillsCapability.
type SkillsOption func(*SkillsCapability)

// WithSkillSelector sets a custom skill selector for filtering available skills.
// It narrows the skill__activate index once per turn, as a selector bound in
// RuntimeConfig does; a RuntimeConfig selector takes precedence over it.
func WithSkillSelector(s skills.SkillSelector) SkillsOption {
	return func(c *SkillsCapability) {
		c.selector = s
	}
}

// WithMaxActiveSkills sets the maximum number of concurrently active skills.
func WithMaxActiveSkills(n int) SkillsOption {
	return func(c *SkillsCapability) {
		c.maxActive = n
	}
}

// NewSkillsCapability creates a new SkillsCapability from the given sources.
func NewSkillsCapability(
	sources []skills.SkillSource, opts ...SkillsOption,
) *SkillsCapability {
	c := &SkillsCapability{
		sources: sources,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// forOpen returns the capability one conversation should use: c itself when
// the open adds nothing to it, otherwise a new, uninitialized capability with
// c's sources and options plus the open's code-added sources and skill
// options. c is never modified, so a host sharing it across opens keeps it as
// it built it (#2398).
//
// An open whose RuntimeConfig binds a skills selector that resolves gets its own
// capability too. The executor fixes its selector when it is built, so a shared
// capability would apply the first open's selector to every later one.
func (c *SkillsCapability) forOpen(extra []skills.SkillSource, cfg *config) *SkillsCapability {
	var added []skills.SkillSource
	for _, src := range extra {
		if !slices.Contains(c.sources, src) && !slices.Contains(added, src) {
			added = append(added, src)
		}
	}
	setsSelector := cfg.skillSelector != nil && c.selector == nil
	setsMaxActive := cfg.maxActiveSkills > 0 && c.maxActive == 0
	bindsRuntimeSelector := cfg.skillsSelectorName != "" && cfg.selectors[cfg.skillsSelectorName] != nil
	if len(added) == 0 && !setsSelector && !setsMaxActive && !bindsRuntimeSelector {
		return c
	}
	own := &SkillsCapability{
		sources:   append(slices.Clone(c.sources), added...),
		selector:  c.selector,
		maxActive: c.maxActive,
	}
	own.applyConfig(cfg)
	return own
}

// applyConfig fills the options the capability was not constructed with from
// the open's config. Called only on a capability this open owns.
func (c *SkillsCapability) applyConfig(cfg *config) {
	if cfg.skillSelector != nil && c.selector == nil {
		c.selector = cfg.skillSelector
	}
	if cfg.maxActiveSkills > 0 && c.maxActive == 0 {
		c.maxActive = cfg.maxActiveSkills
	}
}

// Name returns the capability identifier.
func (c *SkillsCapability) Name() string { return capabilityNameSkills }

// Init discovers skills from sources and creates an executor.
func (c *SkillsCapability) Init(ctx CapabilityContext) error {
	c.initMu.Lock()
	defer c.initMu.Unlock()

	reg := skills.NewRegistry()
	if err := reg.Discover(c.sources); err != nil {
		return fmt.Errorf("skills discovery: %w", err)
	}

	// The ceiling for skill-granted tools is the PACK's declared tools, not
	// the prompt's. The prompt's list is the baseline the model always sees;
	// a skill's allowed-tools extend it on activation, capped by the pack.
	// Using the prompt's list here made every grant a no-op: a skill could
	// only "add" a tool the model already had.
	packTools := make([]string, 0, len(ctx.Pack.Tools))
	for name := range ctx.Pack.Tools {
		packTools = append(packTools, name)
	}
	sort.Strings(packTools)

	// Init runs once per conversation open, and a host may reuse one
	// SkillsCapability across opens via WithCapability. Replacing the executor
	// on a later Init would orphan the ToolExecutor the earlier conversation
	// already registered, so build it once. The executor holds no
	// conversation state, so sharing it is safe — each conversation activates
	// into its own ActiveSet.
	if c.executor != nil {
		if !slices.Equal(c.packTools, packTools) {
			logger.Warn("skills: capability reused across packs with different tools; "+
				"keeping the tool ceiling from the first pack",
				"capability", capabilityNameSkills,
				"ceiling", c.packTools, "ignored", packTools)
		}
		return nil
	}

	// Resolve the external selector by name when RuntimeConfig binds
	// one. Missing names are silently ignored — validation happens at
	// RuntimeConfig load.
	if ctx.SkillsSelectorName != "" {
		if sel, ok := ctx.Selectors[ctx.SkillsSelectorName]; ok {
			c.newSelector = sel
		}
	}
	// A SkillSelector (WithSkillSelector, sdk.WithSkillSelectorOption) runs on
	// the same path, when RuntimeConfig binds no selector of its own.
	if c.newSelector == nil {
		c.newSelector = skills.AdaptSkillSelector(c.selector)
	}

	cfg := skills.ExecutorConfig{
		Registry:    reg,
		Selector:    c.selector,
		NewSelector: c.newSelector,
		PackTools:   packTools,
		MaxActive:   c.maxActive,
	}
	c.executor = skills.NewExecutor(cfg)
	c.packTools = packTools

	// Skills marked preload: true are activated into each conversation's own
	// set by [SkillsCapability.NewActiveSet], not here. Activating them here
	// put them in the executor's set, which every conversation shared.
	//
	// PreloadedSkills is sorted, so which skills lose a MaxActive race is the
	// same on every process start.
	c.preload = make([]string, 0, len(reg.PreloadedSkills()))
	for _, sk := range reg.PreloadedSkills() {
		c.preload = append(c.preload, sk.Name)
	}

	return nil
}

// NewActiveSet returns a fresh ActiveSet for one conversation, with the
// preloaded skills already activated into it.
//
// Preloading is best-effort — a skill that fails here can still be activated
// on first use, so a failure does not abort the open. It is reported, though:
// when the cause is MaxActive the "activate later" recovery does not hold,
// because the limit is as full at first use as it is now.
func (c *SkillsCapability) NewActiveSet() *skills.ActiveSet {
	set := skills.NewActiveSet()
	if c.executor == nil {
		return set
	}
	for _, name := range c.preload {
		if _, err := c.executor.ActivateIn(set, name); err != nil {
			logger.Warn("skills: preload failed", "skill", name, "error", err)
		}
	}
	return set
}

// preloadedInstructions renders the instructions of the preloaded skills that
// are active in set, ready to append to the system prompt. A preloaded skill the
// conversation has since deactivated — or that a workflow state's filter
// dropped — is left out, so its instructions leave the prompt with its tools.
// Empty when there are none.
//
// It only reads set, so a skill deactivated while this runs stays deactivated
// (#2399).
func (c *SkillsCapability) preloadedInstructions(set *skills.ActiveSet) string {
	if c.executor == nil || set == nil || len(c.preload) == 0 {
		return ""
	}
	var sections []string
	for _, name := range c.preload {
		instructions, active := set.Instructions(name)
		if !active || instructions == "" {
			continue
		}
		sections = append(sections, "## Skill: "+name+"\n\n"+instructions)
	}
	return strings.Join(sections, "\n\n")
}

// RegisterTools registers the skill management tools into the registry.
func (c *SkillsCapability) RegisterTools(registry *tools.Registry) {
	if c.executor == nil {
		return
	}

	index := c.executor.SkillIndex("")
	_ = registry.Register(skills.BuildSkillActivateDescriptorWithIndex(index))
	_ = registry.Register(skills.BuildSkillDeactivateDescriptor())
	_ = registry.Register(skills.BuildSkillReadResourceDescriptor())

	registry.RegisterExecutor(skills.NewToolExecutor(c.executor))
}

// Close is a no-op for SkillsCapability.
func (c *SkillsCapability) Close() error { return nil }

// RefreshSkillIndex re-materializes the skill__activate tool's
// description in registry using the configured external Selector, if any,
// ranked against query. When no selector is configured or the query is
// empty, the registered descriptor is left as it is.
//
// A Conversation does not call this: it runs the selector once per Send
// and narrows the index per provider round, to the skills its own
// workflow state admits, without re-registering anything. This remains
// for hosts that drive a tools.Registry directly; the index it writes
// ignores any workflow state's skills filter.
func (c *SkillsCapability) RefreshSkillIndex(ctx context.Context, query string, registry *tools.Registry) {
	if c.executor == nil || registry == nil {
		return
	}
	if c.newSelector == nil || query == "" {
		// Selector bypassed — existing descriptor already holds the
		// full eligible index; avoid redundant re-registration.
		return
	}
	index := c.executor.SkillIndexFiltered(ctx, query, "")
	_ = registry.Register(skills.BuildSkillActivateDescriptorWithIndex(index))
}

// selectSkills runs the external selector, if any, over the skills set
// currently admits, once per user turn. Nil means no narrowing; see
// [skills.Executor.SelectSkillsIn].
func (c *SkillsCapability) selectSkills(ctx context.Context, query string, set *skills.ActiveSet) []string {
	if c.executor == nil {
		return nil
	}
	return c.executor.SelectSkillsIn(ctx, query, set)
}

// describeActivate returns the skill__activate descriptor one conversation's
// round should offer: registered with the full index, narrowed here to the
// skills set's filter admits (and selected names, when a selector chose).
// Nil withholds the tool, when the filter admits no skill at all.
//
// The registered descriptor is shared and never modified; a narrowed index is
// a copy. A description a host replaced with WithToolDescriptorOverride is
// kept as it is when it no longer carries the full index to narrow.
func (c *SkillsCapability) describeActivate(
	d *tools.ToolDescriptor, set *skills.ActiveSet, selected []string,
) *tools.ToolDescriptor {
	if c.executor == nil {
		return d
	}
	index, ok := c.executor.SkillIndexIn(set, selected)
	if !ok {
		return nil
	}
	full := c.executor.SkillIndex("")
	if index == full || !strings.Contains(d.Description, full) {
		return d
	}
	out := *d
	out.Description = strings.Replace(d.Description, full, index, 1)
	return &out
}

// Executor returns the underlying skills executor for testing.
func (c *SkillsCapability) Executor() *skills.Executor { return c.executor }

const capabilityNameSkills = "skills"

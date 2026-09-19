package sdk

import (
	"context"
	"fmt"
	"slices"
	"sort"

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
	// made them one shared set (#2011).
	preload []string
}

// SkillsOption configures a SkillsCapability.
type SkillsOption func(*SkillsCapability)

// WithSkillSelector sets a custom skill selector for filtering available skills.
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

// Name returns the capability identifier.
func (c *SkillsCapability) Name() string { return capabilityNameSkills }

// Init discovers skills from sources and creates an executor.
func (c *SkillsCapability) Init(ctx CapabilityContext) error {
	reg := skills.NewRegistry()
	if err := reg.Discover(c.sources); err != nil {
		return fmt.Errorf("skills discovery: %w", err)
	}

	// The ceiling for skill-granted tools is the PACK's declared tools, not
	// the prompt's. The prompt's list is the baseline the model always sees;
	// a skill's allowed-tools extend it on activation, capped by the pack.
	// Using the prompt's list here made every grant a no-op: a skill could
	// only "add" a tool the model already had (#1957).
	packTools := make([]string, 0, len(ctx.Pack.Tools))
	for name := range ctx.Pack.Tools {
		packTools = append(packTools, name)
	}
	sort.Strings(packTools)

	// Resolve the external selector by name when RuntimeConfig binds
	// one. Missing names are silently ignored — validation happens at
	// RuntimeConfig load.
	if ctx.SkillsSelectorName != "" {
		if sel, ok := ctx.Selectors[ctx.SkillsSelectorName]; ok {
			c.newSelector = sel
		}
	}

	// Init runs once per conversation open, and a host may reuse one
	// SkillsCapability across opens via WithCapability. Replacing the executor
	// on a later Init would orphan the ToolExecutor the earlier conversation
	// already registered, so build it once. The executor holds no
	// conversation state, so sharing it is safe — each conversation activates
	// into its own ActiveSet (#2011).
	if c.executor != nil {
		if !slices.Equal(c.packTools, packTools) {
			logger.Warn("skills: capability reused across packs with different tools; "+
				"keeping the tool ceiling from the first pack",
				"capability", capabilityNameSkills,
				"ceiling", c.packTools, "ignored", packTools)
		}
		return nil
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
// because the limit is just as full at first use as it is now (#1953).
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
// description using the configured external Selector, if any. It is
// called per-Send from the conversation loop so the selector can rank
// against the current user query. When no selector is configured or
// the query is empty, the descriptor is re-registered with the full
// eligible index (same as historical behavior).
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

// Executor returns the underlying skills executor for testing.
func (c *SkillsCapability) Executor() *skills.Executor { return c.executor }

const capabilityNameSkills = "skills"

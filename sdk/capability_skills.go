package sdk

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/selection"
	"github.com/AltairaLabs/PromptKit/runtime/skills"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
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

	// Collect pack tool names from the prompt
	var packTools []string
	if prompt, ok := ctx.Pack.Prompts[ctx.PromptName]; ok {
		packTools = prompt.Tools
	}

	// Resolve the external selector by name when RuntimeConfig binds
	// one. Missing names are silently ignored — validation happens at
	// RuntimeConfig load.
	if ctx.SkillsSelectorName != "" {
		if sel, ok := ctx.Selectors[ctx.SkillsSelectorName]; ok {
			c.newSelector = sel
		}
	}

	cfg := skills.ExecutorConfig{
		Registry:    reg,
		Selector:    c.selector,
		NewSelector: c.newSelector,
		PackTools:   packTools,
		MaxActive:   c.maxActive,
	}
	c.executor = skills.NewExecutor(cfg)

	// Preload skills marked with preload: true.
	// Errors are intentionally ignored: preloading is best-effort and the skill
	// will be activated on first use if preloading fails.
	for _, sk := range reg.PreloadedSkills() {
		_, _, _ = c.executor.Activate(sk.Name)
	}

	return nil
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

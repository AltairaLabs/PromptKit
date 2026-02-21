package sdk

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/skills"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// SkillsCapability provides skill activation/deactivation tools to conversations.
// Skills are loaded from directories or inline definitions and can be dynamically
// activated by the LLM via the skill__activate tool.
type SkillsCapability struct {
	sources   []skills.SkillSource
	selector  skills.SkillSelector
	maxActive int
	executor  *skills.Executor
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

	cfg := skills.ExecutorConfig{
		Registry:  reg,
		Selector:  c.selector,
		PackTools: packTools,
		MaxActive: c.maxActive,
	}
	c.executor = skills.NewExecutor(cfg)

	// Preload skills marked with preload: true
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

// Executor returns the underlying skills executor for testing.
func (c *SkillsCapability) Executor() *skills.Executor { return c.executor }

const capabilityNameSkills = "skills"

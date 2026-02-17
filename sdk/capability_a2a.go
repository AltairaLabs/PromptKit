package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// A2ACapability provides A2A agent tools to conversations.
// It unifies both the bridge path (explicit WithA2ATools) and the pack
// path (agents section in pack) under a single capability.
type A2ACapability struct {
	// bridge path (explicit WithA2ATools)
	bridge *a2a.ToolBridge

	// pack path config
	endpointResolver EndpointResolver
	localExecutor    *LocalAgentExecutor

	// populated during Init
	agentResolver *AgentToolResolver
	prompt        *pack.Prompt
}

// NewA2ACapability creates a new A2ACapability.
func NewA2ACapability() *A2ACapability {
	return &A2ACapability{}
}

// Name returns the capability identifier.
func (c *A2ACapability) Name() string { return "a2a" }

// Init initializes the capability with pack context.
// If the pack has an agents section, it creates an AgentToolResolver.
func (c *A2ACapability) Init(ctx CapabilityContext) error {
	p := ctx.Pack
	if p.Agents != nil && len(p.Agents.Members) > 0 {
		runtimePack := packToRuntimePack(p)
		resolver := NewAgentToolResolver(runtimePack)
		if resolver != nil {
			if c.endpointResolver != nil {
				resolver.SetEndpointResolver(c.endpointResolver)
			}
			c.agentResolver = resolver
		}
	}
	if prompt, ok := p.Prompts[ctx.PromptName]; ok {
		c.prompt = prompt
	}
	return nil
}

// RegisterTools registers A2A tools into the registry.
// Bridge path: registers bridge tool descriptors + A2A executor.
// Pack path: resolves agent tools from prompt tools list + registers executor.
func (c *A2ACapability) RegisterTools(registry *tools.Registry) {
	c.registerBridgeTools(registry)
	c.registerAgentTools(registry)
}

// registerBridgeTools handles the bridge path (explicit WithA2ATools).
func (c *A2ACapability) registerBridgeTools(registry *tools.Registry) {
	if c.bridge == nil {
		return
	}
	for _, td := range c.bridge.GetToolDescriptors() {
		_ = registry.Register(td)
	}
	registry.RegisterExecutor(newA2AExecutor())
}

// registerAgentTools handles the pack path (agents section).
func (c *A2ACapability) registerAgentTools(registry *tools.Registry) {
	if c.agentResolver == nil {
		return
	}
	var toolNames []string
	if c.prompt != nil {
		toolNames = c.prompt.Tools
	}
	descriptors := c.agentResolver.ResolveAgentTools(toolNames)
	if len(descriptors) == 0 {
		return
	}
	for _, td := range descriptors {
		_ = registry.Register(td)
	}
	if c.localExecutor != nil {
		registry.RegisterExecutor(c.localExecutor)
	} else {
		registry.RegisterExecutor(newA2AExecutor())
	}
}

// Close is a no-op for A2ACapability.
func (c *A2ACapability) Close() error { return nil }

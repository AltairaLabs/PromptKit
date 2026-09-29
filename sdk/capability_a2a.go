package sdk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	sdka2a "github.com/AltairaLabs/PromptKit/sdk/v2/internal/a2a"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// A2ACapability provides A2A agent tools to conversations.
// It unifies both the bridge path (explicit WithA2ATools) and the pack
// path (agents section in pack) under a single capability.
type A2ACapability struct {
	// bridge path (explicit WithA2ATools)
	bridge *a2a.ToolBridge

	// builder-based agent bridges (from WithA2AAgent). Unlike the bridge
	// path, these are handed over undiscovered: the capability fetches each
	// agent's card itself (#2087).
	agentBridges []*a2a.ToolBridge
	// agentSettings holds per-bridge discovery settings, keyed by bridge.
	agentSettings map[*a2a.ToolBridge]a2aBridgeSettings
	// discovered records the builder bridges whose card has been fetched, so
	// a successful discovery is never repeated (RegisterAgent appends).
	discovered map[*a2a.ToolBridge]bool
	discoverMu sync.Mutex

	// toolExecutor, when set, runs bridge tools in place of the SDK's own
	// A2A executor (WithA2AToolExecutor).
	toolExecutor tools.Executor

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

// defaultA2ADiscoveryTimeout bounds agent card discovery for a builder agent
// that sets no timeout of its own.
const defaultA2ADiscoveryTimeout = 10 * time.Second

// a2aBridgeSettings are the discovery settings for one builder agent.
type a2aBridgeSettings struct {
	url      string
	timeout  time.Duration
	required bool
}

// Init initializes the capability with pack context.
// If the pack has an agents section, it creates an AgentToolResolver.
// Builder agents (WithA2AAgent) are discovered here, so their skills are
// tools from the first turn. A failed discovery is logged and the conversation
// runs without that agent's tools, unless the agent was marked Required, in
// which case Init fails.
func (c *A2ACapability) Init(ctx CapabilityContext) error {
	if err := c.discoverAgents(); err != nil {
		return err
	}
	p := ctx.Pack
	if p.Agents != nil && len(p.Agents.Members) > 0 {
		resolver := NewAgentToolResolver(p)
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

// registerBridgeTools handles the bridge path (explicit WithA2ATools and WithA2AAgent).
func (c *A2ACapability) registerBridgeTools(registry *tools.Registry) {
	hasTools := false
	if c.bridge != nil {
		for _, td := range c.bridge.GetToolDescriptors() {
			_ = registry.Register(td)
			hasTools = true
		}
	}
	for _, bridge := range c.agentBridges {
		for _, td := range bridge.GetToolDescriptors() {
			_ = registry.Register(td)
			hasTools = true
		}
	}
	if hasTools {
		registry.RegisterExecutor(c.bridgeExecutor())
	}
}

// bridgeExecutor returns the executor bridge tools run on: the host's
// (WithA2AToolExecutor) when set, otherwise the SDK's own.
func (c *A2ACapability) bridgeExecutor() tools.Executor {
	if c.toolExecutor != nil {
		return namedExecutor{Executor: c.toolExecutor, name: nsA2A}
	}
	return sdka2a.NewExecutor()
}

// namedExecutor registers a host executor under the "a2a" name the registry
// routes Mode "a2a" tools to, whatever name the host's executor reports.
type namedExecutor struct {
	tools.Executor
	name string
}

// Name returns the registry name the executor is registered under.
func (e namedExecutor) Name() string { return e.name }

// discoverAgents fetches the agent card of every builder bridge not yet
// discovered and turns its skills into tool descriptors. It returns an error
// only when a Required agent fails; other failures are logged.
func (c *A2ACapability) discoverAgents() error {
	c.discoverMu.Lock()
	defer c.discoverMu.Unlock()
	if c.discovered == nil {
		c.discovered = make(map[*a2a.ToolBridge]bool, len(c.agentBridges))
	}
	for _, bridge := range c.agentBridges {
		if bridge == nil || c.discovered[bridge] {
			continue
		}
		settings := c.agentSettings[bridge]
		timeout := settings.timeout
		if timeout <= 0 {
			timeout = defaultA2ADiscoveryTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		registered, err := bridge.RegisterAgent(ctx)
		cancel()
		if err != nil {
			if settings.required {
				return fmt.Errorf("a2a agent %s: %w", settings.url, err)
			}
			logger.Warn("a2a agent tools not registered: discovery failed; "+
				"the conversation runs without them (mark the agent Required to fail Open instead)",
				"agent", settings.url, "error", err)
			continue
		}
		c.discovered[bridge] = true
		if len(registered) == 0 {
			logger.Warn("a2a agent exposes no tools: its card lists no skills, or the skill filter excluded them all",
				"agent", settings.url)
			continue
		}
		logger.Info("a2a agent tools registered", "agent", settings.url, "tools", len(registered))
	}
	return nil
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
		// The same executor as bridge tools: the host's, when it gave one,
		// so pack agents cannot slip past its policy by replacing it.
		registry.RegisterExecutor(c.bridgeExecutor())
	}
}

// Close is a no-op for A2ACapability.
func (c *A2ACapability) Close() error { return nil }

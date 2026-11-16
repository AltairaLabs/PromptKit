package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdka2a "github.com/AltairaLabs/PromptKit/sdk/v2/internal/a2a"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// A2ACapability provides A2A agent tools to conversations.
// It unifies both the bridge path (explicit WithA2ATools) and the pack
// path (agents section in pack) under a single capability.
type A2ACapability struct {
	// bridge path (explicit WithA2ATools)
	bridges []*a2a.ToolBridge

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

// capabilityNameA2A is the identifier of the A2A capability.
const capabilityNameA2A = "a2a"

// Name returns the capability identifier.
func (c *A2ACapability) Name() string { return capabilityNameA2A }

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
	if err := c.checkDuplicateTools(); err != nil {
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
	for _, bridge := range c.allBridges() {
		for _, td := range bridge.GetToolDescriptors() {
			_ = registry.Register(td)
			hasTools = true
		}
	}
	if hasTools {
		registry.RegisterExecutor(c.bridgeExecutor())
	}
}

// allBridges returns every non-nil bridge: explicit (WithA2ATools) first, then
// builder agents (WithA2AAgent).
func (c *A2ACapability) allBridges() []*a2a.ToolBridge {
	all := make([]*a2a.ToolBridge, 0, len(c.bridges)+len(c.agentBridges))
	for _, b := range slices.Concat(c.bridges, c.agentBridges) {
		if b != nil {
			all = append(all, b)
		}
	}
	return all
}

// bridgeLabel names a bridge in errors: the agent URL when known, else the
// URL on its tool, else its position.
func (c *A2ACapability) bridgeLabel(b *a2a.ToolBridge, td *tools.ToolDescriptor, idx int) string {
	if s, ok := c.agentSettings[b]; ok && s.url != "" {
		return s.url
	}
	if td.A2AConfig != nil && td.A2AConfig.AgentURL != "" {
		return td.A2AConfig.AgentURL
	}
	return fmt.Sprintf("bridge #%d", idx+1)
}

// checkDuplicateTools fails when two bridges register the same tool name, so
// one agent's tools never silently shadow another's.
func (c *A2ACapability) checkDuplicateTools() error {
	owner := map[string]string{}
	for i, b := range c.allBridges() {
		for _, td := range b.GetToolDescriptors() {
			label := c.bridgeLabel(b, td, i)
			if prev, dup := owner[td.Name]; dup {
				return fmt.Errorf("a2a tool %q is registered by both %s and %s; "+
					"tool names must be unique across A2A bridges", td.Name, prev, label)
			}
			owner[td.Name] = label
		}
	}
	return nil
}

// bridgeExecutor returns the executor bridge tools run on: the host's
// (WithA2AToolExecutor) when set, otherwise the SDK's own.
func (c *A2ACapability) bridgeExecutor() tools.Executor {
	if c.toolExecutor != nil {
		return renameExecutor(c.toolExecutor, nsA2A)
	}
	exec := sdka2a.NewExecutor()
	// Each bridge has already fetched its agent's card; hand it over so the
	// executor calls the interface it declares without fetching it again.
	for _, bridge := range c.allBridges() {
		bridge.ShareCards(exec)
	}
	return exec
}

// renameExecutor registers a host executor under name, whatever name it
// reports, without hiding the entry points it implements: a host executor
// that may suspend calls keeps doing so, and every call reaches the host's
// call-taking entry point when it has one, so it sees the call ID and caller.
func renameExecutor(executor tools.Executor, name string) tools.Executor {
	named := namedExecutor{Executor: executor, name: name}
	switch executor.(type) {
	case tools.AsyncCallExecutor, tools.AsyncToolExecutor:
		return namedAsyncExecutor{namedExecutor: named}
	default:
		return named
	}
}

// namedExecutor registers a host executor under the "a2a" name the registry
// routes Mode "a2a" tools to, whatever name the host's executor reports.
type namedExecutor struct {
	tools.Executor
	name string
}

// Name returns the registry name the executor is registered under.
func (e namedExecutor) Name() string { return e.name }

// ExecuteCall runs call on the host executor's most specific synchronous
// entry point: ExecuteCall, then ExecuteMultimodal, then Execute — the same
// order the registry uses for an executor registered directly.
func (e namedExecutor) ExecuteCall(
	ctx context.Context, descriptor *tools.ToolDescriptor, call tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	if callExec, ok := e.Executor.(tools.CallExecutor); ok {
		return callExec.ExecuteCall(ctx, descriptor, call)
	}
	if mmExec, ok := e.Executor.(tools.MultimodalExecutor); ok {
		return mmExec.ExecuteMultimodal(ctx, descriptor, call.Args)
	}
	result, err := e.Execute(ctx, descriptor, call.Args)
	return result, nil, err
}

// namedAsyncExecutor is namedExecutor for a host executor that may suspend
// calls (an AsyncCallExecutor or AsyncToolExecutor).
type namedAsyncExecutor struct {
	namedExecutor
}

// ExecuteCallAsync runs call on the host executor's ExecuteCallAsync, or on
// its ExecuteAsync when it implements only that.
func (e namedAsyncExecutor) ExecuteCallAsync(
	ctx context.Context, descriptor *tools.ToolDescriptor, call tools.ToolCall,
) (*tools.ToolExecutionResult, error) {
	if callExec, ok := e.Executor.(tools.AsyncCallExecutor); ok {
		return callExec.ExecuteCallAsync(ctx, descriptor, call)
	}
	return e.Executor.(tools.AsyncToolExecutor).ExecuteAsync(ctx, descriptor, call.Args)
}

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

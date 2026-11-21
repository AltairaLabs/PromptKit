package sdk

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// ErrToolNameConflict is returned by Send and Stream for a [WithClientTools]
// tool whose name is already taken: by a tool the pack declares, one the host
// registered (OnToolExecutor, WithToolRegistry, MCP), or a capability tool
// (skill__, workflow__, a2a__, memory__ and the other system namespaces). A
// client tool never replaces one of those, so a client cannot shadow a server
// tool by naming its own after it. The error text names the tool.
var ErrToolNameConflict = errors.New("tool name is already registered")

// Limits on the client tools one turn offers (WithClientTools).
const (
	// maxClientTools is the most tools one turn offers.
	maxClientTools = 128
	// maxClientToolBytes caps one tool's description and input schema
	// together, in bytes.
	maxClientToolBytes = 64 << 10
)

// toolModeClientName is the mode of a tool the caller executes.
const toolModeClientName = "client"

// turnTools are the client tools the current turn offers the model
// (WithClientTools), each registered on the conversation's own registry with
// RegisterLocal, so no other conversation over a shared host registry sees it.
type turnTools struct {
	mu sync.RWMutex
	// byName holds each tool as registered.
	byName map[string]*tools.ToolDescriptor
}

// WithClientTools offers the model tools the application executes, for the
// turn this Send or Stream starts, beside the tools the pack's prompt
// declares. Pass the tools an AG-UI client lists in RunAgentInput.tools
// (agui.ToolsFromAGUI converts them).
//
// Each tool is a client tool: a call to it suspends the turn until the
// application answers with [Conversation.SendToolResult],
// [Conversation.RejectClientTool] or [Conversation.FailClientTool] and the
// turn resumes ([Conversation.Resume], [Conversation.ResumeStream]); a
// handler the application bound for the name (OnClientTool) answers it
// instead. The tools stay offered through every round of the turn and every
// Resume or Continue of it, and the next Send or Stream replaces them with its
// own (none without this option). The model is offered each tool's name,
// description and input schema as given, a call's arguments are checked
// against that schema, and the tools are recorded in tools_offered like any
// other.
//
// The tools belong to the conversation alone: they are registered on its own
// tool registry, never on a host registry passed with WithToolRegistry, so
// two conversations can offer the same name with different schemas.
//
// A tool whose name the conversation already has (a pack tool, a host or MCP
// tool) or that is in a system namespace (skill__, workflow__, a2a__,
// memory__, mcp__, ...) fails the Send with [ErrToolNameConflict] before the
// turn runs. So does a tool with no name or one listed twice, more than 128
// tools, or a tool whose description and schema exceed 64 KiB. A tool a host
// registers under an offered name later wins: from then on the model is not
// offered the client's tool, and a call by that name is the host's.
//
// The tools are offered as given, so offer only what the application
// executes and accepts the model calling: the model sees each description,
// which, like any prompt text the client controls, can try to steer it.
func WithClientTools(descs ...*tools.ToolDescriptor) SendOption {
	return func(c *sendConfig) error {
		c.clientTools = append(c.clientTools, descs...)
		return nil
	}
}

// checkClientTools validates a turn's WithClientTools set and returns each
// tool copied as a client tool, by name.
func (c *Conversation) checkClientTools(descs []*tools.ToolDescriptor) (map[string]*tools.ToolDescriptor, error) {
	if len(descs) > maxClientTools {
		return nil, fmt.Errorf("client tools: %d tools offered, at most %d", len(descs), maxClientTools)
	}
	offered := make(map[string]*tools.ToolDescriptor, len(descs))
	for _, d := range descs {
		if d == nil || d.Name == "" {
			return nil, errors.New("client tools: a tool has no name")
		}
		if _, dup := offered[d.Name]; dup {
			return nil, fmt.Errorf("client tools: tool %q is listed twice", d.Name)
		}
		if size := len(d.Description) + len(d.InputSchema); size > maxClientToolBytes {
			return nil, fmt.Errorf("client tools: tool %q is %d bytes, at most %d", d.Name, size, maxClientToolBytes)
		}
		if tools.IsSystemTool(d.Name) {
			return nil, fmt.Errorf("client tools: %q is in a system namespace: %w", d.Name, ErrToolNameConflict)
		}
		if existing := c.toolRegistry.Get(d.Name); existing != nil && existing != c.turnTools.get(d.Name) {
			return nil, fmt.Errorf("client tools: %q: %w", d.Name, ErrToolNameConflict)
		}
		offered[d.Name] = &tools.ToolDescriptor{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: append([]byte(nil), d.InputSchema...),
			Mode:        toolModeClientName,
		}
	}
	return offered, nil
}

// setTurnTools makes offered the turn's client tools, replacing the previous
// turn's, on the conversation's own registry.
func (c *Conversation) setTurnTools(offered map[string]*tools.ToolDescriptor) {
	c.turnTools.mu.Lock()
	defer c.turnTools.mu.Unlock()
	for name := range c.turnTools.byName {
		c.toolRegistry.UnregisterLocal(name)
	}
	for _, d := range offered {
		_ = c.toolRegistry.RegisterLocal(d) // named: cannot fail
	}
	c.turnTools.byName = offered
}

// get returns the registered descriptor of a turn tool, or nil.
func (t *turnTools) get(name string) *tools.ToolDescriptor {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.byName[name]
}

// offeredNames returns the turn tools the model is offered, sorted: those the
// registry still resolves to the turn's own descriptor, so a tool a host
// registered under the same name since is not granted by the offer.
func (c *Conversation) offeredNames() []string {
	c.turnTools.mu.RLock()
	defer c.turnTools.mu.RUnlock()
	var out []string
	for name, d := range c.turnTools.byName {
		if c.toolRegistry.Get(name) == d {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// copyTurnTools gives a fork the turn tools of its parent, registered on the
// fork's registry.
func (c *Conversation) copyTurnTools(fork *Conversation) {
	c.turnTools.mu.RLock()
	defer c.turnTools.mu.RUnlock()
	if len(c.turnTools.byName) == 0 {
		return
	}
	copied := make(map[string]*tools.ToolDescriptor, len(c.turnTools.byName))
	for name, d := range c.turnTools.byName {
		cp := *d
		copied[name] = &cp
	}
	fork.setTurnTools(copied)
}

// toolGrants returns the tools the conversation offers beyond the prompt's
// on this tools build: the active skills' grants and the turn's client tools
// (intpipeline.Config.ToolGrants).
func (c *Conversation) toolGrants() []string {
	skillGrants := c.skillToolGrants()
	offered := c.offeredNames()
	if len(offered) == 0 {
		return skillGrants
	}
	return append(append(make([]string, 0, len(skillGrants)+len(offered)), skillGrants...), offered...)
}

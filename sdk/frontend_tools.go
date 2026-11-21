package sdk

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// ErrToolNameConflict is returned by [Conversation.OfferClientTools] for a
// tool whose name is already taken: by a tool the pack declares, one the
// host registered (OnToolExecutor, WithToolRegistry, MCP), or a capability
// tool (skill__, workflow__, a2a__, memory__ and the other system namespaces).
// A tool offered at runtime never replaces one of those, so a client cannot
// shadow a server tool by naming its own after it. The error text names the
// tool.
var ErrToolNameConflict = errors.New("tool name is already registered")

// toolModeClientName is the mode of a tool the caller executes.
const toolModeClientName = "client"

// frontendDescriptors records the descriptors OfferClientTools registered, by
// identity. A conversation registers its descriptors on its tool registry,
// which a host's shared registry (WithToolRegistry) holds for every
// conversation over it, so this is how a later offer tells a tool offered at
// runtime, which it may offer again, from one the pack or host registered,
// which it must not replace.
var frontendDescriptors sync.Map // *tools.ToolDescriptor -> struct{}

// frontendTools is the set of tools a conversation offers the model at
// runtime (OfferClientTools), beyond the prompt's.
type frontendTools struct {
	mu sync.RWMutex
	// byName holds each offered tool as this conversation offers it.
	byName map[string]*tools.ToolDescriptor
}

// names returns the offered tools' names, sorted.
func (f *frontendTools) names() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if len(f.byName) == 0 {
		return nil
	}
	out := make([]string, 0, len(f.byName))
	for name := range f.byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// describe returns the conversation's own descriptor for an offered tool, or
// nil when name is not offered.
func (f *frontendTools) describe(name string) *tools.ToolDescriptor {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.byName[name]
}

// copyTo gives a fork the tools offered on its parent, offered the same way:
// the fork's registry holds copies of the parent's descriptors, which are
// recorded as offered at runtime so the fork can offer them again.
func (f *frontendTools) copyTo(dst *frontendTools, forkRegistry *tools.Registry) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if len(f.byName) == 0 {
		return
	}
	dst.byName = make(map[string]*tools.ToolDescriptor, len(f.byName))
	for name, d := range f.byName {
		dst.byName[name] = d
		if reg := forkRegistry.Get(name); reg != nil {
			frontendDescriptors.Store(reg, struct{}{})
		}
	}
}

// OfferClientTools offers the model tools the application executes, for the
// conversation's turns from now on, in addition to the tools the pack's
// prompt declares. Each call replaces the previous call's set; a nil or empty
// set offers none. Pass the tools an AG-UI client lists in
// RunAgentInput.tools (agui.ToolsFromAGUI converts them).
//
// Each tool is a client tool: a call to it suspends the turn, as
// [Conversation.DeferClientTool] does, until the application answers with
// [Conversation.SendToolResult], [Conversation.RejectClientTool] or
// [Conversation.FailClientTool] and the turn resumes; a handler the
// application bound for the name (OnClientTool) answers it instead. A call
// made while the tool was offered can still be answered once it is not. The
// model is offered the tool's name, description and input schema as given,
// and its calls are recorded in tools_offered like any other tool's.
//
// Only a new name can be offered. A name the conversation's tool registry
// already holds for a tool not offered this way (a pack tool, a host tool, an
// MCP tool) or a name in a system namespace (skill__, workflow__, a2a__,
// memory__, mcp__, ...) fails with [ErrToolNameConflict], and nothing in this
// call is offered. To have a pack's own client tool executed by the
// application, bind it with DeferClientTool instead; the prompt's tool list
// still decides whether it is offered.
//
// The tools are offered as given, so offer only what the application
// executes and accepts the model calling: the model sees each description,
// which, like any prompt text the client controls, can try to steer it.
//
// The descriptors are registered on the conversation's tool registry, which
// a host registry passed with WithToolRegistry shares between the
// conversations over it. Each conversation offers the model only its own
// set, each tool described as this conversation offers it, and two
// conversations can offer the same name. A tool a later call leaves out stays
// registered, but the model is not offered it, and a call to it is refused.
func (c *Conversation) OfferClientTools(descs []*tools.ToolDescriptor) error {
	offered, err := c.checkClientTools(descs)
	if err != nil {
		return err
	}
	f := &c.frontend
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, d := range offered {
		if _, registered := frontendDescriptors.Load(c.toolRegistry.Get(name)); !registered {
			reg := cloneFrontendDescriptor(d)
			if regErr := c.toolRegistry.Register(reg); regErr != nil {
				return fmt.Errorf("offer client tool %q: %w", name, regErr)
			}
			frontendDescriptors.Store(reg, struct{}{})
		}
	}
	f.byName = offered
	return nil
}

// checkClientTools validates an OfferClientTools set and returns it by name,
// each tool copied as a client tool.
func (c *Conversation) checkClientTools(descs []*tools.ToolDescriptor) (map[string]*tools.ToolDescriptor, error) {
	offered := make(map[string]*tools.ToolDescriptor, len(descs))
	for _, d := range descs {
		if d == nil || d.Name == "" {
			return nil, errors.New("offer client tools: a tool has no name")
		}
		if _, dup := offered[d.Name]; dup {
			return nil, fmt.Errorf("offer client tools: tool %q is listed twice", d.Name)
		}
		if tools.IsSystemTool(d.Name) {
			return nil, fmt.Errorf("offer client tools: %q is in a system namespace: %w", d.Name, ErrToolNameConflict)
		}
		if existing := c.toolRegistry.Get(d.Name); existing != nil {
			if _, ours := frontendDescriptors.Load(existing); !ours {
				return nil, fmt.Errorf("offer client tools: %q: %w", d.Name, ErrToolNameConflict)
			}
		}
		offered[d.Name] = cloneFrontendDescriptor(d)
	}
	return offered, nil
}

// cloneFrontendDescriptor is d as a client tool: its name, description and
// input schema, executed by the caller.
func cloneFrontendDescriptor(d *tools.ToolDescriptor) *tools.ToolDescriptor {
	return &tools.ToolDescriptor{
		Name:        d.Name,
		Description: d.Description,
		InputSchema: append([]byte(nil), d.InputSchema...),
		Mode:        toolModeClientName,
	}
}

// toolGrants returns the tools the conversation offers beyond the prompt's
// on this tools build: the active skills' grants and the client tools offered
// at runtime (intpipeline.Config.ToolGrants).
func (c *Conversation) toolGrants() []string {
	skillGrants := c.skillToolGrants()
	offered := c.frontend.names()
	if len(offered) == 0 {
		return skillGrants
	}
	return append(append(make([]string, 0, len(skillGrants)+len(offered)), skillGrants...), offered...)
}

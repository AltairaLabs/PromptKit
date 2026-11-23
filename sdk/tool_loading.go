package sdk

import (
	"fmt"
	"slices"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
)

// ToolLoadingMode is how a conversation's tools array follows a change to
// what the model may call.
type ToolLoadingMode string

const (
	// ToolLoadingRebuild sends each provider call the tools the model may
	// call on it, so the tools array changes whenever they do. It is the
	// default.
	ToolLoadingRebuild ToolLoadingMode = "rebuild"
	// ToolLoadingStable sends every provider call of the conversation the
	// same tools array, and narrows what the model may call on each call
	// without changing it.
	ToolLoadingStable ToolLoadingMode = "stable"
)

// ToolLoading configures how the conversation builds the tools array it
// sends the provider (WithToolLoading).
type ToolLoading struct {
	// Mode is the mode; empty is ToolLoadingRebuild.
	Mode ToolLoadingMode
}

// WithToolLoading sets how the conversation builds the tools array it sends
// the provider. The tools array comes first in the prompt a provider caches,
// so a change to it costs the cached prefix (tool.offer.rebuilt reports each
// one).
//
// ToolLoadingRebuild, the default, sends each call the tools the model may
// call on it: a ToolSelector's choice, a skill grant, a tool excluded after
// repeated rejection, a workflow state's tools, and none once
// max_tool_calls_per_turn is spent, each rebuild the array.
//
// ToolLoadingStable sends every call the same array: the prompt's tools,
// every pack tool a skill can grant, the tools of every workflow state's
// prompt, and the capability tools. What the model may call on a call is
// what rebuild mode would have sent; a call to any other tool gets the
// result "Tool <name> was not run: it is not available this turn" and does
// not run. OpenAI is also told the narrower set through tool_choice
// allowed_tools, so the model does not try; other providers see every tool
// and rely on the refusal. Once max_tool_calls_per_turn is spent, the rest
// of the turn keeps the array under tool choice none. The skill__activate
// index lists every skill, whatever a workflow state's skills filter or a
// skills selector chose; activating a skill the state does not admit is
// refused, as it always is. The array still changes when an MCP server's tool
// list or a client's frontend tools change, and on a provider switch.
//
// The same setting is RuntimeConfig's spec.tool_loading.mode. An unknown
// mode is an error.
//
//	sdk.WithToolLoading(sdk.ToolLoading{Mode: sdk.ToolLoadingStable})
func WithToolLoading(loading ToolLoading) Option {
	return func(c *config) error {
		switch loading.Mode {
		case "", ToolLoadingRebuild, ToolLoadingStable:
			c.toolLoading = loading
			return nil
		}
		return fmt.Errorf("tool loading mode %q: want %q or %q", loading.Mode, ToolLoadingRebuild, ToolLoadingStable)
	}
}

// stableTools reports whether the conversation runs in tool loading mode
// stable.
func (c *Conversation) stableTools() bool {
	return c.config != nil && c.config.toolLoading.Mode == ToolLoadingStable
}

// stageToolLoading is the provider stage's ToolLoading for the conversation.
func (c *Conversation) stageToolLoading() stage.ToolLoading {
	if !c.stableTools() {
		return stage.ToolLoading{Mode: stage.ToolLoadingRebuild}
	}
	return stage.ToolLoading{Mode: stage.ToolLoadingStable, Catalog: c.toolCatalog}
}

// toolCatalog is what stable mode sends from the first call beyond the
// prompt's tools: every pack tool a skill can grant, and the tools of every
// workflow state's prompt, sorted. Read on each tools build.
func (c *Conversation) toolCatalog() []string {
	var names []string
	if sc := c.skillsCapability(); sc != nil && sc.executor != nil {
		names = append(names, sc.executor.GrantableTools()...)
	}
	if c.pack != nil && c.pack.Workflow != nil {
		for _, state := range c.pack.Workflow.States {
			if state == nil {
				continue
			}
			if p := c.pack.Prompts[state.PromptTask]; p != nil {
				names = append(names, p.Tools...)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

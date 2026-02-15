package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/prompt/agentcard"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// AgentToolResolver resolves agent member references in tool lists
// to A2A-compatible tool descriptors.
type AgentToolResolver struct {
	cards map[string]*a2a.AgentCard
	pack  *prompt.Pack
}

// NewAgentToolResolver creates a resolver from a compiled pack.
// Returns nil if the pack has no agents section.
func NewAgentToolResolver(pack *prompt.Pack) *AgentToolResolver {
	cards := agentcard.GenerateAgentCards(pack)
	if cards == nil {
		return nil
	}
	return &AgentToolResolver{cards: cards, pack: pack}
}

// IsAgentTool checks if a tool name refers to an agent member.
func (r *AgentToolResolver) IsAgentTool(toolName string) bool {
	if r == nil {
		return false
	}
	_, ok := r.cards[toolName]
	return ok
}

// ResolveAgentTools returns tool descriptors for all agent members
// that appear in the given tool names list.
func (r *AgentToolResolver) ResolveAgentTools(toolNames []string) []*tools.ToolDescriptor {
	if r == nil {
		return nil
	}
	var descriptors []*tools.ToolDescriptor
	for _, name := range toolNames {
		card, ok := r.cards[name]
		if !ok {
			continue
		}
		for i := range card.Skills {
			desc := &tools.ToolDescriptor{
				Name:        name,
				Description: card.Skills[i].Description,
				Mode:        "a2a",
				A2AConfig: &tools.A2AConfig{
					SkillID: card.Skills[i].ID,
				},
			}
			descriptors = append(descriptors, desc)
		}
	}
	return descriptors
}

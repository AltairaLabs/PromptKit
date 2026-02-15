// Package agentcard generates A2A Agent Cards from a compiled Pack's agents section.
package agentcard

import (
	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// GenerateAgentCards generates A2A AgentCards from a compiled pack's agents section.
// Returns a map of agent name -> AgentCard.
// Returns nil if the pack has no agents config.
func GenerateAgentCards(pack *prompt.Pack) map[string]*a2a.AgentCard {
	if pack == nil || pack.Agents == nil {
		return nil
	}

	cards := make(map[string]*a2a.AgentCard)

	for name, agentDef := range pack.Agents.Members {
		card := generateCard(pack, name, agentDef)
		cards[name] = card
	}

	return cards
}

func generateCard(pack *prompt.Pack, name string, agentDef *prompt.AgentDef) *a2a.AgentCard {
	p := pack.Prompts[name]

	card := &a2a.AgentCard{
		Name:               cardName(p, name),
		Version:            pack.Version,
		Skills:             []a2a.AgentSkill{generateSkill(name, p, agentDef)},
		DefaultInputModes:  defaultModes(agentDef.InputModes),
		DefaultOutputModes: defaultModes(agentDef.OutputModes),
	}

	if agentDef.Description != "" {
		card.Description = agentDef.Description
	} else if p != nil {
		card.Description = p.Description
	}

	return card
}

func generateSkill(name string, p *prompt.PackPrompt, agentDef *prompt.AgentDef) a2a.AgentSkill {
	skill := a2a.AgentSkill{
		ID:          name,
		InputModes:  defaultModes(agentDef.InputModes),
		OutputModes: defaultModes(agentDef.OutputModes),
		Tags:        agentDef.Tags,
	}

	if p != nil {
		skill.Name = p.Name
		if agentDef.Description != "" {
			skill.Description = agentDef.Description
		} else {
			skill.Description = p.Description
		}
	} else {
		skill.Name = name
		skill.Description = agentDef.Description
	}

	return skill
}

func cardName(p *prompt.PackPrompt, fallback string) string {
	if p != nil && p.Name != "" {
		return p.Name
	}
	return fallback
}

var defaultTextMode = []string{"text/plain"}

func defaultModes(modes []string) []string {
	if len(modes) > 0 {
		return modes
	}
	return defaultTextMode
}

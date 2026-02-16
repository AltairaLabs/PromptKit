package adaptersdk

import (
	"fmt"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/deploy"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/prompt/agentcard"
)

// AgentInfo provides a simplified view of an agent member for deploy adapters.
type AgentInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsEntry     bool     `json:"is_entry"`
	Tags        []string `json:"tags,omitempty"`
	InputModes  []string `json:"input_modes"`
	OutputModes []string `json:"output_modes"`
}

// IsMultiAgent returns true if the pack has an agents section with members.
func IsMultiAgent(pack *prompt.Pack) bool {
	return pack.Agents != nil && len(pack.Agents.Members) > 0
}

// ExtractAgents returns agent info for all members in the pack's agents section.
// Returns nil if the pack is not multi-agent.
// Defaults: text/plain for missing input/output modes, description falls back
// to the corresponding prompt description.
func ExtractAgents(pack *prompt.Pack) []AgentInfo {
	if !IsMultiAgent(pack) {
		return nil
	}

	agents := make([]AgentInfo, 0, len(pack.Agents.Members))

	for name, def := range pack.Agents.Members {
		info := AgentInfo{
			Name:    name,
			IsEntry: name == pack.Agents.Entry,
			Tags:    def.Tags,
		}

		// Description: prefer agent def, fall back to prompt description.
		info.Description = def.Description
		if info.Description == "" {
			if p, ok := pack.Prompts[name]; ok && p != nil {
				info.Description = p.Description
			}
		}

		// Default modes to text/plain when not specified.
		info.InputModes = defaultModes(def.InputModes)
		info.OutputModes = defaultModes(def.OutputModes)

		agents = append(agents, info)
	}

	// Sort by name for deterministic output.
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Name < agents[j].Name
	})

	return agents
}

// GenerateAgentCards re-exports agentcard.GenerateAgentCards for adapter convenience.
func GenerateAgentCards(pack *prompt.Pack) map[string]*a2a.AgentCard {
	return agentcard.GenerateAgentCards(pack)
}

// GenerateAgentResourcePlan creates resource changes for deploying a multi-agent pack.
// For each member it emits an agent_runtime and a2a_endpoint resource.
// For the entry agent it also emits a gateway resource.
// Returns nil if the pack is not multi-agent.
func GenerateAgentResourcePlan(pack *prompt.Pack) []deploy.ResourceChange {
	if !IsMultiAgent(pack) {
		return nil
	}

	// Collect sorted member names for deterministic output.
	names := make([]string, 0, len(pack.Agents.Members))
	for name := range pack.Agents.Members {
		names = append(names, name)
	}
	sort.Strings(names)

	var changes []deploy.ResourceChange

	for _, name := range names {
		changes = append(changes,
			deploy.ResourceChange{
				Type:   "agent_runtime",
				Name:   name,
				Action: deploy.ActionCreate,
				Detail: fmt.Sprintf("Create agent runtime for %s", name),
			},
			deploy.ResourceChange{
				Type:   "a2a_endpoint",
				Name:   name + "_endpoint",
				Action: deploy.ActionCreate,
				Detail: fmt.Sprintf("Create A2A endpoint for %s", name),
			},
		)
	}

	// Gateway for the entry agent.
	changes = append(changes, deploy.ResourceChange{
		Type:   "gateway",
		Name:   pack.Agents.Entry + "_gateway",
		Action: deploy.ActionCreate,
		Detail: fmt.Sprintf("Create gateway for entry agent %s", pack.Agents.Entry),
	})

	return changes
}

// defaultModes returns the given modes or a default of ["text/plain"] when empty.
func defaultModes(modes []string) []string {
	if len(modes) > 0 {
		return modes
	}
	return []string{"text/plain"}
}

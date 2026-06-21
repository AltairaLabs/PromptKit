package engine

import (
	"fmt"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// AgentInfo describes one selectable agent in an Arena config. An "agent" is a
// prompt config, keyed by its task_type, carrying the system prompt and tools.
type AgentInfo struct {
	TaskType    string
	Description string
}

// Agents lists the prompt configs (agents) declared in the loaded config,
// sorted by task_type for stable ordering in pickers.
func (e *Engine) Agents() []AgentInfo {
	taskTypes := make([]string, 0, len(e.config.LoadedPromptConfigs))
	for _, pd := range e.config.LoadedPromptConfigs {
		if pd.TaskType != "" {
			taskTypes = append(taskTypes, pd.TaskType)
		}
	}
	sort.Strings(taskTypes)
	out := make([]AgentInfo, len(taskTypes))
	for i, tt := range taskTypes {
		out[i] = AgentInfo{TaskType: tt}
	}
	return out
}

// ProviderIDs lists the inference provider IDs declared in the loaded config,
// sorted for stable ordering.
func (e *Engine) ProviderIDs() []string {
	out := make([]string, 0, len(e.config.LoadedProviders))
	for id := range e.config.LoadedProviders {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// MissingRequiredVars returns the names of template variables the prompt for
// taskType declares as required but that are absent (or blank) in provided.
// The console uses this to prompt the user before chatting.
func (e *Engine) MissingRequiredVars(taskType string, provided map[string]string) ([]string, error) {
	pc, err := e.promptConfigForTaskType(taskType)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, v := range pc.Spec.Variables {
		if !v.Required {
			continue
		}
		if val, ok := provided[v.Name]; !ok || val == "" {
			missing = append(missing, v.Name)
		}
	}
	return missing, nil
}

// promptConfigForTaskType finds the loaded prompt config whose task_type matches
// taskType. Returns an error if no match is found.
func (e *Engine) promptConfigForTaskType(taskType string) (*prompt.Config, error) {
	for _, pd := range e.config.LoadedPromptConfigs {
		if pd.TaskType != taskType {
			continue
		}
		if pd.Config == nil {
			return nil, fmt.Errorf("prompt config for task type %q has nil config", taskType)
		}
		cfg, ok := pd.Config.(*prompt.Config)
		if !ok {
			return nil, fmt.Errorf("prompt config for task type %q has invalid type", taskType)
		}
		return cfg, nil
	}
	return nil, fmt.Errorf("no prompt config found for task type %q", taskType)
}

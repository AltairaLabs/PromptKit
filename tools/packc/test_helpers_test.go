package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// validateLoadedMedia prints media validation warnings for loaded prompt configs.
// Kept as test helper after migration to compiler package.
func validateLoadedMedia(cfg *config.Config, configDir string) {
	for _, promptData := range cfg.LoadedPromptConfigs {
		promptConfig, ok := promptData.Config.(*prompt.Config)
		if !ok {
			continue
		}

		warnings := validateMediaReferences(promptConfig, configDir)
		if len(warnings) > 0 {
			fmt.Printf("media validation warnings for %s:\n", promptConfig.Spec.TaskType)
			for _, w := range warnings {
				fmt.Printf(warningFormat, w)
			}
		}
	}
}

// parsePackEvalsFromConfig returns pack-level eval definitions from arena config.
// Kept as test helper after migration to compiler package.
func parsePackEvalsFromConfig(cfg *config.Config) []evals.EvalDef {
	return cfg.PackEvals
}

// parseWorkflowFromConfig parses workflow config from arena config.
// Kept as test helper after migration to compiler package.
func parseWorkflowFromConfig(cfg *config.Config) *prompt.WorkflowConfig {
	if cfg.Workflow == nil {
		return nil
	}
	data, err := json.Marshal(cfg.Workflow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to marshal workflow config: %v\n", err)
		return nil
	}
	var wf prompt.WorkflowConfig
	if err := json.Unmarshal(data, &wf); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse workflow config: %v\n", err)
		return nil
	}
	return &wf
}

// parseAgentsFromConfig parses agents config from arena config.
// Kept as test helper after migration to compiler package.
func parseAgentsFromConfig(cfg *config.Config) *prompt.AgentsConfig {
	if cfg.Agents == nil {
		return nil
	}
	data, err := json.Marshal(cfg.Agents)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to marshal agents config: %v\n", err)
		return nil
	}
	var ag prompt.AgentsConfig
	if err := json.Unmarshal(data, &ag); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse agents config: %v\n", err)
		return nil
	}
	return &ag
}

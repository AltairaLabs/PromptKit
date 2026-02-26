package main

import "github.com/AltairaLabs/PromptKit/tools/arena/templates"

// Test helper: resolveIndexPath mimics old behavior for backward compatibility in tests
func resolveIndexPath() (path string, repoName string, err error) {
	cfg, err := templates.LoadRepoConfig(repoConfigPath)
	if err != nil {
		return "", "", err
	}

	// Check if templateIndex is a known repo name
	if cfg != nil && cfg.Repos != nil {
		if repo, ok := cfg.Repos[templateIndex]; ok {
			return repo.URL, templateIndex, nil
		}
	}

	// Not a known repo name, treat as direct path/URL
	return templateIndex, "", nil
}

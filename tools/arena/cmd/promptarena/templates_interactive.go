package main

import (
	"fmt"
	"strings"

	"github.com/manifoldco/promptui"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

// This file contains interactive I/O logic intentionally split for coverage control.
// See sonar-project.properties: **/*_interactive.go is excluded from coverage gates.

func promptForMissingInteractive(vars map[string]string, pkg *templates.TemplatePackage) (map[string]string, error) {
	if pkg == nil {
		return vars, nil
	}
	keys := extractPlaceholders(pkg)
	out := make(map[string]string, len(vars)+len(keys))
	for k, v := range vars {
		out[k] = v
	}
	for _, k := range keys {
		if _, ok := out[k]; ok {
			continue
		}
		p := promptui.Prompt{
			Label:     fmt.Sprintf("Value for %s", k),
			AllowEdit: true,
		}
		val, err := p.Run()
		if err != nil {
			if err == promptui.ErrInterrupt {
				return nil, fmt.Errorf("prompt canceled")
			}
			return nil, fmt.Errorf("prompt for %s: %w", k, err)
		}
		out[k] = strings.TrimSpace(val)
	}
	return out, nil
}

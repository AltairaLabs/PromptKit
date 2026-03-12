package evals

import "github.com/AltairaLabs/PromptKit/runtime/logger"

// ResolveEvals merges pack-level and prompt-level eval definitions.
// Prompt-level evals override pack-level evals when they share the same ID.
// The returned slice preserves pack ordering first, followed by any
// prompt-only evals (those with no pack counterpart) in their original order.
func ResolveEvals(packEvals, promptEvals []EvalDef) []EvalDef {
	if len(packEvals) == 0 && len(promptEvals) == 0 {
		return nil
	}

	// Index prompt evals by ID for O(1) lookup.
	promptByID := make(map[string]EvalDef, len(promptEvals))
	for i := range promptEvals {
		promptByID[promptEvals[i].ID] = promptEvals[i]
	}

	// Track which prompt eval IDs were consumed as overrides.
	seen := make(map[string]bool, len(promptEvals))

	// Start with pack evals, applying prompt overrides where they exist.
	merged := make([]EvalDef, 0, len(packEvals)+len(promptEvals))
	for i := range packEvals {
		if override, ok := promptByID[packEvals[i].ID]; ok {
			merged = append(merged, override)
			seen[packEvals[i].ID] = true
		} else {
			merged = append(merged, packEvals[i])
		}
	}

	// Append prompt-only evals not already seen, preserving their order.
	for i := range promptEvals {
		if !seen[promptEvals[i].ID] {
			merged = append(merged, promptEvals[i])
		}
	}

	overrides := len(seen)
	logger.Debug("evals: resolved eval definitions",
		"pack_count", len(packEvals),
		"prompt_count", len(promptEvals),
		"merged_count", len(merged),
		"overrides", overrides,
	)

	return merged
}

// FilterByGroups returns only the defs that belong to at least one of the
// requested groups. If groups is nil or empty, all defs are returned unchanged.
// Each def's effective groups are determined by GetGroups() (defaults to ["default"]).
func FilterByGroups(defs []EvalDef, groups []string) []EvalDef {
	if len(groups) == 0 {
		return defs
	}

	allowed := make(map[string]bool, len(groups))
	for _, g := range groups {
		allowed[g] = true
	}

	filtered := make([]EvalDef, 0, len(defs))
	for i := range defs {
		for _, g := range defs[i].GetGroups() {
			if allowed[g] {
				filtered = append(filtered, defs[i])
				break
			}
		}
	}

	logger.Debug("evals: filtered by groups",
		"requested_groups", groups,
		"before", len(defs),
		"after", len(filtered),
	)

	return filtered
}

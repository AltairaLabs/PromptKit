package evals

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
	for _, e := range promptEvals {
		promptByID[e.ID] = e
	}

	// Track which prompt eval IDs were consumed as overrides.
	seen := make(map[string]bool, len(promptEvals))

	// Start with pack evals, applying prompt overrides where they exist.
	merged := make([]EvalDef, 0, len(packEvals)+len(promptEvals))
	for _, pe := range packEvals {
		if override, ok := promptByID[pe.ID]; ok {
			merged = append(merged, override)
			seen[pe.ID] = true
		} else {
			merged = append(merged, pe)
		}
	}

	// Append prompt-only evals not already seen, preserving their order.
	for _, e := range promptEvals {
		if !seen[e.ID] {
			merged = append(merged, e)
		}
	}

	return merged
}

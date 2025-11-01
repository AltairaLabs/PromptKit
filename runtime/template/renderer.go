// Package template provides template rendering and variable substitution.
//
// This package implements a flexible template system that can be used by
// both prompts and personas. It supports:
//   - Variable substitution with {{variable}} syntax
//   - Recursive template resolution (variables can contain other variables)
//   - Validation of required variables
//   - Detection of unresolved placeholders
//
// Future versions may support more advanced templating engines like Go templates
// (similar to Helm charts) for conditional logic, loops, and functions.
package template

import (
	"fmt"
	"strings"
)

// Renderer handles variable substitution in templates
type Renderer struct {
	// Future: Add configuration options like template engine type, custom functions, etc.
}

// NewRenderer creates a new template renderer
func NewRenderer() *Renderer {
	return &Renderer{}
}

// Render applies variable substitution to the template with recursive resolution.
//
// The renderer performs multiple passes (up to maxPasses) to handle nested
// variable substitution. For example, if var1="{{var2}}" and var2="value",
// the final result will correctly resolve to "value".
//
// Returns an error if any placeholders remain unresolved after all passes.
func (r *Renderer) Render(templateText string, vars map[string]string) (string, error) {
	// Use a simple string replacement approach with recursive substitution
	result := templateText

	// Do multiple passes to handle nested variable substitution
	maxPasses := 3
	for pass := 0; pass < maxPasses; pass++ {
		changed := false
		for key, value := range vars {
			placeholder := fmt.Sprintf("{{%s}}", key)
			if strings.Contains(result, placeholder) {
				result = strings.ReplaceAll(result, placeholder, value)
				changed = true
			}
		}
		// If no substitutions were made, we're done
		if !changed {
			break
		}
	}

	// Validate that all placeholders were resolved
	if strings.Contains(result, "{{") && strings.Contains(result, "}}") {
		// Find unresolved placeholders for better error reporting
		unresolved := r.findUnresolvedPlaceholders(result)
		if len(unresolved) > 0 {
			return "", fmt.Errorf("unresolved template placeholders: %v", unresolved)
		}
	}

	return result, nil
}

// ValidateRequiredVars checks that all required variables are provided and non-empty.
// Returns an error listing any missing variables.
func (r *Renderer) ValidateRequiredVars(requiredVars []string, vars map[string]string) error {
	var missing []string
	for _, required := range requiredVars {
		if value, exists := vars[required]; !exists || value == "" {
			missing = append(missing, required)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required variables: %v", missing)
	}

	return nil
}

// MergeVars merges multiple variable maps with later maps taking precedence.
// This is useful for combining default values, context variables, and overrides.
//
// Example:
//
//	defaults := map[string]string{"color": "blue", "size": "medium"}
//	overrides := map[string]string{"color": "red"}
//	result := MergeVars(defaults, overrides)
//	// result = {"color": "red", "size": "medium"}
func (r *Renderer) MergeVars(varMaps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, vars := range varMaps {
		for k, v := range vars {
			result[k] = v
		}
	}
	return result
}

// findUnresolvedPlaceholders extracts unresolved {{variable}} placeholders from text.
// This is used for error reporting when template rendering fails.
func (r *Renderer) findUnresolvedPlaceholders(text string) []string {
	var placeholders []string

	// Simple regex-like approach to find {{variable}} patterns
	for i := 0; i < len(text)-3; i++ {
		if text[i:i+2] == "{{" {
			// Find the closing }}
			for j := i + 2; j < len(text)-1; j++ {
				if text[j:j+2] == "}}" {
					placeholder := text[i : j+2]
					placeholders = append(placeholders, placeholder)
					i = j + 1 // Move past this placeholder
					break
				}
			}
		}
	}

	return placeholders
}

// GetUsedVars returns a list of variable names that had non-empty values.
// This is useful for debugging and logging which variables were actually used.
func GetUsedVars(vars map[string]string) []string {
	var used []string
	for key, val := range vars {
		if val != "" {
			used = append(used, key)
		}
	}
	return used
}

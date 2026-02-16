// Package pack provides internal pack loading functionality.
package pack

import (
	"fmt"

	"github.com/xeipuuv/gojsonschema"

	"github.com/AltairaLabs/PromptKit/runtime/prompt/schema"
)

// SchemaValidationError represents a schema validation error with details.
type SchemaValidationError struct {
	Errors []string
}

func (e *SchemaValidationError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("pack schema validation failed: %s", e.Errors[0])
	}
	return fmt.Sprintf("pack schema validation failed with %d errors", len(e.Errors))
}

// ValidateAgainstSchema validates pack JSON data against the PromptPack schema.
// It uses the $schema URL from the pack if present, otherwise uses the embedded schema.
// The PROMPTKIT_SCHEMA_SOURCE environment variable can override this behavior:
//   - "local": Always use embedded schema (default, for offline support)
//   - "remote": Always fetch from URL
//   - file path: Load schema from local file
//
// Returns nil if validation passes, or a SchemaValidationError with details.
func ValidateAgainstSchema(data []byte) error {
	// Extract $schema from the pack to support versioned schemas
	packSchemaURL := schema.ExtractSchemaURL(data)

	schemaLoader, err := schema.GetSchemaLoader(packSchemaURL)
	if err != nil {
		return fmt.Errorf("failed to load schema: %w", err)
	}

	documentLoader := gojsonschema.NewBytesLoader(data)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return fmt.Errorf("schema validation error: %w", err)
	}

	if !result.Valid() {
		errors := make([]string, 0, len(result.Errors()))
		for _, desc := range result.Errors() {
			errors = append(errors, fmt.Sprintf("%s: %s", desc.Field(), desc.Description()))
		}
		return &SchemaValidationError{Errors: errors}
	}

	return nil
}

// AgentsValidationError represents an agents section validation error with details.
type AgentsValidationError struct {
	Errors []string
}

func (e *AgentsValidationError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("agents validation failed: %s", e.Errors[0])
	}
	return fmt.Sprintf("agents validation failed with %d errors: %s",
		len(e.Errors), e.Errors[0])
}

// ValidateAgents validates the agents section of a pack.
// Returns nil if agents is nil (the section is optional) or if validation passes.
func (p *Pack) ValidateAgents() error {
	if p.Agents == nil {
		return nil
	}

	var errs []string

	errs = append(errs, validateAgentsStructure(p.Agents, p.Prompts)...)
	errs = append(errs, validateAgentsModes(p.Agents.Members)...)

	if len(errs) > 0 {
		return &AgentsValidationError{Errors: errs}
	}
	return nil
}

// validateAgentsStructure checks members, entry, and prompt references.
func validateAgentsStructure(agents *AgentsConfig, prompts map[string]*Prompt) []string {
	var errs []string

	if len(agents.Members) == 0 {
		errs = append(errs, "agents.members must be non-empty")
	}

	if agents.Entry == "" {
		errs = append(errs, "agents.entry is required")
	}

	if agents.Entry != "" && len(agents.Members) > 0 {
		if _, ok := agents.Members[agents.Entry]; !ok {
			errs = append(errs, fmt.Sprintf(
				"agents.entry %q does not reference a key in agents.members", agents.Entry))
		}
	}

	for key := range agents.Members {
		if _, ok := prompts[key]; !ok {
			errs = append(errs, fmt.Sprintf(
				"agents.members key %q does not reference a valid prompt", key))
		}
	}

	return errs
}

// validateAgentsModes checks input/output MIME type formats for all members.
func validateAgentsModes(members map[string]*AgentDef) []string {
	var errs []string
	for key, def := range members {
		for _, mode := range def.InputModes {
			if !isValidMIMEFormat(mode) {
				errs = append(errs, fmt.Sprintf(
					"agents.members[%q].input_modes: %q is not a valid MIME type", key, mode))
			}
		}
		for _, mode := range def.OutputModes {
			if !isValidMIMEFormat(mode) {
				errs = append(errs, fmt.Sprintf(
					"agents.members[%q].output_modes: %q is not a valid MIME type", key, mode))
			}
		}
	}
	return errs
}

// isValidMIMEFormat checks if a string looks like a valid MIME type (type/subtype).
// It performs a lightweight format check, not a full registry lookup.
func isValidMIMEFormat(s string) bool {
	if s == "" {
		return false
	}
	// Must contain exactly one slash separating non-empty type and subtype
	slashIdx := -1
	for i, c := range s {
		if c == '/' {
			if slashIdx >= 0 {
				return false // multiple slashes
			}
			slashIdx = i
		}
	}
	if slashIdx <= 0 || slashIdx >= len(s)-1 {
		return false
	}
	return true
}

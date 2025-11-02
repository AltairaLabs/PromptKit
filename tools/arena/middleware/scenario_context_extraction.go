package middleware

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// ScenarioContextExtractionMiddleware extracts context using scenario metadata and conversation history.
// This is designed for Arena use where rich scenario metadata is available.
//
// When scenario metadata is present, it uses:
// - Scenario metadata variables (domain, user role from scenario definition)
// - Scenario description and task type
// - Message analysis as fallback
//
// Extracted variables are merged into execCtx.Variables, allowing templates to use them.

type scenarioContextExtractionMiddleware struct {
	scenario *config.Scenario
}

func ScenarioContextExtractionMiddleware(scenario *config.Scenario) pipeline.Middleware {
	return &scenarioContextExtractionMiddleware{scenario: scenario}
}

func (m *scenarioContextExtractionMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Extract context using scenario metadata + message analysis
	extracted := extractFromScenario(m.scenario, execCtx.Messages)

	// Initialize Variables map if needed
	if execCtx.Variables == nil {
		execCtx.Variables = make(map[string]string)
	}

	// Merge extracted variables (don't overwrite existing ones)
	for k, v := range extracted {
		if _, exists := execCtx.Variables[k]; !exists {
			execCtx.Variables[k] = v
		}
	}

	// Continue to next middleware
	return next()
}

func (m *scenarioContextExtractionMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Scenario context extraction middleware doesn't process chunks
	return nil
}

// buildContextSlot creates the main context description from scenario and conversation
func buildContextSlot(scenario *config.Scenario, messages []types.Message) string {
	contextParts := []string{}

	if scenario.Description != "" {
		contextParts = append(contextParts, scenario.Description)
	}

	if len(messages) > 0 && messages[0].Role == "user" && messages[0].Content != "" {
		content := messages[0].Content
		if len(content) > 150 {
			content = content[:150] + "..."
		}
		contextParts = append(contextParts, fmt.Sprintf("User wants to: %s", content))
	}

	if len(contextParts) == 0 {
		return fmt.Sprintf("%s conversation", scenario.TaskType)
	}

	return strings.Join(contextParts, ". ")
}

// extractFromScenario extracts context using scenario metadata and messages
func extractFromScenario(scenario *config.Scenario, messages []types.Message) map[string]string {
	variables := make(map[string]string)

	// Use scenario metadata if available
	domain := ""
	userRole := ""

	if scenario.ContextMetadata != nil {
		if scenario.ContextMetadata.Domain != "" {
			domain = scenario.ContextMetadata.Domain
		}
		if scenario.ContextMetadata.UserRole != "" {
			userRole = scenario.ContextMetadata.UserRole
		}
	}

	// Fall back to extraction from scenario + messages
	if domain == "" {
		text := strings.ToLower(scenario.ID + " " + scenario.Description)
		for _, msg := range messages {
			text += " " + strings.ToLower(msg.Content)
		}
		domain = extractDomainFromText(text)
	}

	if userRole == "" {
		text := strings.ToLower(scenario.ID + " " + scenario.Description)
		for _, msg := range messages {
			text += " " + strings.ToLower(msg.Content)
		}
		userRole = extractRoleFromText(text)
	}

	contextSlot := buildContextSlot(scenario, messages)

	variables["domain"] = domain
	variables["user_context"] = userRole
	variables["user_role"] = userRole
	variables["context_slot"] = contextSlot

	return variables
}

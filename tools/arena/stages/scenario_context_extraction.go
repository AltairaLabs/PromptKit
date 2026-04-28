package stages

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	roleUser = "user"
	// maxContextTruncationLength is the maximum length of context before truncation
	maxContextTruncationLength = 150
)

// ScenarioContextExtractionStage extracts context using scenario metadata and conversation history.
// This is designed for Arena use where rich scenario metadata is available.
//
// When scenario metadata is present, it uses:
// - Scenario metadata variables (domain, user role from scenario definition)
// - Scenario description and task type
// - Message analysis as fallback
//
// Extracted variables are merged into TurnState.Variables, where TemplateStage
// reads them when rendering the system prompt.
type ScenarioContextExtractionStage struct {
	stage.BaseStage
	scenario  *config.Scenario
	turnState *stage.TurnState
}

// NewScenarioContextExtractionStageWithTurnState creates a scenario context
// extraction stage that writes extracted variables into the shared *TurnState.
func NewScenarioContextExtractionStageWithTurnState(
	scenario *config.Scenario, turnState *stage.TurnState,
) *ScenarioContextExtractionStage {
	return &ScenarioContextExtractionStage{
		BaseStage: stage.NewBaseStage("scenario_context_extraction", stage.StageTypeTransform),
		scenario:  scenario,
		turnState: turnState,
	}
}

// Process extracts scenario context and writes it to TurnState.Variables.
//
//nolint:lll // Channel signature cannot be shortened
func (s *ScenarioContextExtractionStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	// Collect elements and messages
	elements, messages := s.accumulateInput(input)

	// Extract context using scenario metadata + message analysis
	extracted := extractFromScenario(s.scenario, messages)

	// Merge into TurnState.Variables (don't overwrite existing keys).
	if s.turnState != nil {
		if s.turnState.Variables == nil {
			s.turnState.Variables = make(map[string]string, len(extracted))
		}
		for k, v := range extracted {
			if _, exists := s.turnState.Variables[k]; !exists {
				s.turnState.Variables[k] = v
			}
		}
	}

	// Forward elements unchanged.
	for i := range elements {
		select {
		case output <- elements[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// accumulateInput collects all input elements and their messages.
func (s *ScenarioContextExtractionStage) accumulateInput(
	input <-chan stage.StreamElement,
) ([]stage.StreamElement, []types.Message) {
	var elements []stage.StreamElement
	var messages []types.Message

	for elem := range input {
		elements = append(elements, elem)
		if elem.Message != nil {
			messages = append(messages, *elem.Message)
		}
	}

	return elements, messages
}

// buildContextSlot creates the main context description from scenario and conversation
func buildContextSlot(scenario *config.Scenario, messages []types.Message) string {
	contextParts := []string{}

	if scenario.Description != "" {
		contextParts = append(contextParts, scenario.Description)
	}

	if len(messages) > 0 && messages[0].Role == roleUser && messages[0].Content != "" {
		content := messages[0].Content
		if len(content) > maxContextTruncationLength {
			content = content[:maxContextTruncationLength] + "..."
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

	contextSlot := buildContextSlot(scenario, messages)

	variables["domain"] = domain
	variables["user_context"] = userRole
	variables["user_role"] = userRole
	variables["context_slot"] = contextSlot

	return variables
}

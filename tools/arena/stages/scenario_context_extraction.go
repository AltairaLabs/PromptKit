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
// Extracted variables are merged into element metadata, allowing templates to use them.
type ScenarioContextExtractionStage struct {
	stage.BaseStage
	scenario *config.Scenario
}

// NewScenarioContextExtractionStage creates a new scenario context extraction stage.
func NewScenarioContextExtractionStage(scenario *config.Scenario) *ScenarioContextExtractionStage {
	return &ScenarioContextExtractionStage{
		BaseStage: stage.NewBaseStage("scenario_context_extraction", stage.StageTypeTransform),
		scenario:  scenario,
	}
}

// Process extracts scenario context and adds it to elements.
//
//nolint:lll // Channel signature cannot be shortened
func (s *ScenarioContextExtractionStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	// Collect elements and messages
	elements, messages := s.accumulateInput(input)

	// Extract context using scenario metadata + message analysis
	extracted := extractFromScenario(s.scenario, messages)

	// Forward elements with enriched metadata
	return s.forwardEnrichedElements(ctx, elements, extracted, output)
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

// forwardEnrichedElements sends elements with added scenario context.
//
//nolint:lll // Channel signature cannot be shortened
func (s *ScenarioContextExtractionStage) forwardEnrichedElements(ctx context.Context, elements []stage.StreamElement, extracted map[string]string, output chan<- stage.StreamElement) error {
	for i := range elements {
		elem := &elements[i]
		s.enrichElement(elem, extracted)

		select {
		case output <- *elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// enrichElement adds extracted variables to an element's metadata.
func (s *ScenarioContextExtractionStage) enrichElement(elem *stage.StreamElement, extracted map[string]string) {
	if elem.Metadata == nil {
		elem.Metadata = make(map[string]interface{})
	}

	// Add extracted variables to metadata (don't overwrite existing)
	for k, v := range extracted {
		if _, exists := elem.Metadata[k]; !exists {
			elem.Metadata[k] = v
		}
	}

	// Also store in variables sub-map for TemplateStage
	s.enrichVariablesSubMap(elem, extracted)
}

// enrichVariablesSubMap adds extracted variables to the variables sub-map.
func (s *ScenarioContextExtractionStage) enrichVariablesSubMap(elem *stage.StreamElement, extracted map[string]string) {
	if _, ok := elem.Metadata["variables"]; !ok {
		elem.Metadata["variables"] = make(map[string]string)
	}
	if vars, ok := elem.Metadata["variables"].(map[string]string); ok {
		for k, v := range extracted {
			if _, exists := vars[k]; !exists {
				vars[k] = v
			}
		}
	}
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

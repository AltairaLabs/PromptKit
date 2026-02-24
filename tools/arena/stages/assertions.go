package stages

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// TurnEvalRunner is an interface for running assertions as evals during turn execution.
// PackEvalHook in the engine package implements this interface.
type TurnEvalRunner interface {
	RunAssertionsAsEvals(
		ctx context.Context,
		assertionConfigs []assertions.AssertionConfig,
		messages []types.Message,
		turnIndex int,
		sessionID string,
		trigger evals.EvalTrigger,
	) []evals.EvalResult
}

const roleAssistant = "assistant"

// ArenaAssertionStage validates assertions after LLM responses.
type ArenaAssertionStage struct {
	stage.BaseStage
	assertionConfigs []assertions.AssertionConfig
	turnEvalRunner   TurnEvalRunner // Eval runner for assertion execution
	sessionID        string         // Session ID for eval context
}

// NewArenaAssertionStage creates a new assertion stage.
func NewArenaAssertionStage(
	assertionConfigs []assertions.AssertionConfig,
) *ArenaAssertionStage {
	return &ArenaAssertionStage{
		BaseStage:        stage.NewBaseStage("arena_assertions", stage.StageTypeTransform),
		assertionConfigs: assertionConfigs,
	}
}

// WithTurnEvalRunner sets the eval runner for assertion execution.
func (s *ArenaAssertionStage) WithTurnEvalRunner(runner TurnEvalRunner, sessionID string) *ArenaAssertionStage {
	s.turnEvalRunner = runner
	s.sessionID = sessionID
	return s
}

// Process validates assertions on the stream elements.
func (s *ArenaAssertionStage) Process(
	ctx context.Context,
	input <-chan stage.StreamElement,
	output chan<- stage.StreamElement,
) error {
	defer close(output)

	// Skip if no assertions configured
	if len(s.assertionConfigs) == 0 {
		for elem := range input {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	// Collect all elements
	var elements []stage.StreamElement
	var metadata map[string]interface{}

	for elem := range input {
		elements = append(elements, elem)
		if elem.Metadata != nil && metadata == nil {
			metadata = elem.Metadata
		}
	}

	// Build messages list from elements for assertion execution
	messages := s.extractMessagesFromElements(elements)

	// Find the last assistant element to attach assertion results
	lastAssistantElemIdx := s.findLastAssistantElementIndex(elements)

	// Run assertions - this will return results and any errors
	validationErrors := s.runAssertionsOnElements(elements, messages, metadata, lastAssistantElemIdx)

	// Forward all elements (now with assertions attached to assistant message)
	for i := range elements {
		select {
		case output <- elements[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Emit error element if validation failed (pipeline collects errors from elements)
	if len(validationErrors) > 0 {
		validationErr := fmt.Errorf("validation failed: %v", validationErrors)
		select {
		case output <- stage.NewErrorElement(validationErr):
		case <-ctx.Done():
			return ctx.Err()
		}
		return validationErr
	}

	return nil
}

// extractMessagesFromElements extracts messages from elements for assertion validation.
func (s *ArenaAssertionStage) extractMessagesFromElements(elements []stage.StreamElement) []types.Message {
	var messages []types.Message
	for i := range elements {
		if elements[i].Message != nil {
			messages = append(messages, *elements[i].Message)
		}
	}
	return messages
}

// findLastAssistantElementIndex finds the index of the last assistant message element.
func (s *ArenaAssertionStage) findLastAssistantElementIndex(elements []stage.StreamElement) int {
	for i := len(elements) - 1; i >= 0; i-- {
		if elements[i].Message != nil && elements[i].Message.Role == roleAssistant {
			return i
		}
	}
	return -1
}

// runAssertionsOnElements executes assertions and attaches results to the original element.
func (s *ArenaAssertionStage) runAssertionsOnElements(
	elements []stage.StreamElement,
	messages []types.Message,
	metadata map[string]interface{},
	lastAssistantElemIdx int,
) []error {
	if lastAssistantElemIdx < 0 {
		return nil
	}

	// Get pointer to the actual message in the element (not a copy)
	lastAssistantMsg := elements[lastAssistantElemIdx].Message

	// Prepare validation context
	turnMessages := s.extractTurnMessages(messages)

	// Execute all assertions
	_, errors := s.executeAssertions(lastAssistantMsg, turnMessages, messages, metadata)

	return errors
}

// extractTurnMessages extracts messages from the current turn (not from StateStore).
func (s *ArenaAssertionStage) extractTurnMessages(messages []types.Message) []types.Message {
	var turnMessages []types.Message
	for i := range messages {
		if messages[i].Source != "statestore" {
			turnMessages = append(turnMessages, messages[i])
		}
	}
	return turnMessages
}

// executeAssertions runs all configured assertions via TurnEvalRunner and returns results and errors.
func (s *ArenaAssertionStage) executeAssertions(
	lastAssistantMsg *types.Message,
	turnMessages []types.Message,
	allMessages []types.Message,
	metadata map[string]interface{},
) (map[string]interface{}, []error) {
	if s.turnEvalRunner == nil {
		logger.Debug("No TurnEvalRunner configured, skipping turn assertions")
		return map[string]interface{}{
			"results": []interface{}{},
			"passed":  true,
			"total":   0,
			"failed":  0,
		}, nil
	}

	// Pre-filter assertions by when-condition
	var filteredConfigs []assertions.AssertionConfig
	var skippedResults []interface{}
	for _, ac := range s.assertionConfigs {
		if ac.When != nil {
			params := s.buildWhenParams(ac.Params, turnMessages, allMessages, metadata)
			if shouldRun, reason := ac.When.ShouldRun(params); !shouldRun {
				skippedResults = append(skippedResults, map[string]interface{}{
					"type":    ac.Type,
					"passed":  true,
					"skipped": true,
					"message": ac.Message,
					"details": map[string]interface{}{"skip_reason": reason},
				})
				continue
			}
		}
		filteredConfigs = append(filteredConfigs, ac)
	}

	// Run filtered assertions through eval runner
	evalResults := s.turnEvalRunner.RunAssertionsAsEvals(
		context.Background(), filteredConfigs, allMessages,
		len(allMessages)-1, s.sessionID,
		evals.TriggerEveryTurn,
	)

	// Convert eval results to map format for message metadata
	convResults := assertions.ConvertEvalResults(evalResults)
	results := make([]interface{}, 0, len(skippedResults)+len(convResults))
	results = append(results, skippedResults...)

	var validationErrors []error
	for i, cr := range convResults {
		resultMap := map[string]interface{}{
			"type":    cr.Type,
			"passed":  cr.Passed,
			"details": cr.Details,
			"message": cr.Message,
		}
		results = append(results, resultMap)

		if !cr.Passed {
			validationErrors = append(validationErrors,
				fmt.Errorf("assertion %d (%s) failed: %s", i, cr.Type, cr.Message))
		}
	}

	// Build summary map and attach to message metadata
	resultsMap := map[string]interface{}{
		"results": results,
		"passed":  len(validationErrors) == 0,
		"total":   len(results),
		"failed":  len(validationErrors),
	}
	s.attachResultsToMessage(lastAssistantMsg, resultsMap)

	logger.Debug("Turn assertions evaluated via eval path",
		"total", len(results),
		"eval_results", len(evalResults),
		"skipped", len(skippedResults))

	return resultsMap, validationErrors
}

// buildWhenParams builds parameters for when-condition evaluation.
func (s *ArenaAssertionStage) buildWhenParams(
	configParams map[string]interface{},
	turnMessages []types.Message,
	allMessages []types.Message,
	metadata map[string]interface{},
) map[string]interface{} {
	params := make(map[string]interface{})

	for k, v := range configParams {
		params[k] = v
	}

	params["_turn_messages"] = deepCloneMessages(turnMessages)
	params["_execution_context_messages"] = deepCloneMessages(allMessages)
	if metadata != nil {
		params["_metadata"] = deepCopyMap(metadata)
	}

	if len(turnMessages) > 0 {
		for i := len(turnMessages) - 1; i >= 0; i-- {
			if turnMessages[i].Role == roleAssistant {
				params["_assistant_message"] = turnMessages[i]
				break
			}
		}
	}

	return params
}

// attachResultsToMessage attaches validation results to the message metadata.
func (s *ArenaAssertionStage) attachResultsToMessage(msg *types.Message, results map[string]interface{}) {
	if msg.Meta == nil {
		msg.Meta = make(map[string]interface{})
	}
	msg.Meta["assertions"] = results
}

// deepCloneMessages creates a deep copy of messages.
func deepCloneMessages(messages []types.Message) []types.Message {
	if messages == nil {
		return nil
	}

	cloned := make([]types.Message, len(messages))
	for i := range messages {
		msg := &messages[i]
		cloned[i] = types.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
			LatencyMs: msg.LatencyMs,
			Source:    msg.Source,
		}

		if len(msg.ToolCalls) > 0 {
			cloned[i].ToolCalls = make([]types.MessageToolCall, len(msg.ToolCalls))
			copy(cloned[i].ToolCalls, msg.ToolCalls)
		}

		if msg.ToolResult != nil {
			result := *msg.ToolResult
			cloned[i].ToolResult = &result
		}

		if msg.Meta != nil {
			cloned[i].Meta = make(map[string]interface{})
			for k, v := range msg.Meta {
				cloned[i].Meta[k] = v
			}
		}

		if msg.CostInfo != nil {
			costInfo := *msg.CostInfo
			cloned[i].CostInfo = &costInfo
		}

		if len(msg.Validations) > 0 {
			cloned[i].Validations = make([]types.ValidationResult, len(msg.Validations))
			copy(cloned[i].Validations, msg.Validations)
		}
	}

	return cloned
}

// deepCopyMap shallow-copies a map with interface values.
func deepCopyMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

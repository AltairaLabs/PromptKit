package generate

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

const (
	apiVersion   = "promptkit.altairalabs.ai/v1alpha1"
	kindScenario = "Scenario"
	maxIDLength  = 63
	roleUser     = "user"
)

// ConvertOptions controls how a session is converted into a scenario.
type ConvertOptions struct {
	// Pack is the path to a .pack.json file. When set, the scenario is generated
	// as a workflow scenario (with Steps) instead of a regular conversation scenario.
	Pack string
}

// ConvertSessionToScenario converts a SessionDetail into a ScenarioConfig YAML document.
func ConvertSessionToScenario(session *SessionDetail, opts ConvertOptions) (*config.ScenarioConfig, error) {
	if session == nil {
		return nil, fmt.Errorf("session is nil")
	}

	scenarioID := sanitizeID(session.ID)
	description := buildDescription(session)

	scenario := config.Scenario{
		ID:          scenarioID,
		Description: description,
	}

	if opts.Pack != "" {
		scenario.Pack = opts.Pack
		scenario.Steps = buildWorkflowSteps(session)
	} else {
		scenario.TaskType = "conversation"
		scenario.Turns = buildTurns(session)
	}

	// Add conversation-level assertions from failed eval results.
	scenario.ConversationAssertions = buildConversationAssertions(session.EvalResults)

	// Add turn-level assertions from failed turn eval results.
	applyTurnAssertions(&scenario, session.TurnEvalResults, opts.Pack != "")

	return &config.ScenarioConfig{
		APIVersion: apiVersion,
		Kind:       kindScenario,
		Metadata: config.ObjectMeta{
			Name: scenarioID,
		},
		Spec: scenario,
	}, nil
}

// sanitizeID produces a valid scenario ID: lowercase alphanumeric with hyphens, max 63 chars.
func sanitizeID(raw string) string {
	// Replace non-alphanumeric characters with hyphens.
	re := regexp.MustCompile(`[^a-z0-9]+`)
	id := re.ReplaceAllString(strings.ToLower(raw), "-")

	// Trim leading/trailing hyphens.
	id = strings.Trim(id, "-")

	if id == "" {
		id = "generated"
	}

	if len(id) > maxIDLength {
		id = id[:maxIDLength]
		// Don't end with a hyphen after truncation.
		id = strings.TrimRight(id, "-")
	}

	return id
}

func buildDescription(session *SessionDetail) string {
	parts := []string{fmt.Sprintf("Generated from session %s", session.ID)}
	if !session.Timestamp.IsZero() {
		parts = append(parts, fmt.Sprintf("recorded %s", session.Timestamp.Format(time.RFC3339)))
	}
	if session.HasFailures {
		parts = append(parts, "contains assertion failures")
	}
	return strings.Join(parts, ", ")
}

func buildTurns(session *SessionDetail) []config.TurnDefinition {
	var turns []config.TurnDefinition
	for i := range session.Messages {
		if session.Messages[i].Role != roleUser {
			continue
		}
		turns = append(turns, config.TurnDefinition{
			Role:    roleUser,
			Content: session.Messages[i].GetContent(),
		})
	}
	return turns
}

func buildWorkflowSteps(session *SessionDetail) []config.WorkflowStep {
	var steps []config.WorkflowStep
	for i := range session.Messages {
		if session.Messages[i].Role != roleUser {
			continue
		}
		steps = append(steps, config.WorkflowStep{
			Type:    "input",
			Content: session.Messages[i].GetContent(),
		})
	}
	return steps
}

func buildConversationAssertions(
	results []assertions.ConversationValidationResult,
) []assertions.AssertionConfig {
	var configs []assertions.AssertionConfig
	for _, r := range results {
		if r.Passed {
			continue
		}
		ac := assertions.AssertionConfig{
			Type:    r.Type,
			Message: r.Message,
		}
		if r.Details != nil {
			ac.Params = r.Details
		}
		configs = append(configs, ac)
	}
	return configs
}

// applyTurnAssertions attaches per-turn assertions from failed TurnEvalResults
// to the appropriate turn or workflow step.
func applyTurnAssertions(
	scenario *config.Scenario,
	turnResults map[int][]TurnEvalResult,
	isWorkflow bool,
) {
	if len(turnResults) == 0 {
		return
	}

	for turnIdx, results := range turnResults {
		failedAssertions := collectFailedTurnAssertions(results)
		if len(failedAssertions) == 0 {
			continue
		}
		attachAssertions(scenario, turnIdx, failedAssertions, isWorkflow)
	}
}

func collectFailedTurnAssertions(results []TurnEvalResult) []assertions.AssertionConfig {
	var out []assertions.AssertionConfig
	for _, r := range results {
		if r.Passed {
			continue
		}
		ac := assertions.AssertionConfig{
			Type:    r.Type,
			Message: r.Message,
		}
		if r.Params != nil {
			ac.Params = r.Params
		}
		out = append(out, ac)
	}
	return out
}

func attachAssertions(
	scenario *config.Scenario,
	turnIdx int,
	asrtConfigs []assertions.AssertionConfig,
	isWorkflow bool,
) {
	if isWorkflow {
		if turnIdx < len(scenario.Steps) {
			scenario.Steps[turnIdx].Assertions = append(scenario.Steps[turnIdx].Assertions, asrtConfigs...)
		}
	} else {
		if turnIdx < len(scenario.Turns) {
			scenario.Turns[turnIdx].Assertions = append(scenario.Turns[turnIdx].Assertions, asrtConfigs...)
		}
	}
}

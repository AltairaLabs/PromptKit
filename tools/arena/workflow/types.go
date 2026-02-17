// Package workflow provides workflow scenario support for Arena.
//
// Workflow scenarios test multi-state flows where an agent transitions
// between prompt_tasks in response to events. Each step in a scenario
// is either an "input" (send a message) or an "event" (trigger a
// state transition).
//
// Example YAML:
//
//	id: support-escalation
//	pack: ./support.pack.json
//	description: "Test escalation from intake to specialist"
//	steps:
//	  - type: input
//	    content: "I need help with billing"
//	    assertions:
//	      - type: content_includes
//	        params: { substring: "billing" }
//	  - type: event
//	    event: Escalate
//	    expect_state: specialist
//	  - type: input
//	    content: "My invoice is wrong"
package workflow

import (
	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// Scenario defines a workflow test scenario that drives a
// WorkflowConversation through multiple states.
type Scenario struct {
	// ID uniquely identifies this workflow scenario.
	ID string `json:"id" yaml:"id"`

	// Pack is the path to the prompt pack file.
	Pack string `json:"pack" yaml:"pack"`

	// Description is a human-readable summary of what this scenario tests.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Steps is the ordered sequence of actions to execute.
	Steps []Step `json:"steps" yaml:"steps"`

	// Providers optionally restricts which providers to run this scenario against.
	Providers []string `json:"providers,omitempty" yaml:"providers,omitempty"`

	// Variables are injected into the pack's template variables.
	Variables map[string]string `json:"variables,omitempty" yaml:"variables,omitempty"`

	// ContextCarryForward enables conversation context hand-off between states.
	ContextCarryForward bool `json:"context_carry_forward,omitempty" yaml:"context_carry_forward,omitempty"`
}

// StepType identifies the kind of workflow step.
type StepType string

const (
	// StepInput sends a user message to the active conversation.
	StepInput StepType = "input"

	// StepEvent triggers a workflow state transition.
	StepEvent StepType = "event"
)

// Step is a single action in a workflow scenario.
type Step struct {
	// Type is "input" or "event".
	Type StepType `json:"type" yaml:"type"`

	// Content is the user message text (only for input steps).
	Content string `json:"content,omitempty" yaml:"content,omitempty"`

	// Event is the transition event name (only for event steps).
	Event string `json:"event,omitempty" yaml:"event,omitempty"`

	// ExpectState is the expected state after an event transition.
	// Validated only for event steps.
	ExpectState string `json:"expect_state,omitempty" yaml:"expect_state,omitempty"`

	// Assertions are evaluated against the assistant response (input steps only).
	Assertions []asrt.AssertionConfig `json:"assertions,omitempty" yaml:"assertions,omitempty"`
}

// Validate checks that the scenario is well-formed.
func (s *Scenario) Validate() error {
	if s.ID == "" {
		return ErrMissingID
	}
	if s.Pack == "" {
		return ErrMissingPack
	}
	if len(s.Steps) == 0 {
		return ErrNoSteps
	}
	for i, step := range s.Steps {
		if err := step.validate(i); err != nil {
			return err
		}
	}
	return nil
}

// validate checks that a single step is well-formed.
func (step *Step) validate(index int) error {
	switch step.Type {
	case StepInput:
		if step.Content == "" {
			return &StepError{Index: index, Msg: "input step requires content"}
		}
	case StepEvent:
		if step.Event == "" {
			return &StepError{Index: index, Msg: "event step requires event name"}
		}
	default:
		return &StepError{Index: index, Msg: "unknown step type: " + string(step.Type)}
	}
	return nil
}

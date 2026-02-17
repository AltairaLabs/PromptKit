package workflow

import (
	"errors"
	"fmt"
)

// Sentinel errors for scenario validation.
var (
	ErrMissingID   = errors.New("workflow scenario: missing id")
	ErrMissingPack = errors.New("workflow scenario: missing pack path")
	ErrNoSteps     = errors.New("workflow scenario: no steps defined")
)

// StepError describes a validation error for a specific step.
type StepError struct {
	Index int
	Msg   string
}

func (e *StepError) Error() string {
	return fmt.Sprintf("workflow scenario step %d: %s", e.Index, e.Msg)
}

package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// VariableInjectionStage injects variables into stream element metadata.
type VariableInjectionStage struct {
	stage.BaseStage
	variables map[string]string
}

// NewVariableInjectionStage creates a new variable injection stage.
func NewVariableInjectionStage(variables map[string]string) *VariableInjectionStage {
	return &VariableInjectionStage{
		BaseStage: stage.NewBaseStage("variable_injection", stage.StageTypeTransform),
		variables: variables,
	}
}

// Process adds variables to all elements' metadata.
func (s *VariableInjectionStage) Process(
	ctx context.Context,
	input <-chan stage.StreamElement,
	output chan<- stage.StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Ensure metadata exists
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}

		// Set variables in metadata for downstream stages
		elem.Metadata["variables"] = s.variables

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

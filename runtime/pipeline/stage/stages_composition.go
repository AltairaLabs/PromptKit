package stage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/composition/engine"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// CompositionStage runs an RFC 0010 composition to completion as a single
// pipeline stage: it reads the turn's input element, executes the composition's
// step DAG via the engine, and emits one assistant element carrying the
// structured output.
type CompositionStage struct {
	name string
	comp *composition.Composition
	eng  *engine.Engine
}

// NewCompositionStage builds a CompositionStage from a composition spec and the
// runtime collaborators its steps need.
func NewCompositionStage(name string, comp *composition.Composition, deps CompositionExecutorDeps) *CompositionStage {
	return &CompositionStage{
		name: name,
		comp: comp,
		eng:  engine.New(NewCompositionStepExecutor(deps)),
	}
}

// Name returns the stage name.
func (s *CompositionStage) Name() string { return s.name }

// Type reports this as a transform stage.
func (s *CompositionStage) Type() StageType { return StageTypeTransform }

// Process reads the first message element as the composition input (its message
// Content bytes are the input JSON), runs the composition, and emits one
// assistant element whose Content is the composition's structured output.
// History/non-message/EndOfStream elements and any elements after the first are
// forwarded unchanged.
func (s *CompositionStage) Process(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error { //nolint:lll
	defer close(out)
	executed := false
	for elem := range in {
		if executed || elem.Message == nil || elem.EndOfStream {
			select {
			case out <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		result, err := s.eng.Execute(ctx, s.comp, compositionInput(elem.Message))
		if err != nil {
			return fmt.Errorf("composition %q: %w", s.name, err)
		}
		executed = true
		assistant := &types.Message{Role: roleAssistant, Content: string(result)}
		select {
		case out <- NewMessageElement(assistant):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// compositionInput extracts the engine input JSON from a user message: a Content
// that is already valid JSON is used verbatim; otherwise it is JSON-encoded.
func compositionInput(msg *types.Message) json.RawMessage {
	c := []byte(msg.Content)
	if json.Valid(c) {
		return c
	}
	enc, _ := json.Marshal(msg.Content)
	return enc
}

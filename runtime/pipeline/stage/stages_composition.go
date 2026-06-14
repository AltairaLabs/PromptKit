package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

// Process reads the first non-history message element as the composition input
// (its message Content bytes are the input JSON), runs the composition, and
// emits one assistant element whose Content is the composition's structured
// output. History elements (elem.Meta.FromHistory == true),
// non-message/EndOfStream elements, and any elements after the first live
// message are forwarded unchanged.
func (s *CompositionStage) Process(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error { //nolint:lll // Channel signature cannot be shortened
	defer close(out)
	executed := false
	for elem := range in {
		if executed || elem.Message == nil || elem.EndOfStream || elem.Meta.FromHistory {
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

// compositionInput extracts the engine input JSON from a user message. Content
// that is an explicit JSON object or array ({...} / [...]) is used verbatim as
// the composition input; any other content (including bare scalars like `true`
// or `42` that happen to be valid JSON) is encoded as a JSON string, since a
// user message's Content is always text.
//
// GetContent() is used to unify the Content field with the Parts fallback so
// that SDK messages built via AddTextPart (which set Parts instead of Content)
// are handled correctly.
func compositionInput(msg *types.Message) json.RawMessage {
	text := msg.GetContent()
	c := []byte(strings.TrimSpace(text))
	if len(c) > 0 && (c[0] == '{' || c[0] == '[') && json.Valid(c) {
		return c
	}
	enc, _ := json.Marshal(text)
	return enc
}

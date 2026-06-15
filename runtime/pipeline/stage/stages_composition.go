package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/composition/engine"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// CompositionStage runs an RFC 0010 composition to completion as a single
// pipeline stage: it reads the turn's input element, executes the composition's
// step DAG via the engine, and emits one assistant element carrying the
// structured output.
type CompositionStage struct {
	name     string
	comp     *composition.Composition
	eng      *engine.Engine
	recorder *CompositionRecorder
	emitter  *events.Emitter
}

// NewCompositionStage builds a CompositionStage from a composition spec and the
// runtime collaborators its steps need.
func NewCompositionStage(name string, comp *composition.Composition, deps CompositionExecutorDeps) *CompositionStage {
	return &CompositionStage{
		name:    name,
		comp:    comp,
		eng:     engine.New(NewCompositionStepExecutor(deps)),
		emitter: deps.Emitter,
	}
}

// NewCompositionStageWithRecorder builds a CompositionStage that records
// step-level execution data via rec for Arena observability. rec.Reset() is
// called before each composition execution so that each turn's metadata is
// independent. A nil rec is equivalent to calling NewCompositionStage.
func NewCompositionStageWithRecorder(
	name string, comp *composition.Composition,
	deps CompositionExecutorDeps, rec *CompositionRecorder,
) *CompositionStage {
	return &CompositionStage{
		name:     name,
		comp:     comp,
		eng:      engine.NewWithRecorder(NewCompositionStepExecutor(deps), rec),
		recorder: rec,
		emitter:  deps.Emitter,
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
		result, err := s.execute(ctx, elem.Message)
		if err != nil {
			return err
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

// execute runs a single composition turn: prepares the recorder and emitter,
// brackets the engine call with composition.started / composition.completed
// events, and returns the engine output.
func (s *CompositionStage) execute(ctx context.Context, msg *types.Message) (json.RawMessage, error) {
	inputBytes := compositionInput(msg)
	if s.recorder != nil {
		if s.emitter != nil {
			s.recorder.SetEmitter(s.emitter)
		}
		s.recorder.Reset()
	}
	if s.emitter != nil {
		s.emitter.CompositionStartedCtx(ctx, s.name, inputBytes)
	}
	start := time.Now()
	result, execErr := s.eng.Execute(ctx, s.comp, inputBytes)
	if s.emitter != nil {
		var outputBytes json.RawMessage
		if result != nil {
			outputBytes = result
		}
		s.emitter.CompositionCompleted(s.name, outputBytes, execErr, time.Since(start).Milliseconds())
	}
	if execErr != nil {
		return nil, fmt.Errorf("composition %q: %w", s.name, execErr)
	}
	return result, nil
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

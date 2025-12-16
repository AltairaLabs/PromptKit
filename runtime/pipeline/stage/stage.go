package stage

import (
	"context"
)

const (
	unknownType = "unknown"
)

// Stage is a processing unit in the pipeline DAG.
// Unlike traditional middleware, stages explicitly declare their I/O characteristics
// and operate on channels of StreamElements, enabling true streaming execution.
//
// Stages read from an input channel, process elements, and write to an output channel.
// The stage MUST close the output channel when done (or when input closes).
//
// Example implementation:
//
//	type ExampleStage struct {
//	    name string
//	}
//
//	func (s *ExampleStage) Name() string {
//	    return s.name
//	}
//
//	func (s *ExampleStage) Type() StageType {
//	    return StageTypeTransform
//	}
//
//	func (s *ExampleStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
//	    defer close(output)
//
//	    for elem := range input {
//	        // Process element
//	        processedElem := s.transform(elem)
//
//	        // Write to output
//	        select {
//	        case output <- processedElem:
//	        case <-ctx.Done():
//	            return ctx.Err()
//	        }
//	    }
//
//	    return nil
//	}
type Stage interface {
	// Name returns a unique identifier for this stage.
	// This is used for logging, tracing, and debugging.
	Name() string

	// Type returns the stage's processing model.
	// This helps the pipeline builder understand how the stage behaves.
	Type() StageType

	// Process is called once when the pipeline starts.
	// The stage reads from input, processes elements, and writes to output.
	// The stage MUST close output when done (or when input closes).
	// Returns an error if processing fails.
	Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error
}

// StageType defines the processing model of a stage.
//
//nolint:revive // Intentionally named StageType for clarity; stage.Type would be too generic
type StageType int

const (
	// StageTypeTransform performs 1:1 or 1:N element transformation.
	// Each input element produces one or more output elements.
	// Examples: validation, prompt assembly, text formatting.
	StageTypeTransform StageType = iota

	// StageTypeAccumulate performs N:1 accumulation.
	// Multiple input elements are collected and combined into one output element.
	// Examples: VAD buffering, message accumulation.
	StageTypeAccumulate

	// StageTypeGenerate performs 0:N generation.
	// Generates output elements without consuming input (or consumes once then generates many).
	// Examples: LLM streaming response, TTS generation.
	StageTypeGenerate

	// StageTypeSink is a terminal stage (N:0).
	// Consumes input elements but produces no output.
	// Examples: state store save, metrics collection, logging.
	StageTypeSink

	// StageTypeBidirectional supports full duplex communication.
	// Both reads from input and writes to output concurrently.
	// Examples: WebSocket session, duplex provider.
	StageTypeBidirectional
)

// String returns the string representation of the stage type.
func (st StageType) String() string {
	switch st {
	case StageTypeTransform:
		return "transform"
	case StageTypeAccumulate:
		return "accumulate"
	case StageTypeGenerate:
		return "generate"
	case StageTypeSink:
		return "sink"
	case StageTypeBidirectional:
		return "bidirectional"
	default:
		return unknownType
	}
}

// BaseStage provides common functionality for stage implementations.
// Stages can embed this to reduce boilerplate.
type BaseStage struct {
	name      string
	stageType StageType
}

// NewBaseStage creates a new BaseStage with the given name and type.
func NewBaseStage(name string, stageType StageType) BaseStage {
	return BaseStage{
		name:      name,
		stageType: stageType,
	}
}

// Name returns the stage name.
func (b *BaseStage) Name() string {
	return b.name
}

// Type returns the stage type.
func (b *BaseStage) Type() StageType {
	return b.stageType
}

// StageFunc is a functional adapter that allows using a function as a Stage.
// This is useful for simple transformations without defining a new type.
//
//nolint:revive // Intentionally named StageFunc for clarity; stage.Func would be unclear
type StageFunc struct {
	BaseStage
	processFunc func(context.Context, <-chan StreamElement, chan<- StreamElement) error
}

// NewStageFunc creates a new functional stage.
//
//nolint:lll // Channel signature cannot be shortened
func NewStageFunc(name string, stageType StageType, fn func(context.Context, <-chan StreamElement, chan<- StreamElement) error) *StageFunc {
	return &StageFunc{
		BaseStage:   NewBaseStage(name, stageType),
		processFunc: fn,
	}
}

// Process executes the stage function.
func (sf *StageFunc) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	return sf.processFunc(ctx, input, output)
}

// PassthroughStage is a simple stage that passes all elements through unchanged.
// Useful for testing or as a placeholder.
type PassthroughStage struct {
	BaseStage
}

// NewPassthroughStage creates a new passthrough stage.
func NewPassthroughStage(name string) *PassthroughStage {
	return &PassthroughStage{
		BaseStage: NewBaseStage(name, StageTypeTransform),
	}
}

// Process passes all elements through unchanged.
//
//nolint:lll // Channel signature cannot be shortened
func (ps *PassthroughStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// FilterStage filters elements based on a predicate function.
type FilterStage struct {
	BaseStage
	predicate func(StreamElement) bool
}

// NewFilterStage creates a new filter stage.
func NewFilterStage(name string, predicate func(StreamElement) bool) *FilterStage {
	return &FilterStage{
		BaseStage: NewBaseStage(name, StageTypeTransform),
		predicate: predicate,
	}
}

// Process filters elements based on the predicate.
func (fs *FilterStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	for elem := range input {
		if fs.predicate(elem) {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// MapStage transforms elements using a mapping function.
type MapStage struct {
	BaseStage
	mapFunc func(StreamElement) (StreamElement, error)
}

// NewMapStage creates a new map stage.
func NewMapStage(name string, mapFunc func(StreamElement) (StreamElement, error)) *MapStage {
	return &MapStage{
		BaseStage: NewBaseStage(name, StageTypeTransform),
		mapFunc:   mapFunc,
	}
}

// Process transforms each element using the map function.
func (ms *MapStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	for elem := range input {
		transformed, err := ms.mapFunc(elem)
		if err != nil {
			// Send error element
			output <- NewErrorElement(err)
			return err
		}

		select {
		case output <- transformed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

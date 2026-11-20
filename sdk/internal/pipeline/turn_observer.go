package pipeline

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
)

// TurnRecordedFunc is told each time an assistant reply of a duplex session has
// entered the transcript. continues reports that the reply called tools whose
// results the model answers in a further reply, so the caller's turn is not
// over yet. It runs on the pipeline's goroutine, so it must not block for long.
type TurnRecordedFunc func(ctx context.Context, continues bool)

// turnObserverStage is a passthrough stage, placed after the state store save
// stage, that reports each reply the save stage has stored (TurnRecordedFunc)
// before forwarding it:
//
//   - a realtime reply: the element carrying its assistant Message and ending
//     it (EndOfStream), or ending it cut short by barge-in (Meta.Interrupted);
//   - a realtime reply held until it was heard: its PlayoutRelease, the turn
//     end recorded on it (Meta.PlayoutTurnEnd), not the held element, whose
//     Message the transcript does not take yet;
//   - a cascaded (VAD) turn: its EndOfTurn, which follows the turn's messages.
//
// The save stage stores a turn before it forwards the element that ends it
// (IncrementalSaveConfig.FlushEachTurn), so the transcript holds the turn when
// it is reported.
type turnObserverStage struct {
	stage.BaseStage
	recorded TurnRecordedFunc
}

func newTurnObserverStage(recorded TurnRecordedFunc) *turnObserverStage {
	return &turnObserverStage{
		BaseStage: stage.NewBaseStage("turn-observer", stage.StageTypeTransform),
		recorded:  recorded,
	}
}

// Process implements stage.Stage. It reads its input to the end, even once
// ctx is canceled, so no stage before it blocks on a send while it winds down.
func (s *turnObserverStage) Process(ctx context.Context, input <-chan stage.StreamElement,
	output chan<- stage.StreamElement) error {
	defer close(output)
	for elem := range input {
		if ends, continues := endsRecordedTurn(&elem); ends {
			s.recorded(ctx, continues)
		}
		select {
		case output <- elem:
		case <-ctx.Done():
		}
	}
	return ctx.Err()
}

// endsRecordedTurn reports whether elem ends a reply the transcript now holds,
// and whether that reply continues into a tool round.
func endsRecordedTurn(elem *stage.StreamElement) (ends, continues bool) {
	if elem.EndOfTurn {
		return true, false
	}
	msg := elem.Message
	if msg == nil || msg.Role != "assistant" || elem.Meta.PlayoutHeld {
		return false, false
	}
	if elem.Meta.PlayoutRelease {
		ends = elem.Meta.PlayoutTurnEnd
	} else {
		ends = elem.EndOfStream || elem.Meta.Interrupted
	}
	return ends, ends && len(msg.ToolCalls) > 0
}

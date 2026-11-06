package pipeline

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
)

// AudioTapFunc observes one audio element at a tap position. It must not block
// and must not retain or modify audio.Samples after it returns.
type AudioTapFunc func(position stage.RecordingPosition, audio *stage.AudioData, ts time.Time)

// audioTapStage is a passthrough stage that hands each audio element to an
// AudioTapFunc and forwards every element unchanged.
type audioTapStage struct {
	stage.BaseStage
	position stage.RecordingPosition
	tap      AudioTapFunc
}

// newAudioTapStage returns a tap for position.
func newAudioTapStage(position stage.RecordingPosition, tap AudioTapFunc) *audioTapStage {
	return &audioTapStage{
		BaseStage: stage.NewBaseStage("audio-monitor-"+string(position), stage.StageTypeTransform),
		position:  position,
		tap:       tap,
	}
}

// Process implements stage.Stage.
func (s *audioTapStage) Process(ctx context.Context, input <-chan stage.StreamElement,
	output chan<- stage.StreamElement) error {
	defer close(output)
	for elem := range input {
		if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
			s.tap(s.position, elem.Audio, elem.Timestamp)
		}
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

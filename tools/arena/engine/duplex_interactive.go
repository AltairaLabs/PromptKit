package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// RunInteractiveVoice drives a live, mic-fed voice conversation through the
// duplex pipeline until mic is closed or ctx is canceled.
//
// Mode is selected by provider capability:
//   - StreamInputSupport providers use the existing ASM pipeline
//     (buildDuplexPipeline → DuplexProviderStage → ArenaStateStoreSaveStage).
//   - Text providers use the composed VAD pipeline (Task 7, not yet implemented).
//
// Mic frames (raw PCM16 mono @ 16 kHz or provider-preferred rate) are forwarded
// to the pipeline input channel one element per frame. Response audio from the
// pipeline output channel is delivered to play concurrently; the drain goroutine
// is joined before RunInteractiveVoice returns so every response frame is
// delivered to play by the time the function exits.
//
// When mic is closed, an EndOfStream element is sent to the pipeline to signal
// end-of-user-speech to the provider (matching how processSingleDuplexTurn does
// it), then inputChan is closed and the output drain is awaited.
//
// History (transcripts + tool calls) is persisted by ArenaStateStoreSaveStage
// inside the pipeline, exactly as for a duplex scenario run.
func (de *DuplexConversationExecutor) RunInteractiveVoice(
	ctx context.Context,
	req *ConversationRequest,
	mic <-chan []byte,
	play func([]byte),
) error {
	streamProvider, ok := req.Provider.(providers.StreamInputSupport)
	if !ok {
		// Task 7 will implement a VAD-composed pipeline for text-only providers.
		return de.runInteractiveVADVoice(ctx, req, mic, play)
	}

	pipeline, err := de.buildDuplexPipeline(req, streamProvider)
	if err != nil {
		return fmt.Errorf("build duplex pipeline: %w", err)
	}

	inputChan := make(chan stage.StreamElement)
	outputChan, err := pipeline.Execute(ctx, inputChan)
	if err != nil {
		return fmt.Errorf("execute pipeline: %w", err)
	}

	// Drain the output channel in a goroutine so response audio is played back
	// concurrently with mic input streaming. The WaitGroup ensures every audio
	// frame has been delivered to play before RunInteractiveVoice returns.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		drainAudioOutput(outputChan, play)
	}()

	// Feed mic frames into the pipeline until mic closes or ctx ends, then
	// signal end-of-user-speech, close inputChan, and await drain completion.
	defer func() {
		close(inputChan)
		wg.Wait()
	}()
	return de.feedMicToPipeline(ctx, mic, inputChan)
}

// drainAudioOutput ranges over outputChan and delivers every audio frame to play.
// It exits when outputChan is closed (by the pipeline on completion or ctx cancel).
func drainAudioOutput(outputChan <-chan stage.StreamElement, play func([]byte)) {
	for elem := range outputChan {
		if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
			play(elem.Audio.Samples)
		}
	}
}

// feedMicToPipeline forwards mic frames to inputChan until mic closes or ctx is
// canceled. When mic closes it sends an EndOfStream element to trigger the
// provider's end-of-user-speech response (matching the pattern used by
// streamAudioChunks), then returns nil.
func (de *DuplexConversationExecutor) feedMicToPipeline(
	ctx context.Context,
	mic <-chan []byte,
	inputChan chan<- stage.StreamElement,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-mic:
			if !ok {
				// Mic exhausted: signal end-of-user-speech to the pipeline.
				// Ignore send errors — the pipeline may already be shutting down
				// and the deferred close+wait in RunInteractiveVoice handles cleanup.
				endElem := stage.StreamElement{EndOfStream: true}
				select {
				case inputChan <- endElem:
				case <-ctx.Done():
				}
				return nil
			}
			audioElem := stage.StreamElement{
				Audio: &stage.AudioData{
					Samples:    frame,
					SampleRate: defaultSampleRate, // 16000 Hz
					Channels:   1,
					Format:     stage.AudioFormatPCM16,
				},
			}
			select {
			case inputChan <- audioElem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// runInteractiveVADVoice is the interactive entry point for text-based providers
// that do not implement StreamInputSupport. It uses a VAD-composed pipeline to
// segment mic audio into speech turns and submits each turn as a text message.
//
// TODO(Task 7): implement the VAD-composed pipeline.
func (de *DuplexConversationExecutor) runInteractiveVADVoice(
	_ context.Context,
	_ *ConversationRequest,
	_ <-chan []byte,
	_ func([]byte),
) error {
	return fmt.Errorf("voice over text providers not yet implemented (Task 7)")
}

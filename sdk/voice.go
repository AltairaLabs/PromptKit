package sdk

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// WithAudioSession binds a duplex voice conversation to an audio.Session — the
// caller's microphone source(s) and speaker sink(s). The SDK runs the
// capture→pipeline→playback pump in Conversation.Start, so applications get
// voice I/O without touching hardware from the pipeline.
//
// The device implementation stays outside the pure-Go SDK (e.g. the PortAudio
// helper in sdk/examples/audiohelper, or audio.MemSource/MemSink for tests), so
// the SDK itself never links a sound-card binding. Use with OpenVoice, or with
// OpenDuplex when you drive Start yourself.
func WithAudioSession(sess audio.Session) Option {
	return func(c *config) error {
		c.audioSession = sess
		return nil
	}
}

// OpenVoice opens a duplex voice conversation bound to an audio.Session and
// returns it ready for Conversation.Start, which pumps the microphone into the
// pipeline and plays replies back to the speaker.
//
// It is OpenDuplex plus a required WithAudioSession — the same provider / VAD /
// ingestion options apply (WithProvider + WithVADMode for STT→LLM→TTS, or a
// streaming provider for ASM). For manual chunk I/O without a bound session, use
// OpenDuplex directly.
//
//	sess, _ := audiohelper.NewSession(audiohelper.WithCaptureRate(16000))
//	conv, _ := sdk.OpenVoice("./assistant.pack.json", "assist",
//	    sdk.WithProvider(llm),
//	    sdk.WithVADMode(stt, tts, nil),
//	    sdk.WithAudioSession(sess),
//	)
//	_ = conv.Start(ctx) // blocks: mic → LLM → speaker, until ctx is canceled
func OpenVoice(packPath, promptName string, opts ...Option) (*Conversation, error) {
	conv, err := OpenDuplex(packPath, promptName, opts...)
	if err != nil {
		return nil, err
	}
	if conv.config.audioSession == nil {
		_ = conv.Close()
		return nil, fmt.Errorf(
			"OpenVoice requires WithAudioSession; pass an audio.Session " +
				"(e.g. audiohelper.NewSession) or use OpenDuplex for manual chunk I/O")
	}
	return conv, nil
}

// WithVoiceObserver registers a callback invoked with every response chunk
// during Conversation.Start — text deltas, input-transcription metadata, tool
// events, and audio chunks alike — so an application can display or log the
// conversation while Start manages microphone and speaker.
//
// The callback runs on Start's output-pump goroutine; keep it quick and do not
// call back into the conversation from it.
//
//	sdk.WithVoiceObserver(func(c providers.StreamChunk) {
//	    if c.Delta != "" { fmt.Print(c.Delta) }
//	    if t, ok := c.Metadata["input_transcription"].(string); ok { fmt.Println("you:", t) }
//	})
func WithVoiceObserver(fn func(providers.StreamChunk)) Option {
	return func(c *config) error {
		c.voiceObserver = fn
		return nil
	}
}

// audioChunkSender is the slice of Conversation the input pump needs; narrow so
// the pump is unit-testable without a full conversation.
type audioChunkSender interface {
	SendChunk(ctx context.Context, chunk *providers.StreamChunk) error
}

// Start runs the bound audio session: it feeds every microphone source into the
// pipeline and plays the pipeline's audio replies back to the speaker sinks,
// flushing them on barge-in. It blocks until ctx is canceled or the session
// ends (a source closes or the response stream finishes), then tears the session
// down. A context-cancel shutdown returns nil.
//
// Start owns the response stream — do not also read Response() while it runs.
// Requires a session bound via OpenVoice / WithAudioSession.
func (c *Conversation) Start(ctx context.Context) error {
	c.mu.RLock()
	if err := c.requireDuplex("Start()"); err != nil {
		c.mu.RUnlock()
		return err
	}
	sess := c.config.audioSession
	c.mu.RUnlock()

	if sess == nil {
		return fmt.Errorf("Start() requires an audio session; open with OpenVoice / WithAudioSession")
	}

	respCh, err := c.Response()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 1) // only the first error is surfaced

	// A pump finishing (mic source closed, response stream ended) ends the whole
	// session; cancel unwinds the rest. The name+exit log identifies which pump
	// ended a session and why — the first pump to exit is the cause; the others
	// then see the canceled context. Invaluable for diagnosing premature closes.
	launch := func(name string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			pumpErr := fn()
			logger.Info("voice pump exited", "pump", name, "err", pumpErr)
			trySendErr(errCh, pumpErr)
		}()
	}

	// Starting the device is NOT a pump: many sessions (e.g. the PortAudio
	// helper) launch capture/playback in the background and return from Start
	// immediately. A nil return means "running", not "session over", so it must
	// not cancel the pumps — only a real start error tears the session down.
	wg.Add(1)
	go func() {
		defer wg.Done()
		e := sess.Start(ctx)
		logger.Info("voice device session.Start returned", "err", e)
		if e != nil && !errors.Is(e, context.Canceled) {
			trySendErr(errCh, e)
			cancel()
		}
	}()

	for i, src := range audioSources(sess.Sources()) {
		launch(fmt.Sprintf("mic-input-%d", i), func() error { return pumpAudioInput(ctx, src, c) })
	}
	sinks := audioSinks(sess.Sinks())
	launch("speaker-output", func() error {
		return pumpAudioOutput(ctx, respCh, sinks, c.config.voiceObserver)
	})

	wg.Wait()
	_ = sess.Close()

	select {
	case err = <-errCh:
	default:
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// trySendErr does a non-blocking send of a non-nil error onto ch.
func trySendErr(ch chan error, err error) {
	if err == nil {
		return
	}
	select {
	case ch <- err:
	default:
	}
}

// pumpAudioInput reads captured frames from a microphone source and feeds them
// into the pipeline as PCM audio chunks, until the source closes or ctx ends.
func pumpAudioInput(ctx context.Context, src audio.Source, sender audioChunkSender) error {
	frames := src.Frames()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case f, ok := <-frames:
			if !ok {
				return nil
			}
			chunk := &providers.StreamChunk{
				MediaData: &providers.StreamMediaData{
					MIMEType:   "audio/pcm",
					Data:       f.Data,
					SampleRate: f.Format.SampleRate,
					Channels:   f.Format.Channels,
				},
			}
			if err := sender.SendChunk(ctx, chunk); err != nil {
				return err
			}
		}
	}
}

// pumpAudioOutput plays the pipeline's audio replies to the sinks, flushing them
// when a chunk reports a barge-in interruption so the caller stops hearing a
// reply they talked over. Every chunk is also handed to observe (when non-nil)
// so the app can display text/transcription while Start manages the speaker.
// Runs until the response stream closes or ctx ends.
func pumpAudioOutput(
	ctx context.Context,
	resp <-chan providers.StreamChunk,
	sinks []audio.Sink,
	observe func(providers.StreamChunk),
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-resp:
			if !ok {
				return nil
			}
			if observe != nil {
				observe(chunk)
			}
			playChunkToSinks(&chunk, sinks)
		}
	}
}

// playChunkToSinks writes a response chunk's audio to the sinks, first flushing
// them if the chunk reports a barge-in interruption.
func playChunkToSinks(chunk *providers.StreamChunk, sinks []audio.Sink) {
	if chunk.Interrupted {
		for _, s := range sinks {
			s.Flush()
		}
	}
	if chunk.MediaData == nil || len(chunk.MediaData.Data) == 0 {
		return
	}
	frame := audio.MediaFrame{
		Kind: audio.KindAudio,
		Data: chunk.MediaData.Data,
		Format: audio.Format{
			SampleRate: chunk.MediaData.SampleRate,
			Channels:   chunk.MediaData.Channels,
		},
	}
	for _, s := range sinks {
		s.Write(frame)
	}
}

// audioSources returns only the audio-kind sources from a session's source set.
func audioSources(all []audio.Source) []audio.Source {
	out := make([]audio.Source, 0, len(all))
	for _, s := range all {
		if s.Kind() == audio.KindAudio {
			out = append(out, s)
		}
	}
	return out
}

// audioSinks returns only the audio-kind sinks from a session's sink set.
func audioSinks(all []audio.Sink) []audio.Sink {
	out := make([]audio.Sink, 0, len(all))
	for _, s := range all {
		if s.Kind() == audio.KindAudio {
			out = append(out, s)
		}
	}
	return out
}

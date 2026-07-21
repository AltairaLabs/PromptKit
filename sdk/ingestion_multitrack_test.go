package sdk

import (
	"context"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bytesPerTestFrame is 20ms of 8kHz 16-bit mono PCM (8000 * 0.02 * 2).
const bytesPerTestFrame = 320

// fixedSTT is a base.STTProvider that answers every transcription with the same
// line. Each track gets its own instance so the test can prove the two tracks
// transcribe independently and are labeled with the right speaker.
type fixedSTT struct{ line string }

func (f *fixedSTT) Name() string                      { return "fixed" }
func (f *fixedSTT) Type() base.ProviderType           { return base.ProviderTypeSTT }
func (f *fixedSTT) Pricing() *base.PricingDescriptor  { return nil }
func (f *fixedSTT) Validate() error                   { return nil }
func (f *fixedSTT) Init(context.Context) error        { return nil }
func (f *fixedSTT) HealthCheck(context.Context) error { return nil }
func (f *fixedSTT) Close() error                      { return nil }
func (f *fixedSTT) Transcribe(context.Context, base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: f.line}, nil
}

// feedTrackTurn pushes one loud-then-silent "turn" of 8kHz PCM16 on the given
// Source track: > MinSpeechDuration of non-silent samples, then enough silence
// (> the default turn SilenceDuration) that AudioTurnStage detects a complete
// turn and emits it to STT. Amplitude pattern (~0x20 every other byte) matches
// what drives the real audio VAD.
func feedTrackTurn(in chan<- stage.StreamElement, source string) {
	loud := make([]byte, 4800) // 300ms
	for i := 0; i+1 < len(loud); i += 2 {
		loud[i+1] = 0x20
	}
	silence := make([]byte, 48000) // 3s, comfortably over any default SilenceDuration
	pcm := append(loud, silence...)

	for off := 0; off < len(pcm); off += bytesPerTestFrame {
		end := min(off+bytesPerTestFrame, len(pcm))
		elem := stage.NewAudioElement(&stage.AudioData{
			Samples:    pcm[off:end],
			SampleRate: 8000,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		})
		elem.Source = source
		in <- elem
	}
}

// TestMultiTrackIngestion_RoutesTranscribesLabelsAndBroadcasts drives the
// IngestionFunc built by MultiTrackIngestion directly through a PipelineBuilder
// (bypassing OpenDuplex) to deterministically prove its whole contract:
//
//  1. Elements route to their own per-track resample→VAD→STT pipeline by
//     StreamElement.Source.
//  2. Each track transcribes independently (distinct STT per track).
//  3. Each transcript becomes a role="user" Message prefixed with the track's
//     speaker label, and OnTranscript fires per completed turn.
//  4. Control signals (EndOfStream) with no Source are broadcast to every track
//     rather than dropped — so they reach the merge output and Drain/Close of a
//     real duplex session can't deadlock waiting on a merge that never sees
//     end-of-stream (the #1638 failure class).
//  5. The whole graph passes PipelineBuilder.Build validation — i.e. no
//     duplicate stage registration (the AddStage+Chain double-register footgun).
func TestMultiTrackIngestion_RoutesTranscribesLabelsAndBroadcasts(t *testing.T) {
	var mu sync.Mutex
	var transcripts []string
	onTranscript := func(speaker, text string) {
		mu.Lock()
		transcripts = append(transcripts, speaker+": "+text)
		mu.Unlock()
	}

	ingest := MultiTrackIngestion(MultiTrackIngestionConfig{
		Tracks: []IngestionTrack{
			{Source: "caller", Speaker: "CUSTOMER", STT: &fixedSTT{line: "hello from caller"}},
			{Source: "agent", Speaker: "AGENT", STT: &fixedSTT{line: "hi from agent"}},
		},
		OnTranscript: onTranscript,
	})

	b := stage.NewPipelineBuilder()
	outputNode, err := ingest(b)
	require.NoError(t, err)

	var sinkMu sync.Mutex
	var messages []string
	var sawEndOfStream bool
	sink := stage.NewMapStage("sink", func(elem stage.StreamElement) (stage.StreamElement, error) {
		sinkMu.Lock()
		if elem.Message != nil {
			messages = append(messages, elem.Message.GetContent())
		}
		if elem.EndOfStream {
			sawEndOfStream = true
		}
		sinkMu.Unlock()
		return elem, nil
	})
	b.AddStage(sink)
	b.Connect(outputNode, sink.Name())

	pipe, err := b.Build()
	require.NoError(t, err, "the ingestion sub-graph must pass build validation (no duplicate registration)")

	in := make(chan stage.StreamElement, 512)
	out, err := pipe.Execute(context.Background(), in)
	require.NoError(t, err)

	feedTrackTurn(in, "caller")
	feedTrackTurn(in, "agent")
	// A control signal with no Source must be broadcast to every track, not
	// dropped for lack of a matching route.
	in <- stage.StreamElement{EndOfStream: true}
	close(in)

	for range out { // drain to completion; if merge deadlocked this would hang
	}

	mu.Lock()
	assert.ElementsMatch(t,
		[]string{"CUSTOMER: hello from caller", "AGENT: hi from agent"},
		transcripts,
		"each track must transcribe independently and fire OnTranscript with its speaker label")
	mu.Unlock()

	sinkMu.Lock()
	assert.ElementsMatch(t,
		[]string{"CUSTOMER: hello from caller", "AGENT: hi from agent"},
		messages,
		"each transcript must reach the merge output as a speaker-labeled Message")
	assert.True(t, sawEndOfStream,
		"EndOfStream must be broadcast through the tracks to the merge output, not dropped")
	sinkMu.Unlock()
}

// TestMultiTrackIngestion_SingleTrack proves the helper is not two-party-only:
// one track builds and routes correctly through the same router/merge topology.
func TestMultiTrackIngestion_SingleTrack(t *testing.T) {
	ingest := MultiTrackIngestion(MultiTrackIngestionConfig{
		Tracks: []IngestionTrack{
			{Source: "caller", Speaker: "CUSTOMER", STT: &fixedSTT{line: "solo"}},
		},
	})

	b := stage.NewPipelineBuilder()
	outputNode, err := ingest(b)
	require.NoError(t, err)

	var sinkMu sync.Mutex
	var messages []string
	sink := stage.NewMapStage("sink", func(elem stage.StreamElement) (stage.StreamElement, error) {
		if elem.Message != nil {
			sinkMu.Lock()
			messages = append(messages, elem.Message.GetContent())
			sinkMu.Unlock()
		}
		return elem, nil
	})
	b.AddStage(sink)
	b.Connect(outputNode, sink.Name())

	pipe, err := b.Build()
	require.NoError(t, err)

	in := make(chan stage.StreamElement, 512)
	out, err := pipe.Execute(context.Background(), in)
	require.NoError(t, err)

	feedTrackTurn(in, "caller")
	close(in)
	for range out {
	}

	sinkMu.Lock()
	assert.Equal(t, []string{"CUSTOMER: solo"}, messages)
	sinkMu.Unlock()
}

// TestMultiTrackIngestion_RejectsEmptyTracks guards the obvious misuse.
func TestMultiTrackIngestion_RejectsEmptyTracks(t *testing.T) {
	ingest := MultiTrackIngestion(MultiTrackIngestionConfig{})
	_, err := ingest(stage.NewPipelineBuilder())
	require.Error(t, err)
	assert.ErrorContains(t, err, "at least one track")
}

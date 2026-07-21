package main

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// bytesPerFrame is 20ms of 8kHz 16-bit mono PCM (8000 * 0.02 * 2).
const bytesPerFrame = 320

// turn is one speaker's utterance in the built-in demo conversation.
type turn struct {
	Source  string // routing key: matches StreamChunk.Source and IngestionTrack.Source
	Speaker string // label prefixed onto the transcribed Message ("SPEAKER-A")
	Line    string // what scriptedSTT returns for this turn
}

// script is a short, domain-neutral two-person exchange. Kept tiny: each turn
// is transcribed by scriptedSTT and drives one agent firing.
var script = []turn{
	{"speaker-a", "SPEAKER-A", "Morning — are we still on for the two o'clock review?"},
	{"speaker-b", "SPEAKER-B", "Yes, I've booked the small room and sent the agenda."},
	{"speaker-a", "SPEAKER-A", "Great, I'll bring the latency numbers from last week."},
	{"speaker-b", "SPEAKER-B", "Perfect. Let's keep it to thirty minutes."},
}

// scriptedSTT is a base.STTProvider that replays one source's script lines in
// order. It deliberately ignores the audio — it stands in for a transcriber so
// the demo runs without an STT key. A real run (--live) still uses this stub;
// see the README for why (--live only swaps the LLM).
type scriptedSTT struct {
	mu    sync.Mutex
	lines []string
	next  int
}

func newScriptedSTT(source string) *scriptedSTT {
	var lines []string
	for _, tn := range script {
		if tn.Source == source {
			lines = append(lines, tn.Line)
		}
	}
	return &scriptedSTT{lines: lines}
}

func (s *scriptedSTT) Name() string                      { return "scripted" }
func (s *scriptedSTT) Type() base.ProviderType           { return base.ProviderTypeSTT }
func (s *scriptedSTT) Pricing() *base.PricingDescriptor  { return nil }
func (s *scriptedSTT) Validate() error                   { return nil }
func (s *scriptedSTT) Init(context.Context) error        { return nil }
func (s *scriptedSTT) HealthCheck(context.Context) error { return nil }
func (s *scriptedSTT) Close() error                      { return nil }

func (s *scriptedSTT) Transcribe(context.Context, base.STTRequest) (base.STTResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.next >= len(s.lines) {
		return base.STTResponse{Text: ""}, nil
	}
	line := s.lines[s.next]
	s.next++
	return base.STTResponse{Text: line}, nil
}

// synthTurnPCM returns one turn of 8kHz PCM16: ~300ms of non-silent samples
// followed by ~2s of silence. AudioTurnStage measures turn boundaries in
// accumulated audio samples (not wall-clock), so this closes a real VAD turn
// even when burst-fed. The amplitude pattern (~0x20 every other byte) is what
// the real audio VAD reads as speech.
func synthTurnPCM() []byte {
	loud := make([]byte, 4800) // 300ms
	for i := 0; i+1 < len(loud); i += 2 {
		loud[i+1] = 0x20
	}
	silence := make([]byte, 32000) // 2s > default SilenceDuration
	return append(loud, silence...)
}

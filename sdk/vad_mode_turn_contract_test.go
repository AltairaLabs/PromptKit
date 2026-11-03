package sdk

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// vadContractEvent is one chunk from Response() reduced to the parts of the
// duplex turn contract a caller keys on (#2287).
type vadContractEvent struct {
	transcript   string
	turnComplete bool
	interrupted  bool
}

// vadContractRecorder collects the contract-relevant chunks from Response().
type vadContractRecorder struct {
	mu     sync.Mutex
	events []vadContractEvent
}

func (r *vadContractRecorder) consume(ch <-chan providers.StreamChunk) {
	for chunk := range ch {
		var ev vadContractEvent
		ev.transcript, _ = chunk.Metadata["input_transcription"].(string)
		ev.turnComplete, _ = chunk.Metadata["assistant_turn_complete"].(bool)
		ev.interrupted = chunk.Interrupted
		if ev.transcript == "" && !ev.turnComplete && !ev.interrupted {
			continue
		}
		r.mu.Lock()
		r.events = append(r.events, ev)
		r.mu.Unlock()
	}
}

func (r *vadContractRecorder) snapshot() []vadContractEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]vadContractEvent, len(r.events))
	copy(out, r.events)
	return out
}

func (r *vadContractRecorder) transcripts() []string {
	var out []string
	for _, ev := range r.snapshot() {
		if ev.transcript != "" {
			out = append(out, ev.transcript)
		}
	}
	return out
}

func (r *vadContractRecorder) turnCompletes() int {
	n := 0
	for _, ev := range r.snapshot() {
		if ev.turnComplete {
			n++
		}
	}
	return n
}

func (r *vadContractRecorder) sawInterrupt() bool {
	for _, ev := range r.snapshot() {
		if ev.interrupted {
			return true
		}
	}
	return false
}

// waitFor polls cond until it holds or the timeout passes.
func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// TestVADMode_ResponseCarriesTranscriptAndTurnBoundary holds VAD-mode duplex to
// the continuous realtime contract (#2287): every user turn reaches Response()
// as an input_transcription chunk carrying the STT text, and every assistant
// turn ends with an assistant_turn_complete chunk.
func TestVADMode_ResponseCarriesTranscriptAndTurnBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("drives two real VAD turns")
	}
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(mock.NewProvider("mock-vad", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("first question", "second question"), newConvMockTTSService(), vadTestConfig()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	responseCh, err := conv.Response()
	require.NoError(t, err)
	rec := &vadContractRecorder{}
	go rec.consume(responseCh)

	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, 8*time.Second),
		"the first assistant turn must end with assistant_turn_complete; got %+v", rec.snapshot())

	speakOneUtterance(t, conv)
	require.True(t, waitFor(func() bool { return rec.turnCompletes() >= 2 }, 8*time.Second),
		"the second assistant turn must end with assistant_turn_complete; got %+v", rec.snapshot())

	assert.Equal(t, []string{"first question", "second question"}, rec.transcripts(),
		"each user turn must reach Response() once as input_transcription")
	assert.Equal(t, 2, rec.turnCompletes(), "exactly one boundary per assistant turn")

	// Each turn's transcript precedes that turn's boundary.
	events := rec.snapshot()
	require.Len(t, events, 4)
	assert.Equal(t, "first question", events[0].transcript)
	assert.True(t, events[1].turnComplete)
	assert.Equal(t, "second question", events[2].transcript)
	assert.True(t, events[3].turnComplete)
}

// TestVADMode_BargeInTurnStillGetsBoundary checks a reply cut short by barge-in
// still closes its assistant turn: the caller gets an Interrupted chunk (#2264)
// and an assistant_turn_complete for that turn (#2287), so a UI can close the
// assistant's line before the next user turn.
func TestVADMode_BargeInTurnStillGetsBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns")
	}
	ttsSvc := newPricedPCMTTS(24000)
	ttsSvc.block = true

	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(mock.NewProvider("mock-vad", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("first", "second"), ttsSvc, vadTestConfig()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	responseCh, err := conv.Response()
	require.NoError(t, err)
	rec := &vadContractRecorder{}
	go rec.consume(responseCh)

	speakOneUtterance(t, conv)
	select {
	case <-ttsSvc.started:
	case <-time.After(8 * time.Second):
		t.Fatal("the reply never started synthesizing")
	}

	// The caller talks over the bot.
	speakOneUtterance(t, conv)

	require.True(t, waitFor(rec.sawInterrupt, 8*time.Second),
		"barge-in never reached the caller as an Interrupted chunk; got %+v", rec.snapshot())
	assert.True(t, waitFor(func() bool { return rec.turnCompletes() >= 1 }, 2*time.Second),
		"the interrupted assistant turn must still end with assistant_turn_complete; got %+v", rec.snapshot())
	assert.Contains(t, rec.transcripts(), "first", "the interrupted turn's user transcript must reach the caller")

	// The cut-short turn closes before its Interrupted chunk (as the docs say).
	boundaryAt, interruptAt := -1, -1
	for i, ev := range rec.snapshot() {
		if ev.turnComplete && boundaryAt < 0 {
			boundaryAt = i
		}
		if ev.interrupted && interruptAt < 0 {
			interruptAt = i
		}
	}
	assert.Less(t, boundaryAt, interruptAt, "the interrupted turn's boundary precedes its Interrupted chunk")
}

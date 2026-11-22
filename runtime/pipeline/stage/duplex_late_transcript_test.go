package stage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// finalTranscriptChunk is a provider-marked final input transcript (OpenAI
// Realtime's conversation.item.input_audio_transcription.completed).
func finalTranscriptChunk(text string) providers.StreamChunk {
	return providers.StreamChunk{Metadata: map[string]interface{}{
		"type": "input_transcription", "transcription": text, "transcription_final": true,
	}}
}

// userTurns returns the user messages in elems, each with whether it was
// marked a late transcript.
func userTurns(elems []StreamElement) []string {
	var out []string
	for i := range elems {
		if m := elems[i].Message; m != nil && m.Role == core.RoleUser {
			if elems[i].Meta.LateTranscript {
				out = append(out, "late:"+m.Content)
			} else {
				out = append(out, m.Content)
			}
		}
	}
	return out
}

// A final transcript that lands after the reply to it completed is that
// turn's user message: it is marked late (so the reorder stage cannot take it
// for the next turn's), and the hooks' history has it ahead of the reply it
// prompted, not after it. The next turn's transcript, arriving before its
// reply, is an ordinary user turn.
func TestDuplexLateTranscript_FillsTheAnsweredTurn(t *testing.T) {
	h := &capturingHook{}
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(h)))

	elems, err := feedChunks(t, s,
		transcriptChunk("Hello.", nil), stopChunk(),
		finalTranscriptChunk("Hi there"),
		finalTranscriptChunk("Again"),
		providers.StreamChunk{Content: "ok"}, stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"late:Hi there", "Again"}, userTurns(elems))
	require.Len(t, h.calls, 2)
	var got []string
	for _, m := range h.calls[1].Messages {
		got = append(got, m.Role+":"+m.Content)
	}
	assert.Equal(t, []string{"user:Hi there", "assistant:Hello.", "user:Again"}, got)
}

// A provider that sends incremental transcripts and then a final one does not
// get the user turn twice: the turn materialized from the deltas stands, and
// the final (same or shorter text) is dropped.
func TestDuplexLateTranscript_FinalAfterDeltasNotDuplicated(t *testing.T) {
	s := newDuplexStageForUnit()
	elems, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "Hi there",
		}},
		providers.StreamChunk{Content: "Hello."}, // materializes the user turn from the deltas
		stopChunk(),
		finalTranscriptChunk("Hi there"),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"Hi there"}, userTurns(elems))
}

// A late transcript arriving while the answered turn's tool round runs stays
// in that turn: it opens no new tool turn (so the turn's tool limits still
// count its earlier calls), and the turn-end of the round the stage runs is
// marked ToolCallsRunning.
func TestDuplexLateTranscript_OpensNoNewToolTurn(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})

	h.emit(toolCallTurn(weatherCall("c1")))
	h.awaitSent(1)
	turn := h.toolTurn()
	require.NotNil(t, turn)
	h.emit([]providers.StreamChunk{finalTranscriptChunk("weather in Paris?")})
	h.emit(toolCallTurn(weatherCall("c2"))) // the reply to the round calls again
	h.awaitSent(2)
	assert.Same(t, turn, h.toolTurn(), "the late transcript must not begin a new tool turn")
	h.emit(textTurn("Sunny.", nil))

	elems := h.finish()
	assert.Equal(t, []string{"late:weather in Paris?"}, userTurns(elems))
	var running []bool
	for i := range elems {
		if m := elems[i].Message; elems[i].EndOfStream && m != nil && m.Role == core.RoleAssistant {
			running = append(running, elems[i].Meta.ToolCallsRunning)
		}
	}
	assert.Equal(t, []bool{true, true, false}, running, "only the turn-ends whose calls the stage runs")
}

// The slow-tool sequence end to end, duplex stage into reorder stage: the
// tool runs for several hold windows, the user's transcript lands after the
// round, and the stored order is still user, tool call, result, reply — no
// placeholder, because the hold does not time out while the stage runs the
// round.
func TestDuplexLateTranscript_SlowToolRoundKeepsTheTranscript(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), delay: 150 * time.Millisecond}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})
	reorder := NewTranscriptReorderStageWithTimeout("[none]", 30*time.Millisecond)
	out := make(chan StreamElement, 64)
	go func() { _ = reorder.Process(context.Background(), h.output, out) }()

	h.emit(append([]providers.StreamChunk{transcriptChunk("Let me check.", nil)}, toolCallTurn(weatherCall("c1"))...))
	h.awaitSent(1)
	h.emit([]providers.StreamChunk{finalTranscriptChunk("weather in Paris?")})
	h.emit(textTurn("Sunny.", nil))
	_ = h.sess().MockStreamSession.Close()
	<-h.done

	var got []string
	for e := range out {
		switch m := e.Message; {
		case m == nil:
		case m.Role == core.RoleTool:
			got = append(got, "tool:"+m.ToolResult.ID)
		case len(m.ToolCalls) > 0:
			got = append(got, m.Role+":"+m.Content+" calls "+m.ToolCalls[0].ID)
		default:
			got = append(got, m.Role+":"+m.Content)
		}
	}
	assert.Equal(t, []string{
		"user:weather in Paris?", "assistant:Let me check. calls c1", "tool:c1", "assistant:Sunny.",
	}, got)
}

// Once the next reply has started, a final transcript is that reply's turn's,
// not a late one for the finished turn before it.
func TestDuplexLateTranscript_TranscriptDuringNextReplyIsThatTurns(t *testing.T) {
	s := newDuplexStageForUnit()
	elems, err := feedChunks(t, s,
		transcriptChunk("Hello.", nil), stopChunk(), // turn 1: no transcript
		transcriptChunk("Sure", nil), // turn 2's reply starts
		finalTranscriptChunk("q2"),
		stopChunk(),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"q2"}, userTurns(elems))
}

// A final transcript with no text materializes no user turn.
func TestDuplexLateTranscript_EmptyFinalTranscriptEmitsNothing(t *testing.T) {
	s := newDuplexStageForUnit()
	elems, err := feedChunks(t, s, finalTranscriptChunk(""))
	require.NoError(t, err)
	assert.Empty(t, userTurns(elems))
}

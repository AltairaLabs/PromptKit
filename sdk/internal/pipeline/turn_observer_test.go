package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// recordingReporter records what a turn observer reports.
type recordingReporter struct {
	turns        []TurnRecord
	begun, ended int
}

func (r *recordingReporter) Begin()                                       { r.begun++ }
func (r *recordingReporter) End()                                         { r.ended++ }
func (r *recordingReporter) TurnRecorded(_ context.Context, t TurnRecord) { r.turns = append(r.turns, t) }

// observeTurns runs elems through a turn observer stage that waits no time
// for late transcripts, and returns what it reported (each report's
// continues) and what it forwarded.
func observeTurns(t *testing.T, elems ...stage.StreamElement) (reports []bool, forwarded int) {
	t.Helper()
	r := &recordingReporter{}
	s := newTurnObserverStage(r, -1)
	defer func() {
		for _, turn := range r.turns {
			reports = append(reports, turn.Continues)
		}
		assert.Equal(t, 1, r.begun)
		assert.Equal(t, 1, r.ended)
	}()
	in := make(chan stage.StreamElement, len(elems))
	out := make(chan stage.StreamElement, len(elems))
	for _, e := range elems {
		in <- e
	}
	close(in)
	require.NoError(t, s.Process(context.Background(), in, out))
	for range out {
		forwarded++
	}
	return reports, forwarded
}

func msgElem(role string, endOfStream bool) stage.StreamElement {
	return stage.StreamElement{Message: &types.Message{Role: role, Content: "x"}, EndOfStream: endOfStream}
}

// The stage reports the elements that end a reply the transcript holds, and
// no others, and forwards every element.
func TestTurnObserver_ReportsRecordedTurnEnds(t *testing.T) {
	held := msgElem("assistant", true)
	held.Meta.PlayoutHeld = true
	release := msgElem("assistant", false)
	release.Meta.PlayoutRelease = true
	release.Meta.PlayoutTurnEnd = true
	releasedUser := msgElem("user", false)
	releasedUser.Meta.PlayoutRelease = true
	releasedUser.Meta.PlayoutTurnEnd = true
	bargedIn := msgElem("assistant", false)
	bargedIn.Meta.Interrupted = true
	toolCall := msgElem("assistant", true)
	toolCall.Message.ToolCalls = []types.MessageToolCall{{ID: "c1", Name: "lookup"}}
	text := "hello"

	cases := []struct {
		name string
		elem stage.StreamElement
		want []bool
	}{
		{"a completed reply", msgElem("assistant", true), []bool{false}},
		{"a reply barge-in cut short", bargedIn, []bool{false}},
		{"a reply that calls tools", toolCall, []bool{true}},
		{"a held reply, not yet in the transcript", held, nil},
		{"a held reply released", release, []bool{false}},
		{"a cascaded turn's end", stage.NewEndOfTurnElement(), []bool{false}},
		{"a user message ending input", msgElem("user", true), nil},
		{"a released user message", releasedUser, nil},
		{"a reply message mid-stream", msgElem("assistant", false), nil},
		{"live text", stage.StreamElement{Text: &text}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reports, forwarded := observeTurns(t, tc.elem)
			assert.Equal(t, tc.want, reports)
			assert.Equal(t, 1, forwarded)
		})
	}
}

// runObserver runs elems through an observer that waits wait for late
// transcripts, sending each element once the observer has forwarded the one
// before (and pausing pause after each), and returns its reports.
func runObserver(t *testing.T, wait, pause time.Duration, elems ...stage.StreamElement) []TurnRecord {
	t.Helper()
	r := &recordingReporter{}
	s := newTurnObserverStage(r, wait)
	in := make(chan stage.StreamElement)
	out := make(chan stage.StreamElement, len(elems))
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), in, out) }()
	for _, e := range elems {
		in <- e
		<-out
		time.Sleep(pause)
	}
	close(in)
	require.NoError(t, <-done)
	return r.turns
}

func lateUser() stage.StreamElement {
	e := msgElem("user", false)
	e.Meta.LateTranscript = true
	return e
}

// A reply stored after its caller's words is reported at once, over the
// transcript so far.
func TestTurnObserver_CallerFirstReportsAtOnce(t *testing.T) {
	got := runObserver(t, time.Hour, 0, msgElem("user", false), msgElem("assistant", true))
	assert.Equal(t, []TurnRecord{{Stored: 2}}, got)
}

// A reply with no caller words before it waits for the caller's late
// transcript and is reported over it.
func TestTurnObserver_WaitsForTheLateTranscript(t *testing.T) {
	got := runObserver(t, time.Hour, 0, msgElem("assistant", true), lateUser())
	assert.Equal(t, []TurnRecord{{Stored: 2}}, got)
}

// A late transcript that does not come within the wait leaves the turn
// reported without it, and its arrival afterwards reports nothing more.
func TestTurnObserver_ReportsWithoutATranscriptThatNeverCame(t *testing.T) {
	got := runObserver(t, 20*time.Millisecond, 100*time.Millisecond, msgElem("assistant", true), lateUser())
	assert.Equal(t, []TurnRecord{{Stored: 1}}, got)
}

// The next caller turn's words, or the next reply, report the waiting turn
// without them; a turn still waiting when the input ends is reported too.
func TestTurnObserver_NextTurnReportsTheWaitingOne(t *testing.T) {
	got := runObserver(t, time.Hour, 0,
		msgElem("assistant", true), msgElem("user", false), msgElem("assistant", true))
	assert.Equal(t, []TurnRecord{{Stored: 1}, {Stored: 3}}, got)

	got = runObserver(t, time.Hour, 0, msgElem("assistant", true), msgElem("assistant", true))
	assert.Equal(t, []TurnRecord{{Stored: 1}, {Stored: 2}}, got)

	got = runObserver(t, time.Hour, 0, msgElem("assistant", true))
	assert.Equal(t, []TurnRecord{{Stored: 1}}, got)
}

// A reply that calls tools is reported at once, as continuing.
func TestTurnObserver_ToolRoundReportedAtOnce(t *testing.T) {
	toolCall := msgElem("assistant", true)
	toolCall.Message.ToolCalls = []types.MessageToolCall{{ID: "c1", Name: "lookup"}}
	got := runObserver(t, time.Hour, 0, msgElem("user", false), toolCall, msgElem("assistant", true))
	assert.Equal(t, []TurnRecord{{Continues: true, Stored: 2}, {Stored: 3}}, got)
}

// syncReporter is a recordingReporter safe to read while the stage runs.
type syncReporter struct {
	mu    sync.Mutex
	turns []TurnRecord
}

func (r *syncReporter) Begin() {}
func (r *syncReporter) End()   {}
func (r *syncReporter) TurnRecorded(_ context.Context, t TurnRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns = append(r.turns, t)
}
func (r *syncReporter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.turns)
}

// reportsAsForwarded sends each element and returns how many turns had been
// reported by the time the observer forwarded it. A turn reported at once is
// reported before its element is forwarded.
func reportsAsForwarded(t *testing.T, wait, pause time.Duration, elems ...stage.StreamElement) []int {
	t.Helper()
	r := &syncReporter{}
	s := newTurnObserverStage(r, wait)
	in := make(chan stage.StreamElement)
	out := make(chan stage.StreamElement)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), in, out) }()
	var counts []int
	for _, e := range elems {
		in <- e
		<-out
		counts = append(counts, r.count())
		time.Sleep(pause)
	}
	close(in)
	require.NoError(t, <-done)
	return counts
}

// Whether a reply waits depends on whether the caller spoke since the last
// turn reported: a reply right after one reported waits, and so does one
// after a late transcript that came too late for its own turn. Only a
// caller's message counts as speaking: a tool result does not.
func TestTurnObserver_WaitsOnlyWithoutNewCallerWords(t *testing.T) {
	assert.Equal(t, []int{0, 1, 1},
		reportsAsForwarded(t, time.Hour, 0, msgElem("user", false), msgElem("assistant", true),
			msgElem("assistant", true)),
		"the first reply is reported at once; the second, with no new caller words, waits")
	assert.Equal(t, []int{0, 1, 1},
		reportsAsForwarded(t, 20*time.Millisecond, 60*time.Millisecond,
			msgElem("assistant", true), lateUser(), msgElem("assistant", true)),
		"a late transcript after its turn was reported is not the next turn's words")
	tool := msgElem("tool", false)
	assert.Equal(t, []int{0, 0, 1},
		reportsAsForwarded(t, time.Hour, 0, msgElem("assistant", true), tool, lateUser()),
		"a tool result neither completes nor releases the waiting turn")
}

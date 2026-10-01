package agui

import (
	"fmt"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is the AG-UI 1.0 event-sequence guard. Every adapter test runs its
// output through validateAGUISequence (see runAndCollect), so a change that
// drops a terminal event, leaves a message or tool call open, unbalances a step
// or answers a call twice fails the suite rather than a consumer.
//
// The rules are the producer obligations in the 1.0 spec:
//   - events/lifecycle.mdx: a stream opens with RUN_STARTED or RUN_ERROR; no
//     nested RUN_STARTED; after RUN_FINISHED only RUN_STARTED or a late
//     RUN_ERROR; after RUN_ERROR only RUN_STARTED; RUN_FINISHED needs every
//     message, tool call, reasoning item and step the run opened to be closed,
//     and agrees with RUN_STARTED on runId; a step is never opened twice nor
//     finished without being opened.
//   - basic/patterns/streaming.mdx: an item is never opened while open, and no
//     content or end event names an item that is not open.
//   - events/tool-calls.mdx: TOOL_CALL_RESULT answers a closed call (it does
//     not reopen it), once, and a reopening TOOL_CALL_START agrees with the
//     call's name.
// A producer stream also has to end: the adapter closes its channel when the
// run is over, so a stream whose last run is still open lost its terminal
// event.

type runPhase int

const (
	phaseNone runPhase = iota
	phaseActive
	phaseFinished
	phaseErrored
)

// sequenceValidator tracks the lifecycle state of one producer stream.
type sequenceValidator struct {
	phase    runPhase
	runID    string
	messages map[string]bool // open text messages
	calls    map[string]bool // open tool calls
	steps    map[string]bool // open steps
	phases   map[string]bool // open REASONING_START phases
	thoughts map[string]bool // open reasoning messages
	names    map[string]string
	answered map[string]bool
}

func newSequenceValidator() *sequenceValidator {
	v := &sequenceValidator{names: map[string]string{}, answered: map[string]bool{}}
	v.resetRun()
	return v
}

func (v *sequenceValidator) resetRun() {
	v.messages = map[string]bool{}
	v.calls = map[string]bool{}
	v.steps = map[string]bool{}
	v.phases = map[string]bool{}
	v.thoughts = map[string]bool{}
}

// validateAGUISequence returns the first AG-UI 1.0 lifecycle violation in a
// complete producer stream, or nil when the stream conforms.
func validateAGUISequence(evts []aguievents.Event) error {
	_, err := replay(evts)
	return err
}

// replay runs evts through a validator and returns the phase the stream ended
// in, with the first violation.
func replay(evts []aguievents.Event) (runPhase, error) {
	if len(evts) == 0 {
		return phaseNone, fmt.Errorf("empty stream: a stream must begin with RUN_STARTED or RUN_ERROR")
	}
	v := newSequenceValidator()
	for i, ev := range evts {
		if err := checkEventFields(ev); err != nil {
			return v.phase, fmt.Errorf("event %d (%s): %w", i, ev.Type(), err)
		}
		if err := v.accept(ev); err != nil {
			return v.phase, fmt.Errorf("event %d (%s): %w", i, ev.Type(), err)
		}
	}
	if v.phase == phaseActive {
		return v.phase, fmt.Errorf(
			"stream ended with run %q still open: its RUN_FINISHED or RUN_ERROR never arrived", v.runID)
	}
	return v.phase, nil
}

// checkEventFields runs the AG-UI Go SDK's own field validation on every
// event, since a Go consumer decoding the stream applies it. It is stricter
// than the spec in one place: TOOL_CALL_RESULT content must not be empty.
func checkEventFields(ev aguievents.Event) error {
	return ev.Validate()
}

func (v *sequenceValidator) accept(ev aguievents.Event) error {
	switch ev.Type() {
	case aguievents.EventTypeRunStarted:
		return v.runStarted(ev.(*aguievents.RunStartedEvent))
	case aguievents.EventTypeRunFinished:
		return v.runFinished(ev.(*aguievents.RunFinishedEvent))
	case aguievents.EventTypeRunError:
		return v.runError()
	}
	if v.phase != phaseActive {
		return fmt.Errorf("event outside an active run (lifecycle.mdx: nothing follows a closed run but RUN_STARTED or RUN_ERROR)")
	}
	return v.acceptInRun(ev)
}

func (v *sequenceValidator) runStarted(ev *aguievents.RunStartedEvent) error {
	if v.phase == phaseActive {
		return fmt.Errorf("RUN_STARTED while run %q is still active", v.runID)
	}
	v.phase = phaseActive
	v.runID = ev.RunID()
	v.resetRun()
	return nil
}

func (v *sequenceValidator) runFinished(ev *aguievents.RunFinishedEvent) error {
	if v.phase != phaseActive {
		return fmt.Errorf("RUN_FINISHED without an active run")
	}
	if ev.RunID() != v.runID {
		return fmt.Errorf("RUN_FINISHED runId %q does not match RUN_STARTED runId %q", ev.RunID(), v.runID)
	}
	for kind, open := range map[string]map[string]bool{
		"text message": v.messages, "tool call": v.calls, "step": v.steps,
		"reasoning phase": v.phases, "reasoning message": v.thoughts,
	} {
		for id := range open {
			return fmt.Errorf("RUN_FINISHED with %s %q still open", kind, id)
		}
	}
	v.phase = phaseFinished
	return nil
}

func (v *sequenceValidator) runError() error {
	if v.phase == phaseErrored {
		return fmt.Errorf("RUN_ERROR after RUN_ERROR")
	}
	// RUN_ERROR is admitted first, mid-run, and late after RUN_FINISHED; it
	// ends whatever the run left open.
	v.phase = phaseErrored
	v.resetRun()
	return nil
}

func (v *sequenceValidator) acceptInRun(ev aguievents.Event) error {
	switch e := ev.(type) {
	case *aguievents.TextMessageStartEvent:
		return open(v.messages, e.MessageID, "text message")
	case *aguievents.TextMessageContentEvent:
		return requireOpen(v.messages, e.MessageID, "text message")
	case *aguievents.TextMessageEndEvent:
		return closeItem(v.messages, e.MessageID, "text message")
	case *aguievents.ToolCallStartEvent:
		return v.toolCallStart(e)
	case *aguievents.ToolCallArgsEvent:
		return requireOpen(v.calls, e.ToolCallID, "tool call")
	case *aguievents.ToolCallEndEvent:
		return closeItem(v.calls, e.ToolCallID, "tool call")
	case *aguievents.ToolCallResultEvent:
		return v.toolCallResult(e)
	case *aguievents.StepStartedEvent:
		return open(v.steps, e.StepName, "step")
	case *aguievents.StepFinishedEvent:
		return closeItem(v.steps, e.StepName, "step")
	case *aguievents.ReasoningStartEvent:
		return open(v.phases, e.MessageID, "reasoning phase")
	case *aguievents.ReasoningEndEvent:
		return closeItem(v.phases, e.MessageID, "reasoning phase")
	case *aguievents.ReasoningMessageStartEvent:
		return open(v.thoughts, e.MessageID, "reasoning message")
	case *aguievents.ReasoningMessageContentEvent:
		return requireOpen(v.thoughts, e.MessageID, "reasoning message")
	case *aguievents.ReasoningMessageEndEvent:
		return closeItem(v.thoughts, e.MessageID, "reasoning message")
	case *aguievents.TextMessageChunkEvent, *aguievents.ToolCallChunkEvent,
		*aguievents.ReasoningMessageChunkEvent:
		return fmt.Errorf("chunk events are not expanded by this validator; the adapter emits the explicit triads")
	default:
		// Standalone events (STATE_*, MESSAGES_SNAPSHOT, CUSTOM, RAW, ...)
		// open and close nothing and may appear anywhere within a run.
		return nil
	}
}

func (v *sequenceValidator) toolCallStart(e *aguievents.ToolCallStartEvent) error {
	// A start for a call that has its result is a new call that reuses the id:
	// some providers number call ids per response (call_0, call_1, ...). Only a
	// start for a call still waiting for its result is a reopening.
	if v.answered[e.ToolCallID] {
		delete(v.answered, e.ToolCallID)
		delete(v.names, e.ToolCallID)
	}
	if name, seen := v.names[e.ToolCallID]; seen && name != e.ToolCallName {
		return fmt.Errorf("TOOL_CALL_START reopens %q as %q, but it was opened as %q", e.ToolCallID, e.ToolCallName, name)
	}
	v.names[e.ToolCallID] = e.ToolCallName
	return open(v.calls, e.ToolCallID, "tool call")
}

// toolCallResult checks a result against the calls the stream carried. The
// call may belong to an earlier run on the stream, as when a run continues
// after an approval hold and reports the approved tool's result, but it must
// have been started and left unanswered. Tests of a continuing run validate it
// together with the runs before it (continueAndCollect).
func (v *sequenceValidator) toolCallResult(e *aguievents.ToolCallResultEvent) error {
	if _, seen := v.names[e.ToolCallID]; !seen {
		return fmt.Errorf("TOOL_CALL_RESULT for call %q that no run on the stream started", e.ToolCallID)
	}
	if v.calls[e.ToolCallID] {
		return fmt.Errorf("TOOL_CALL_RESULT for call %q before its TOOL_CALL_END", e.ToolCallID)
	}
	if v.answered[e.ToolCallID] {
		return fmt.Errorf("TOOL_CALL_RESULT for call %q, which was already answered", e.ToolCallID)
	}
	if v.messages[e.MessageID] {
		return fmt.Errorf("TOOL_CALL_RESULT messageId %q collides with an open text message", e.MessageID)
	}
	v.answered[e.ToolCallID] = true
	return nil
}

func open(set map[string]bool, id, kind string) error {
	if set[id] {
		return fmt.Errorf("%s %q opened while already open", kind, id)
	}
	set[id] = true
	return nil
}

func requireOpen(set map[string]bool, id, kind string) error {
	if !set[id] {
		return fmt.Errorf("%s %q is not open", kind, id)
	}
	return nil
}

func closeItem(set map[string]bool, id, kind string) error {
	if err := requireOpen(set, id, kind); err != nil {
		return err
	}
	delete(set, id)
	return nil
}

// requireValidSequence fails the test when evts violates the AG-UI lifecycle.
func requireValidSequence(t *testing.T, evts []aguievents.Event) {
	t.Helper()
	require.NoError(t, validateAGUISequence(evts), "AG-UI event sequence:\n%s", describeEvents(evts))
}

func describeEvents(evts []aguievents.Event) string {
	var s string
	for i, ev := range evts {
		s += fmt.Sprintf("  %2d %s\n", i, ev.Type())
	}
	return s
}

// --- The guard's own tests: each rule must reject the sequence it forbids. ---

func TestValidateAGUISequence_AcceptsConformingStreams(t *testing.T) {
	start, finish := aguievents.NewRunStartedEvent("t", "r"), aguievents.NewRunFinishedEvent("t", "r")
	cases := map[string]struct {
		evts []aguievents.Event
		ends runPhase
	}{
		"text and tool": {[]aguievents.Event{
			start,
			aguievents.NewStepStartedEvent("s"),
			aguievents.NewTextMessageStartEvent("m1"),
			aguievents.NewTextMessageContentEvent("m1", "hi"),
			aguievents.NewTextMessageEndEvent("m1"),
			aguievents.NewToolCallStartEvent("c1", "lookup"),
			aguievents.NewToolCallArgsEvent("c1", "{}"),
			aguievents.NewToolCallEndEvent("c1"),
			aguievents.NewToolCallResultEvent("m2", "c1", "found"),
			aguievents.NewStepFinishedEvent("s"),
			finish,
		}, phaseFinished},
		"error first":         {[]aguievents.Event{aguievents.NewRunErrorEvent("unreachable")}, phaseErrored},
		"error mid-run":       {[]aguievents.Event{start, aguievents.NewTextMessageStartEvent("m"), aguievents.NewRunErrorEvent("x")}, phaseErrored},
		"late error":          {[]aguievents.Event{start, finish, aguievents.NewRunErrorEvent("x")}, phaseErrored},
		"two runs":            {[]aguievents.Event{start, finish, aguievents.NewRunStartedEvent("t", "r2"), aguievents.NewRunFinishedEvent("t", "r2")}, phaseFinished},
		"unanswered frontend": {[]aguievents.Event{start, aguievents.NewToolCallStartEvent("c", "f"), aguievents.NewToolCallEndEvent("c"), finish}, phaseFinished},
		"answered id reused by a new call": {[]aguievents.Event{
			start,
			aguievents.NewToolCallStartEvent("call_0", "lookup"), aguievents.NewToolCallEndEvent("call_0"),
			aguievents.NewToolCallResultEvent("m1", "call_0", "one"),
			aguievents.NewToolCallStartEvent("call_0", "search"), aguievents.NewToolCallEndEvent("call_0"),
			aguievents.NewToolCallResultEvent("m2", "call_0", "two"),
			finish,
		}, phaseFinished},
		"result in a later run": {[]aguievents.Event{
			start, aguievents.NewToolCallStartEvent("c0", "f"), aguievents.NewToolCallEndEvent("c0"), finish,
			aguievents.NewRunStartedEvent("t", "r2"), aguievents.NewToolCallResultEvent("m", "c0", "ok"),
			aguievents.NewRunFinishedEvent("t", "r2"),
		}, phaseFinished},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ends, err := replay(tc.evts)
			require.NoError(t, err)
			assert.Equal(t, tc.ends, ends)
		})
	}
}

func TestValidateAGUISequence_RejectsViolations(t *testing.T) {
	start := aguievents.NewRunStartedEvent("t", "r")
	finish := aguievents.NewRunFinishedEvent("t", "r")
	cases := map[string][]aguievents.Event{
		"empty":                    {},
		"no RUN_STARTED first":     {aguievents.NewTextMessageStartEvent("m")},
		"never finished":           {start, aguievents.NewTextMessageStartEvent("m"), aguievents.NewTextMessageEndEvent("m")},
		"nested RUN_STARTED":       {start, aguievents.NewRunStartedEvent("t", "r2")},
		"event after RUN_FINISHED": {start, finish, aguievents.NewStepStartedEvent("late")},
		"event after RUN_ERROR":    {start, aguievents.NewRunErrorEvent("x"), aguievents.NewRunFinishedEvent("t", "r")},
		"runId mismatch":           {start, aguievents.NewRunFinishedEvent("t", "other")},
		"message left open":        {start, aguievents.NewTextMessageStartEvent("m"), finish},
		"message reopened open":    {start, aguievents.NewTextMessageStartEvent("m"), aguievents.NewTextMessageStartEvent("m")},
		"content for closed":       {start, aguievents.NewTextMessageContentEvent("m", "x"), finish},
		"tool call left open":      {start, aguievents.NewToolCallStartEvent("c", "f"), finish},
		"args for unopened call":   {start, aguievents.NewToolCallArgsEvent("c", "{}"), finish},
		"result before end":        {start, aguievents.NewToolCallStartEvent("c", "f"), aguievents.NewToolCallResultEvent("m", "c", "x")},
		"result for unknown call":  {start, aguievents.NewToolCallResultEvent("m", "c", "x"), finish},
		"answered call answered again in a later run": {
			start, aguievents.NewToolCallStartEvent("c", "f"), aguievents.NewToolCallEndEvent("c"),
			aguievents.NewToolCallResultEvent("m1", "c", "x"), finish,
			aguievents.NewRunStartedEvent("t", "r2"), aguievents.NewToolCallResultEvent("m2", "c", "x"),
			aguievents.NewRunFinishedEvent("t", "r2"),
		},
		"duplicate result":          {start, aguievents.NewToolCallStartEvent("c", "f"), aguievents.NewToolCallEndEvent("c"), aguievents.NewToolCallResultEvent("m1", "c", "x"), aguievents.NewToolCallResultEvent("m2", "c", "x"), finish},
		"reopen with another name":  {start, aguievents.NewToolCallStartEvent("c", "f"), aguievents.NewToolCallEndEvent("c"), aguievents.NewToolCallStartEvent("c", "g"), aguievents.NewToolCallEndEvent("c"), finish},
		"step finished not started": {start, aguievents.NewStepFinishedEvent("s"), finish},
		"step left open":            {start, aguievents.NewStepStartedEvent("s"), finish},
		"step opened twice":         {start, aguievents.NewStepStartedEvent("s"), aguievents.NewStepStartedEvent("s")},
		"invalid event fields":      {start, aguievents.NewTextMessageStartEvent(""), finish},
		"empty result content": {
			start, aguievents.NewToolCallStartEvent("c", "f"), aguievents.NewToolCallEndEvent("c"),
			aguievents.NewToolCallResultEvent("m", "c", ""), finish,
		},
	}
	for name, evts := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, validateAGUISequence(evts))
		})
	}
}

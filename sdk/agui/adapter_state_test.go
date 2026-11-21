package agui

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// orderState is application state a tool changes while the turn runs.
type orderState struct {
	mu    sync.Mutex
	state map[string]any
	err   error
}

func (s *orderState) Snapshot(Sender) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	data, _ := json.Marshal(s.state)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out, nil
}

func (s *orderState) set(key string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = v
}

// describeState renders the run's state events, in order, among its tool
// results and the starts of its text messages.
func describeState(t *testing.T, evts []aguievents.Event) []string {
	t.Helper()
	var out []string
	for _, ev := range evts {
		switch e := ev.(type) {
		case *aguievents.StateSnapshotEvent:
			data, err := json.Marshal(e.Snapshot)
			require.NoError(t, err)
			out = append(out, "STATE_SNAPSHOT "+string(data))
		case *aguievents.StateDeltaEvent:
			data, err := json.Marshal(e.Delta)
			require.NoError(t, err)
			out = append(out, "STATE_DELTA "+string(data))
		case *aguievents.ToolCallResultEvent:
			out = append(out, "TOOL_CALL_RESULT "+e.ToolCallID)
		case *aguievents.TextMessageStartEvent:
			out = append(out, "TEXT_MESSAGE_START")
		}
	}
	return out
}

// A tool that changes the state while the turn runs is reported as a
// STATE_DELTA patching the run's snapshot, right after the tool's result. A
// state that does not change sends nothing more.
func TestRunSend_StateChangeIsSentAsADelta(t *testing.T) {
	state := &orderState{state: map[string]any{"status": "new", "items": []any{"tea"}}}
	conv := openConv(t, streamingProvider(
		streamedCall("lookup", map[string]any{"id": "42"}, "Checking."),
		streamedText("Shipped."),
	))
	conv.OnTool("lookup", func(map[string]any) (any, error) {
		state.set("status", "shipped")
		state.set("tracking", "TRK-1")
		return "shipped", nil
	})

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithStateProvider(state)), "ship 42")
	require.NoError(t, err)

	assert.Equal(t, []string{
		`STATE_SNAPSHOT {"items":["tea"],"status":"new"}`,
		"TEXT_MESSAGE_START",
		"TOOL_CALL_RESULT call_0_lookup",
		`STATE_DELTA [{"op":"replace","path":"/status","value":"shipped"},{"op":"add","path":"/tracking","value":"TRK-1"}]`,
		"TEXT_MESSAGE_START",
	}, describeState(t, evts))
}

// historyState is state derived from the conversation: the number of
// messages it holds.
type historyState struct {
	conv interface {
		Messages(context.Context) []types.Message
	}
}

func (h historyState) Snapshot(Sender) (any, error) {
	return map[string]any{"messages": len(h.conv.Messages(context.Background()))}, nil
}

// The state the turn changed is sent before RUN_FINISHED, even when no tool
// result came after the change.
func TestRunSend_StateChangedByTheTurnIsSentBeforeTheRunFinishes(t *testing.T) {
	conv := openConv(t, streamingProvider(streamedText("Hi.")))

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithStateProvider(historyState{conv})), "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		`STATE_SNAPSHOT {"messages":0}`,
		"TEXT_MESSAGE_START",
		`STATE_DELTA [{"op":"replace","path":"/messages","value":2}]`,
	}, describeState(t, evts))
	assert.Equal(t, aguievents.EventTypeStateDelta, evts[len(evts)-2].Type(), "the delta comes right before RUN_FINISHED")
}

// A snapshot that fails at the run's start is retried: the first one that
// succeeds is sent whole, as a STATE_SNAPSHOT, since the client has no
// baseline to patch.
func TestRunSend_StateWithoutABaselineIsSentAsASnapshot(t *testing.T) {
	state := &orderState{state: map[string]any{"status": "new"}, err: errors.New("store down")}
	conv := openConv(t, streamingProvider(
		streamedCall("lookup", map[string]any{"id": "42"}),
		streamedText("Shipped."),
	))
	conv.OnTool("lookup", func(map[string]any) (any, error) {
		state.mu.Lock()
		state.err = nil
		state.state["status"] = "shipped"
		state.mu.Unlock()
		return "shipped", nil
	})

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithStateProvider(state)), "ship 42")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"TOOL_CALL_RESULT call_0_lookup",
		`STATE_SNAPSHOT {"status":"shipped"}`,
		"TEXT_MESSAGE_START",
	}, describeState(t, evts))
}

// stateSequence answers each snapshot with the next state, repeating the last.
type stateSequence struct {
	mu     sync.Mutex
	states []any
}

func (s *stateSequence) Snapshot(Sender) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.states[0]
	if len(s.states) > 1 {
		s.states = s.states[1:]
	}
	return v, nil
}

// A change a patch cannot carry (a value becoming null, which the AG-UI Go
// SDK cannot encode in an operation, or the state's root changing type) is
// sent as a new STATE_SNAPSHOT. A nil state is never sent.
func TestRunSend_StateAPatchCannotCarryIsSentWhole(t *testing.T) {
	cases := map[string]struct {
		states []any
		want   []string
	}{
		"value becomes null": {
			[]any{map[string]any{"owner": "ann"}, map[string]any{"owner": nil}},
			[]string{`STATE_SNAPSHOT {"owner":"ann"}`, "TEXT_MESSAGE_START", `STATE_SNAPSHOT {"owner":null}`},
		},
		"root changes type": {
			[]any{map[string]any{"a": 1}, []any{"a"}},
			[]string{`STATE_SNAPSHOT {"a":1}`, "TEXT_MESSAGE_START", `STATE_SNAPSHOT ["a"]`},
		},
		"nil state": {
			[]any{nil, map[string]any{"a": 1}},
			[]string{"TEXT_MESSAGE_START", `STATE_SNAPSHOT {"a":1}`},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			conv := openConv(t, streamingProvider(streamedText("Hi.")))
			a := NewEventAdapter(conv, WithStateProvider(&stateSequence{states: tc.states}))

			evts, err := sendAndCollect(t, a, "hi")
			require.NoError(t, err)

			assert.Equal(t, tc.want, describeState(t, evts))
		})
	}
}

func TestStatePatch(t *testing.T) {
	op := func(o, path string, v any) aguievents.JSONPatchOperation {
		return aguievents.JSONPatchOperation{Op: o, Path: path, Value: v}
	}
	cases := map[string]struct {
		from, to string
		want     []aguievents.JSONPatchOperation
		ok       bool
	}{
		"unchanged":       {`{"a":{"b":[1,2]}}`, `{"a":{"b":[1,2]}}`, nil, true},
		"nested replace":  {`{"a":{"b":1,"c":2}}`, `{"a":{"b":1,"c":3}}`, []aguievents.JSONPatchOperation{op("replace", "/a/c", json.Number("3"))}, true},
		"add and remove":  {`{"a":1,"b":2}`, `{"b":2,"c":true}`, []aguievents.JSONPatchOperation{op("remove", "/a", nil), op("add", "/c", true)}, true},
		"type change":     {`{"a":{"x":1}}`, `{"a":"x"}`, []aguievents.JSONPatchOperation{op("replace", "/a", "x")}, true},
		"array grows":     {`{"a":[1]}`, `{"a":[1,2,3]}`, []aguievents.JSONPatchOperation{op("add", "/a/1", json.Number("2")), op("add", "/a/2", json.Number("3"))}, true},
		"array shrinks":   {`{"a":[1,2,3]}`, `{"a":[1]}`, []aguievents.JSONPatchOperation{op("remove", "/a/2", nil), op("remove", "/a/1", nil)}, true},
		"array element":   {`{"a":[{"x":1},{"x":2}]}`, `{"a":[{"x":1},{"x":5}]}`, []aguievents.JSONPatchOperation{op("replace", "/a/1/x", json.Number("5"))}, true},
		"escaped keys":    {`{"a/b":1,"t~x":1}`, `{"a/b":2,"t~x":2}`, []aguievents.JSONPatchOperation{op("replace", "/a~1b", json.Number("2")), op("replace", "/t~0x", json.Number("2"))}, true},
		"null to value":   {`{"a":null}`, `{"a":1}`, []aguievents.JSONPatchOperation{op("replace", "/a", json.Number("1"))}, true},
		"value to null":   {`{"a":1}`, `{"a":null}`, nil, false},
		"null added":      {`{}`, `{"a":null}`, nil, false},
		"root scalar":     {`1`, `2`, nil, false},
		"root type":       {`{}`, `[]`, nil, false},
		"array of arrays": {`[[1]]`, `[[1,2]]`, []aguievents.JSONPatchOperation{op("add", "/0/1", json.Number("2"))}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			from, err := normalizeState(json.RawMessage(tc.from))
			require.NoError(t, err)
			to, err := normalizeState(json.RawMessage(tc.to))
			require.NoError(t, err)
			got, ok := statePatch(from, to)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

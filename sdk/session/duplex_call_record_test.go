package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The session records each call the model makes, its name and arguments,
// under its ID. An ID a later response reuses (Gemini numbers calls call_0,
// call_1, ... per response) names the later call.
func TestDuplexSession_ToolCallIsTheLatestCallUnderItsID(t *testing.T) {
	s := &duplexSession{}
	first := types.MessageToolCall{ID: "call_0", Name: "lookup_order", Args: []byte(`{"order_id":"ORD-1"}`)}
	s.rememberCalls([]types.MessageToolCall{first})
	got, ok := s.ToolCall("call_0")
	require.True(t, ok)
	assert.Equal(t, first, got)

	assert.Equal(t, "lookup_order", s.takeCallName("call_0"))
	_, ok = s.ToolCall("call_0")
	assert.False(t, ok, "an answered call is forgotten")

	later := types.MessageToolCall{ID: "call_0", Name: "get_location", Args: []byte(`{}`)}
	s.rememberCalls([]types.MessageToolCall{first})
	s.rememberCalls([]types.MessageToolCall{later})
	got, ok = s.ToolCall("call_0")
	require.True(t, ok)
	assert.Equal(t, later, got)
}

// replyingStage answers each input with a complete assistant reply.
type replyingStage struct{}

func (replyingStage) Name() string          { return "replying" }
func (replyingStage) Type() stage.StageType { return stage.StageTypeTransform }

func (replyingStage) Process(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
	defer close(out)
	for range in {
		reply := stage.NewMessageElement(&types.Message{Role: "assistant", Content: "done"})
		reply.EndOfStream = true
		select {
		case out <- reply:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Each complete reply ends a turn, and the session counts them.
func TestDuplexSession_CountsCompletedTurns(t *testing.T) {
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(replyingStage{}).Build()
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        mock.NewProvider("mock", "mock-model", false),
		PipelineBuilder: builder,
	})
	require.NoError(t, err)
	defer sess.Close()
	resp := sess.Response()
	ds := sess.(*duplexSession)
	assert.Zero(t, ds.TurnsCompleted())

	for i := 1; i <= 2; i++ {
		require.NoError(t, sess.SendChunk(context.Background(), &providers.StreamChunk{Content: "hi"}))
		select {
		case <-resp:
		case <-time.After(lifecycleWait):
			t.Fatal("no reply")
		}
		require.Eventually(t, func() bool { return ds.TurnsCompleted() == int64(i) }, lifecycleWait, time.Millisecond)
	}
}

// callScriptStage answers its first input with a tool call the pipeline
// answers itself, and its second with a call to another tool under the same
// ID, which it holds for the caller.
type callScriptStage struct{ inputs int }

func (*callScriptStage) Name() string          { return "call-script" }
func (*callScriptStage) Type() stage.StageType { return stage.StageTypeTransform }

func (s *callScriptStage) Process(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
	defer close(out)
	for range in {
		s.inputs++
		var elems []stage.StreamElement
		if s.inputs == 1 {
			run := stage.NewMessageElement(&types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
				{ID: "call_0", Name: "lookup_order", Args: []byte(`{"order_id":"ORD-1"}`)},
			}})
			run.EndOfStream = true
			reply := stage.NewMessageElement(&types.Message{Role: "assistant", Content: "shipped"})
			reply.EndOfStream = true
			elems = []stage.StreamElement{run, reply}
		} else {
			held := stage.NewMessageElement(&types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
				{ID: "call_0", Name: "get_location", Args: []byte(`{"accuracy":"high"}`)},
			}})
			held.EndOfStream = true
			var pending stage.StreamElement
			pending.Meta.PendingTools = []tools.PendingToolExecution{{
				CallID: "call_0", ToolName: "get_location", Args: map[string]any{"accuracy": "high"},
			}}
			elems = []stage.StreamElement{held, pending}
		}
		for _, e := range elems {
			select {
			case out <- e:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

// Every call the model makes is recorded, with its arguments as sent; a later
// call under the same ID, which the pipeline holds, replaces it.
func TestDuplexSession_RecordsEveryCallTheLatestUnderAnID(t *testing.T) {
	st := &callScriptStage{}
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(st).Build()
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:          mock.NewProvider("mock", "mock-model", false),
		PipelineBuilder:   builder,
		ToolRegistry:      tools.NewRegistry(),
		pipelineRunsTools: true,
	})
	require.NoError(t, err)
	defer sess.Close()
	resp := sess.Response()
	ds := sess.(*duplexSession)

	require.NoError(t, sess.SendText(context.Background(), "where is my order?"))
	require.Eventually(t, func() bool { return ds.TurnsCompleted() == 1 }, lifecycleWait, time.Millisecond)
	first, kept := ds.ToolCall("call_0")
	require.True(t, kept, "a call is recorded when the model makes it")
	assert.Equal(t, types.MessageToolCall{ID: "call_0", Name: "lookup_order", Args: []byte(`{"order_id":"ORD-1"}`)}, first)

	require.NoError(t, sess.SendText(context.Background(), "and where am I?"))
	for chunk := range resp {
		if len(chunk.PendingTools) > 0 {
			break
		}
	}
	got, kept := ds.ToolCall("call_0")
	require.True(t, kept)
	assert.Equal(t, types.MessageToolCall{ID: "call_0", Name: "get_location", Args: []byte(`{"accuracy":"high"}`)}, got)
}

// The record keeps at most maxRecordedCalls calls, dropping the oldest.
func TestDuplexSession_CallRecordIsBounded(t *testing.T) {
	s := &duplexSession{}
	for i := 0; i <= maxRecordedCalls; i++ {
		s.rememberCalls([]types.MessageToolCall{{ID: fmt.Sprintf("c%d", i), Name: "t"}})
	}
	_, kept := s.ToolCall("c0")
	assert.False(t, kept, "the oldest is dropped")
	_, kept = s.ToolCall(fmt.Sprintf("c%d", maxRecordedCalls))
	assert.True(t, kept)
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	assert.Len(t, s.calls, maxRecordedCalls)
	assert.Len(t, s.callOrder, maxRecordedCalls)
}

// heldTwiceStage holds a call_0 on each input, to another tool each time:
// get_weather first, get_location second, the first never answered.
type heldTwiceStage struct{ inputs int }

func (*heldTwiceStage) Name() string          { return "held-twice" }
func (*heldTwiceStage) Type() stage.StageType { return stage.StageTypeTransform }

func (s *heldTwiceStage) Process(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
	defer close(out)
	for range in {
		s.inputs++
		name, city := "get_weather", "Paris"
		if s.inputs > 1 {
			name, city = "get_location", "Lyon"
		}
		call := stage.NewMessageElement(&types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "call_0", Name: name, Args: []byte(`{"city":"` + city + `"}`)},
		}})
		call.EndOfStream = true
		var held stage.StreamElement
		held.Meta.PendingTools = []tools.PendingToolExecution{{
			CallID: "call_0", ToolName: name, Args: map[string]any{"city": city},
		}}
		for _, e := range []stage.StreamElement{call, held} {
			select {
			case out <- e:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

// A response that reuses the ID of a call still held names the new call: the
// record, and the name a submitted result is given, are the latest call's.
func TestDuplexSession_ReusedIDWhileTheFirstIsHeld(t *testing.T) {
	st := &heldTwiceStage{}
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(st).Build()
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:          mock.NewProvider("mock", "mock-model", false),
		PipelineBuilder:   builder,
		ToolRegistry:      tools.NewRegistry(),
		pipelineRunsTools: true,
	})
	require.NoError(t, err)
	defer sess.Close()
	resp := sess.Response()
	ds := sess.(*duplexSession)
	awaitHeld := func() {
		t.Helper()
		select {
		case chunk := <-resp:
			require.NotEmpty(t, chunk.PendingTools)
		case <-time.After(lifecycleWait):
			t.Fatal("no held call")
		}
	}

	require.NoError(t, sess.SendText(context.Background(), "weather?"))
	awaitHeld()
	first, kept := ds.ToolCall("call_0")
	require.True(t, kept)
	assert.Equal(t, "get_weather", first.Name)

	require.NoError(t, sess.SendText(context.Background(), "where am I?"))
	awaitHeld()
	got, kept := ds.ToolCall("call_0")
	require.True(t, kept)
	assert.Equal(t, types.MessageToolCall{ID: "call_0", Name: "get_location", Args: []byte(`{"city":"Lyon"}`)}, got)
	assert.Equal(t, "get_location", ds.takeCallName("call_0"))
}

// stoppedHoldStage answers its input with an element that reports the tool
// turn stopped and still holds a call for the caller.
type stoppedHoldStage struct{}

func (stoppedHoldStage) Name() string          { return "stopped-hold" }
func (stoppedHoldStage) Type() stage.StageType { return stage.StageTypeTransform }

func (stoppedHoldStage) Process(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
	defer close(out)
	for range in {
		var elem stage.StreamElement
		elem.Error = errors.New("max rounds exceeded")
		elem.Meta.PendingTools = []tools.PendingToolExecution{{CallID: "call_7", ToolName: "approve_refund"}}
		select {
		case out <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// A call held on an element that also reports an error is recorded too.
func TestDuplexSession_RecordsACallHeldAlongsideAnError(t *testing.T) {
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(stoppedHoldStage{}).Build()
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:          mock.NewProvider("mock", "mock-model", false),
		PipelineBuilder:   builder,
		ToolRegistry:      tools.NewRegistry(),
		pipelineRunsTools: true,
	})
	require.NoError(t, err)
	defer sess.Close()
	resp := sess.Response()
	ds := sess.(*duplexSession)

	require.NoError(t, sess.SendText(context.Background(), "refund it"))
	select {
	case <-resp:
	case <-time.After(lifecycleWait):
		t.Fatal("no output")
	}
	got, kept := ds.ToolCall("call_7")
	require.True(t, kept)
	assert.Equal(t, "approve_refund", got.Name)
}

// A held call is recorded with its arguments exactly as the model sent them.
func TestDuplexSession_HeldCallKeepsTheRawArguments(t *testing.T) {
	s := &duplexSession{}
	raw := json.RawMessage(`{"z": 1, "a": "two"}`)
	s.holdCalls([]tools.PendingToolExecution{{
		CallID: "c1", ToolName: "lookup", Args: map[string]any{"a": "two", "z": 1}, RawArgs: raw,
	}})
	got, kept := s.ToolCall("c1")
	require.True(t, kept)
	assert.Equal(t, types.MessageToolCall{ID: "c1", Name: "lookup", Args: raw}, got)
}

// unheldCallStage answers its input with a tool call it does not hold, and
// records the tool results it is sent back.
type unheldCallStage struct{ toolCallingStage }

func (s *unheldCallStage) Process(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
	defer close(out)
	called := false
	for elem := range in {
		for i := range elem.Meta.ToolResultMessages {
			if tr := elem.Meta.ToolResultMessages[i].ToolResult; tr != nil {
				s.mu.Lock()
				s.results = append(s.results, *tr)
				s.mu.Unlock()
			}
		}
		if called {
			continue
		}
		called = true
		call := stage.NewMessageElement(&types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "call-1", Name: "get_weather", Args: []byte(`{"city": "Paris"}`)},
		}})
		call.EndOfStream = true
		select {
		case out <- call:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// A call answered with SubmitToolResults without ever being held is named in
// the history by the call the model made.
func TestDuplexSession_UnheldCallAnsweredByIDIsNamed(t *testing.T) {
	st := &unheldCallStage{}
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(st).Build()
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:          mock.NewProvider("mock", "mock-model", false),
		PipelineBuilder:   builder,
		ToolRegistry:      tools.NewRegistry(),
		pipelineRunsTools: true,
	})
	require.NoError(t, err)
	defer sess.Close()
	ds := sess.(*duplexSession)

	require.NoError(t, sess.SendText(context.Background(), "weather?"))
	require.Eventually(t, func() bool { _, ok := ds.ToolCall("call-1"); return ok }, lifecycleWait, time.Millisecond)
	got, _ := ds.ToolCall("call-1")
	assert.Equal(t, `{"city": "Paris"}`, string(got.Args), "the arguments as the model sent them")

	require.NoError(t, sess.SubmitToolResults(context.Background(), []providers.ToolResponse{
		{ToolCallID: "call-1", Result: `{"temp":21}`},
	}))
	require.Eventually(t, func() bool { return len(st.recorded()) == 1 }, lifecycleWait, 5*time.Millisecond)
	assert.Equal(t, "get_weather", st.recorded()[0].Name)
}

// A held call outlasts any number of calls the pipeline makes and answers,
// and stays answerable with its name; the calls not held are bounded.
func TestDuplexSession_HeldCallOutlastsManyOtherCalls(t *testing.T) {
	s := &duplexSession{}
	held := &stage.StreamElement{Message: &types.Message{
		Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "held", Name: "approve_refund", Args: []byte(`{}`)}},
	}}
	held.Meta.PendingTools = []tools.PendingToolExecution{{CallID: "held", ToolName: "approve_refund"}}
	s.recordElementCalls(held)
	for i := 0; i < 3*maxRecordedCalls; i++ {
		id := fmt.Sprintf("run-%d", i)
		s.recordElementCalls(&stage.StreamElement{Message: &types.Message{
			Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: id, Name: "lookup", Args: []byte(`{}`)}},
		}})
		if i%2 == 0 { // half are answered by the pipeline
			s.recordElementCalls(&stage.StreamElement{Message: &types.Message{
				Role: "tool", ToolResult: &types.MessageToolResult{ID: id, Name: "lookup"},
			}})
		}
	}
	got, kept := s.ToolCall("held")
	require.True(t, kept)
	assert.Equal(t, "approve_refund", got.Name)
	last := 3*maxRecordedCalls - 1
	_, kept = s.ToolCall(fmt.Sprintf("run-%d", last-1))
	assert.False(t, kept, "an answered call is dropped")
	_, kept = s.ToolCall(fmt.Sprintf("run-%d", last))
	assert.True(t, kept, "a call not answered is kept")
	s.callsMu.Lock()
	assert.Equal(t, maxRecordedCalls+1, len(s.calls), "the calls not held are bounded; the held one is kept")
	assert.LessOrEqual(t, len(s.callOrder), 2*maxRecordedCalls+1)
	s.callsMu.Unlock()
	assert.Equal(t, "approve_refund", s.takeCallName("held"))
}

// An element that makes a call and holds it records the held call: the
// held record wins.
func TestDuplexSession_HeldRecordWinsOnTheSameElement(t *testing.T) {
	s := &duplexSession{}
	elem := &stage.StreamElement{Message: &types.Message{
		Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "approve_refund", Args: []byte(`{"a":1}`)}},
	}}
	elem.Meta.PendingTools = []tools.PendingToolExecution{{CallID: "c1", ToolName: "approve_refund", RawArgs: []byte(`{"a": 1}`)}}
	s.recordElementCalls(elem)
	got, kept := s.ToolCall("c1")
	require.True(t, kept)
	assert.Equal(t, `{"a": 1}`, string(got.Args))
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	assert.True(t, s.calls["c1"].held)
	assert.Zero(t, s.unheld)
}

// The bound drops the oldest calls not held, never a held one, however many
// calls the model makes after it.
func TestDuplexSession_BoundNeverDropsAHeldCall(t *testing.T) {
	s := &duplexSession{}
	held := &stage.StreamElement{Message: &types.Message{
		Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "held", Name: "approve_refund", Args: []byte(`{}`)}},
	}}
	held.Meta.PendingTools = []tools.PendingToolExecution{{CallID: "held", ToolName: "approve_refund"}}
	s.recordElementCalls(held)
	for i := 0; i <= maxRecordedCalls; i++ {
		s.rememberCalls([]types.MessageToolCall{{ID: fmt.Sprintf("run-%d", i), Name: "lookup"}})
	}
	_, kept := s.ToolCall("held")
	assert.True(t, kept)
	_, kept = s.ToolCall("run-0")
	assert.False(t, kept, "the oldest call not held is dropped")
}

// A call recorded again is fresh: the bound drops older calls before it.
func TestDuplexSession_RerecordedCallIsFresh(t *testing.T) {
	s := &duplexSession{}
	s.rememberCalls([]types.MessageToolCall{{ID: "a", Name: "lookup"}})
	for i := 0; i < maxRecordedCalls-1; i++ {
		s.rememberCalls([]types.MessageToolCall{{ID: fmt.Sprintf("r-%d", i), Name: "lookup"}})
	}
	s.rememberCalls([]types.MessageToolCall{{ID: "a", Name: "get_location"}})
	s.rememberCalls([]types.MessageToolCall{{ID: "last", Name: "lookup"}})

	got, kept := s.ToolCall("a")
	require.True(t, kept, "the fresh call is not dropped for its old position")
	assert.Equal(t, "get_location", got.Name)
	_, kept = s.ToolCall("r-0")
	assert.False(t, kept, "the oldest call is dropped")
}

// Recording a held call again from an element that does not hold it keeps
// it held: it is never dropped by the bound.
func TestDuplexSession_HeldCallStaysHeldWhenRecordedAgain(t *testing.T) {
	s := &duplexSession{}
	s.holdCalls([]tools.PendingToolExecution{{CallID: "held", ToolName: "approve_refund", RawArgs: []byte(`{}`)}})
	s.rememberCalls([]types.MessageToolCall{{ID: "held", Name: "approve_refund", Args: []byte(`{}`)}})
	for i := 0; i <= maxRecordedCalls; i++ {
		s.rememberCalls([]types.MessageToolCall{{ID: fmt.Sprintf("r-%d", i), Name: "lookup"}})
	}
	_, kept := s.ToolCall("held")
	assert.True(t, kept)
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	assert.True(t, s.calls["held"].held)
	assert.Equal(t, maxRecordedCalls, s.unheld)
}

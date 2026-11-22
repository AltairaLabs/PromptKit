package sdk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	filestore "github.com/AltairaLabs/PromptKit/runtime/v2/statestore/file"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	streamPkg "github.com/AltairaLabs/PromptKit/sdk/v2/stream"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a/v2"
)

// callEndStores returns each state store a streamed turn's messages can be
// persisted in, fresh.
func callEndStores(t *testing.T) map[string]func() statestore.Store {
	return map[string]func() statestore.Store{
		"memory": func() statestore.Store { return statestore.NewMemoryStore() },
		"redis": func() statestore.Store {
			mr := miniredis.RunT(t)
			return statestore.NewRedisStore(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
		},
		"file": func() statestore.Store {
			s, err := filestore.NewStore(filestore.Options{Root: t.TempDir()})
			require.NoError(t, err)
			return s
		},
	}
}

// failingCallProvider is the mock tool provider whose failAt'th streamed call
// (0-based) fails: it streams the call's text deltas, keepDeltas of them,
// then an error chunk in place of the reply's end.
type failingCallProvider struct {
	*mock.ToolProvider
	failAt     int32
	keepDeltas int
	calls      atomic.Int32
}

var errCallEndStreamBroke = errors.New("stream broke")

func (p *failingCallProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools interface{}, choice string,
) (<-chan providers.StreamChunk, error) {
	in, err := p.ToolProvider.PredictStreamWithTools(ctx, req, tools, choice)
	if err != nil || p.calls.Add(1)-1 != p.failAt {
		return in, err
	}
	out := make(chan providers.StreamChunk, providers.DefaultStreamBufferSize)
	go func() {
		defer close(out)
		sent := 0
		for chunk := range in {
			if chunk.FinishReason != nil || sent == p.keepDeltas {
				continue
			}
			out <- chunk
			sent++
		}
		out <- providers.StreamChunk{Error: errCallEndStreamBroke}
	}()
	return out, nil
}

// callEndConv opens the rounds pack on provider, persisting in store under
// the id "call-end".
func callEndConv(t *testing.T, provider providers.Provider, store statestore.Store) *Conversation {
	t.Helper()
	conv, err := Open(writeWorkflowTestPack(t, roundsPackJSON), "chat",
		WithProvider(provider), WithSkipSchemaValidation(),
		WithStateStore(store), WithConversationID("call-end"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup", func(args map[string]any) (any, error) { return args["id"], nil })
	return conv
}

func mockRounds(turns ...mock.Turn) *mock.ToolProvider {
	return mock.NewToolProviderWithRepository("mock", "mock-model", false, &sequenceRepo{turns: turns})
}

// callEndLog summarizes streams one line per chunk, naming each message id
// m0, m1, ... in the order it first appears, so a test can state which
// chunks name the same message and compare them with the stored ids.
type callEndLog struct {
	labels map[string]string
}

func newCallEndLog() *callEndLog { return &callEndLog{labels: map[string]string{}} }

func (l *callEndLog) label(id string) string {
	if id == "" {
		return "-"
	}
	if lbl, ok := l.labels[id]; ok {
		return lbl
	}
	lbl := fmt.Sprintf("m%d", len(l.labels))
	l.labels[id] = lbl
	return lbl
}

func callIDs(calls []types.MessageToolCall) string {
	ids := make([]string, 0, len(calls))
	for _, c := range calls {
		ids = append(ids, c.ID)
	}
	return "[" + strings.Join(ids, " ") + "]"
}

func (l *callEndLog) drain(ch <-chan StreamChunk) []string {
	var got []string
	for chunk := range ch {
		if chunk.Error != nil {
			got = append(got, "error "+chunk.Error.Error())
			continue
		}
		switch chunk.Type {
		case ChunkText:
			got = append(got, fmt.Sprintf("text %s %s", l.label(chunk.MessageID), chunk.Text))
		case ChunkProviderCallEnd:
			got = append(got, fmt.Sprintf("end %d %s %s %s",
				chunk.Round, l.label(chunk.MessageID), chunk.FinishReason, callIDs(chunk.ToolCalls)))
		case ChunkToolCall:
			got = append(got, fmt.Sprintf("call %d %s %s", chunk.Round, l.label(chunk.MessageID), chunk.ToolCall.ID))
		case ChunkToolResult:
			got = append(got, fmt.Sprintf("result %d %s %s",
				chunk.Round, chunk.ToolResult.ID, chunk.ToolResult.GetTextContent()))
		case ChunkClientTool:
			got = append(got, "client "+chunk.ClientTool.CallID)
		case ChunkDone:
			got = append(got, "done")
		default:
			got = append(got, chunk.Type.String())
		}
	}
	return got
}

// stored names the assistant messages store holds for the conversation, in
// order, by their labels in the stream.
func (l *callEndLog) stored(t *testing.T, store statestore.Store) []string {
	t.Helper()
	state, err := store.Load(context.Background(), "call-end")
	require.NoError(t, err)
	var out []string
	for i := range state.Messages {
		if state.Messages[i].Role == roleAssistant {
			out = append(out, l.label(state.Messages[i].ID()))
		}
	}
	return out
}

// A server tool between two model calls: each call ends with its boundary
// after its text and before its calls run, the last call included, and each
// boundary names the message the call is stored under.
func TestStream_ProviderCallEnd_ServerToolBetweenTwoCalls(t *testing.T) {
	for name, newStore := range callEndStores(t) {
		t.Run(name, func(t *testing.T) {
			store := newStore()
			conv := callEndConv(t, mockRounds(
				mock.Turn{Type: "tool_calls", StreamChunks: []string{"Let me ", "look."},
					ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "1"}}}},
				mock.Turn{Type: "text", StreamChunks: []string{"Both ", "shipped."}},
			), store)
			log := newCallEndLog()

			got := log.drain(conv.Stream(context.Background(), "check 1"))

			assert.Equal(t, []string{
				"text m0 Let me ", "text m0 look.",
				"end 0 m0 tool_use [call_0_lookup]",
				"call 0 m0 call_0_lookup", "result 0 call_0_lookup 1",
				"text m1 Both ", "text m1 shipped.",
				"end 1 m1 stop []",
				"done",
			}, got)
			assert.Equal(t, []string{"m0", "m1"}, log.stored(t, store))
		})
	}
}

// A later call with fewer tool calls than an earlier one still reports its
// calls, once each, and its boundary carries only its own.
func TestStream_ProviderCallEnd_LaterCallWithFewerToolCalls(t *testing.T) {
	store := statestore.NewMemoryStore()
	conv := callEndConv(t, mockRounds(
		mock.Turn{Type: "tool_calls", ToolCalls: []mock.ToolCall{
			{Name: "lookup", Arguments: map[string]any{"id": "1"}},
			{Name: "lookup", Arguments: map[string]any{"id": "2"}},
		}},
		mock.Turn{Type: "tool_calls", ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "3"}}}},
		mock.Turn{Type: "text", StreamChunks: []string{"Done."}},
	), store)
	log := newCallEndLog()

	got := log.drain(conv.Stream(context.Background(), "check 1, 2 and 3"))

	assert.Equal(t, []string{
		"end 0 m0 tool_use [call_0_lookup call_1_lookup]",
		"call 0 m0 call_0_lookup", "call 0 m0 call_1_lookup",
		"result 0 call_0_lookup 1", "result 0 call_1_lookup 2",
		"end 1 m1 tool_use [call_0_lookup]",
		"call 1 m1 call_0_lookup", "result 1 call_0_lookup 3",
		"text m2 Done.",
		"end 2 m2 stop []",
		"done",
	}, got)
	assert.Equal(t, []string{"m0", "m1", "m2"}, log.stored(t, store))
}

// A call that suspends on a client tool ends before its ChunkClientTool, and
// ResumeStream counts its calls on from where the turn left off.
func TestStream_ProviderCallEnd_ClientToolThenResumeStream(t *testing.T) {
	for name, newStore := range callEndStores(t) {
		t.Run(name, func(t *testing.T) {
			store := newStore()
			conv := callEndConv(t, mockRounds(
				mock.Turn{Type: "tool_calls", StreamChunks: []string{"Looking."},
					ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "1"}}}},
				mock.Turn{Type: "tool_calls", ToolCalls: []mock.ToolCall{
					{Name: "send_message", Arguments: map[string]any{"body": "hi"}}}},
				mock.Turn{Type: "text", StreamChunks: []string{"Sent."}},
			), store)
			conv.DeferClientTool("send_message")
			ctx := context.Background()
			log := newCallEndLog()

			got := log.drain(conv.Stream(ctx, "look up 1, then send hi"))
			assert.Equal(t, []string{
				"text m0 Looking.",
				"end 0 m0 tool_use [call_0_lookup]",
				"call 0 m0 call_0_lookup", "result 0 call_0_lookup 1",
				"end 1 m1 tool_use [call_0_send_message]",
				"call 1 m1 call_0_send_message",
				"client call_0_send_message",
				"done",
			}, got)

			require.NoError(t, conv.SendToolResult(ctx, "call_0_send_message", "sent"))
			got = log.drain(conv.ResumeStream(ctx))
			assert.Equal(t, []string{
				"text m2 Sent.",
				"end 2 m2 stop []",
				"done",
			}, got)
			assert.Equal(t, []string{"m0", "m1", "m2"}, log.stored(t, store))
		})
	}
}

// A provider call that fails mid-stream still ends with its boundary, after
// its partial text and before the error: interrupted when the partial reply
// is kept, error when nothing of it is.
func TestStream_ProviderCallEnd_FailedCall(t *testing.T) {
	turns := func() []mock.Turn {
		return []mock.Turn{
			{Type: "tool_calls", StreamChunks: []string{"Let me ", "look."},
				ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "1"}}}},
			{Type: "text", StreamChunks: []string{"Partial ", "answer."}},
		}
	}
	t.Run("partial reply kept", func(t *testing.T) {
		for name, newStore := range callEndStores(t) {
			t.Run(name, func(t *testing.T) {
				store := newStore()
				conv := callEndConv(t, &failingCallProvider{
					ToolProvider: mockRounds(turns()...), failAt: 1, keepDeltas: 1,
				}, store)
				log := newCallEndLog()

				got := log.drain(conv.Stream(context.Background(), "check 1"))

				require.Len(t, got, 8)
				assert.Equal(t, []string{
					"text m0 Let me ", "text m0 look.",
					"end 0 m0 tool_use [call_0_lookup]",
					"call 0 m0 call_0_lookup", "result 0 call_0_lookup 1",
					"text m1 Partial ",
					"end 1 m1 interrupted []",
				}, got[:7])
				assert.Contains(t, got[7], "error ")
				assert.Contains(t, got[7], errCallEndStreamBroke.Error())
				assert.Equal(t, []string{"m0", "m1"}, log.stored(t, store))
			})
		}
	})
	t.Run("nothing kept", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		conv := callEndConv(t, &failingCallProvider{
			ToolProvider: mockRounds(turns()...), failAt: 1, keepDeltas: 0,
		}, store)
		log := newCallEndLog()

		got := log.drain(conv.Stream(context.Background(), "check 1"))

		require.Len(t, got, 7)
		assert.Equal(t, []string{
			"text m0 Let me ", "text m0 look.",
			"end 0 m0 tool_use [call_0_lookup]",
			"call 0 m0 call_0_lookup", "result 0 call_0_lookup 1",
			"end 1 m1 error []",
		}, got[:6])
		assert.Contains(t, got[6], errCallEndStreamBroke.Error())
	})
}

// A text-only turn makes one provider call: its boundary follows its text.
// StreamRaw and StreamWithCallback do not report the boundary.
func TestStream_ProviderCallEnd_TextOnlySingleCall(t *testing.T) {
	newConv := func() (*Conversation, statestore.Store) {
		store := statestore.NewMemoryStore()
		return callEndConv(t, mockRounds(
			mock.Turn{Type: "text", StreamChunks: []string{"Hello ", "there."}},
		), store), store
	}

	conv, store := newConv()
	log := newCallEndLog()
	got := log.drain(conv.Stream(context.Background(), "hi"))
	assert.Equal(t, []string{"text m0 Hello ", "text m0 there.", "end 0 m0 stop []", "done"}, got)
	assert.Equal(t, []string{"m0"}, log.stored(t, store))

	raw, _ := newConv()
	ch, err := raw.StreamRaw(context.Background(), "hi")
	require.NoError(t, err)
	var rawGot []string
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		switch {
		case chunk.Done:
			rawGot = append(rawGot, "done")
		case chunk.Type == streamPkg.ChunkText:
			rawGot = append(rawGot, "text "+chunk.Text)
		default:
			rawGot = append(rawGot, fmt.Sprintf("type %d", chunk.Type))
		}
	}
	assert.Equal(t, []string{"text Hello ", "text there.", "done"}, rawGot)

	cb, _ := newConv()
	var events []string
	cb.OnStreamEvent(func(e StreamEvent) { events = append(events, fmt.Sprintf("%T", e)) })
	_, err = cb.StreamWithCallback(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, []string{"sdk.TextDeltaEvent", "sdk.TextDeltaEvent", "sdk.StreamDoneEvent"}, events)

	served, _ := newConv()
	adapter := &streamConvAdapter{convAdapter{c: served}}
	var kinds []a2aserver.EventKind
	for evt := range adapter.Stream(context.Background(), "hi") {
		require.NoError(t, evt.Error)
		kinds = append(kinds, evt.Kind)
	}
	assert.Equal(t, []a2aserver.EventKind{a2aserver.EventText, a2aserver.EventText, a2aserver.EventDone}, kinds)
}

func reaskTurns() []mock.Turn {
	return []mock.Turn{
		{Type: "tool_calls", StreamChunks: []string{"Checking."},
			ToolCalls: []mock.ToolCall{{Name: "lookup", Arguments: map[string]any{"id": "1"}}}},
		{Type: "text", StreamChunks: []string{"It shipped."}},
		{Type: "text", StreamChunks: []string{`{"shipped":`, `true}`}},
	}
}

func reaskConv(t *testing.T, provider providers.Provider, store statestore.Store) *Conversation {
	t.Helper()
	conv, err := Open(writeWorkflowTestPack(t, roundsPackJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(), WithStateStore(store), WithConversationID("call-end"),
		WithResponseFormat(&providers.ResponseFormat{
			Type: providers.ResponseFormatJSONSchema, SchemaName: "status",
			JSONSchema: []byte(`{"type":"object","properties":{"shipped":{"type":"boolean"}}}`),
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup", func(args map[string]any) (any, error) { return args["id"], nil })
	return conv
}

// A re-ask that fails keeps the unconstrained answer, stored under the
// replaced reply's id: that answer streams under it, as stored, and nothing
// of the failed re-ask does, however much of it the model sent. The re-ask's
// end names an id of its own, under which nothing is stored.
func TestStream_ProviderCallEnd_FailedStructuredOutputReask(t *testing.T) {
	for _, keep := range []int{0, 1} {
		t.Run(fmt.Sprintf("after %d deltas", keep), func(t *testing.T) {
			store := statestore.NewMemoryStore()
			conv := reaskConv(t, &failingCallProvider{
				ToolProvider: mockRounds(reaskTurns()...), failAt: 2, keepDeltas: keep,
			}, store)
			log := newCallEndLog()

			var resp *Response
			got := log.drain(captureDone(conv.Stream(context.Background(), "did 1 ship?"), &resp))

			assert.Equal(t, []string{
				"end 0 m0 tool_use [call_0_lookup]",
				"call 0 m0 call_0_lookup", "result 0 call_0_lookup 1",
				"end 1 m1 stop []",
				"text m1 It shipped.",
				"end 2 m2 error []",
				"done",
			}, got)
			assert.Equal(t, []string{"m0", "m1"}, log.stored(t, store))
			require.NotNil(t, resp)
			assert.Equal(t, "It shipped.", resp.Text())
		})
	}
}

// captureDone passes ch on, keeping its ChunkDone response in resp.
func captureDone(ch <-chan StreamChunk, resp **Response) <-chan StreamChunk {
	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		for c := range ch {
			if c.Type == ChunkDone {
				*resp = c.Message
			}
			out <- c
		}
	}()
	return out
}

// A structured-output turn re-asks its closing answer under the schema: the
// re-ask is a provider call of its own, and its answer replaces the closing
// reply and is stored under that reply's id, so the re-ask's text and both
// calls' ends name it.
func TestStream_ProviderCallEnd_StructuredOutputReask(t *testing.T) {
	store := statestore.NewMemoryStore()
	conv, err := Open(writeWorkflowTestPack(t, roundsPackJSON), "chat",
		WithProvider(mockRounds(reaskTurns()...)),
		WithSkipSchemaValidation(), WithStateStore(store), WithConversationID("call-end"),
		WithResponseFormat(&providers.ResponseFormat{
			Type: providers.ResponseFormatJSONSchema, SchemaName: "status",
			JSONSchema: []byte(`{"type":"object","properties":{"shipped":{"type":"boolean"}}}`),
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup", func(args map[string]any) (any, error) { return args["id"], nil })
	log := newCallEndLog()

	got := log.drain(conv.Stream(context.Background(), "did 1 ship?"))

	assert.Equal(t, []string{
		"end 0 m0 tool_use [call_0_lookup]",
		"call 0 m0 call_0_lookup", "result 0 call_0_lookup 1",
		"end 1 m1 stop []",
		`text m1 {"shipped":`, "text m1 true}",
		"end 2 m1 stop []",
		"done",
	}, got)
	assert.Equal(t, []string{"m0", "m1"}, log.stored(t, store))
}

// stallingCallProvider is the mock tool provider whose streamed reply sends
// its first text delta, then waits for the caller to cancel and fails with
// the cancellation.
type stallingCallProvider struct {
	*mock.ToolProvider
}

func (p *stallingCallProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools interface{}, choice string,
) (<-chan providers.StreamChunk, error) {
	in, err := p.ToolProvider.PredictStreamWithTools(ctx, req, tools, choice)
	if err != nil {
		return nil, err
	}
	out := make(chan providers.StreamChunk, providers.DefaultStreamBufferSize)
	go func() {
		defer close(out)
		first := <-in
		go func() {
			for range in {
			}
		}()
		out <- first
		<-ctx.Done()
		out <- providers.StreamChunk{Error: ctx.Err()}
	}()
	return out, nil
}

// A call the caller cancels mid-stream still ends with its boundary, after
// its partial text and before the error, to a caller that goes on draining
// the stream.
func TestStream_ProviderCallEnd_CanceledCallStillEnds(t *testing.T) {
	// The boundary must not be lost to the race between the canceled
	// context and the channel send, so run the turn several times.
	for i := 0; i < 10; i++ {
		conv := callEndConv(t, &stallingCallProvider{ToolProvider: mockRounds(
			mock.Turn{Type: "text", StreamChunks: []string{"Partial ", "answer."}},
		)}, statestore.NewMemoryStore())
		ctx, cancel := context.WithCancel(context.Background())
		log := newCallEndLog()
		var got []string
		for chunk := range conv.Stream(ctx, "hi") {
			if chunk.Type == ChunkText && chunk.Error == nil {
				cancel()
			}
			got = append(got, log.drain(oneChunk(chunk))...)
		}
		cancel()
		require.Len(t, got, 3, "run %d: %v", i, got)
		assert.Equal(t, []string{"text m0 Partial ", "end 0 m0 interrupted []"}, got[:2], "run %d", i)
		assert.Contains(t, got[2], context.Canceled.Error(), "run %d", i)
	}
}

// oneChunk is a closed channel holding chunk.
func oneChunk(chunk StreamChunk) <-chan StreamChunk {
	ch := make(chan StreamChunk, 1)
	ch <- chunk
	close(ch)
	return ch
}

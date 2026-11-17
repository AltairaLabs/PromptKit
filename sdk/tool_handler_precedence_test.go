package sdk

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// precedenceMode opens a conversation over callerToolsPack in one mode and
// runs one turn that calls lookup, returning once the call has run (done).
type precedenceMode struct {
	name string
	open func(t *testing.T) *Conversation
	turn func(t *testing.T, conv *Conversation, done func() bool)
}

func precedenceModes() []precedenceMode {
	return []precedenceMode{
		{
			name: "unary",
			open: func(t *testing.T) *Conversation {
				conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
					WithProvider(newCallerToolProvider()), WithSkipSchemaValidation())
				require.NoError(t, err)
				return conv
			},
			turn: func(t *testing.T, conv *Conversation, _ func() bool) {
				_, err := conv.Send(context.Background(), "ada")
				require.NoError(t, err)
			},
		},
		{
			name: "duplex-asm",
			open: func(t *testing.T) *Conversation {
				prov := &callerStreamProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false)}
				conv, err := OpenDuplex(createTestPackFile(t, callerToolsPack), "chat", WithProvider(prov),
					WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio))
				require.NoError(t, err)
				return conv
			},
			turn: func(t *testing.T, conv *Conversation, done func() bool) {
				convDrain(t, conv)
				require.NoError(t, conv.SendText(context.Background(), "ada"))
				require.Eventually(t, done, 5*time.Second, 5*time.Millisecond)
			},
		},
		{
			name: "duplex-vad",
			open: func(t *testing.T) *Conversation {
				if testing.Short() {
					t.Skip("drives a real VAD turn")
				}
				conv, err := OpenDuplex(createTestPackFile(t, callerToolsPack), "chat",
					WithProvider(newCallerToolProvider()), WithSkipSchemaValidation(),
					WithVADMode(newScriptedSTT("ada"), newConvMockTTSService(), vadTestConfig()))
				require.NoError(t, err)
				return conv
			},
			turn: func(t *testing.T, conv *Conversation, done func() bool) {
				convDrain(t, conv)
				speakOneUtterance(t, conv)
				require.Eventually(t, done, 8*time.Second, 5*time.Millisecond)
			},
		},
	}
}

// precedenceExecutor is a host executor that counts the calls it runs.
type precedenceExecutor struct{ runs *atomic.Int32 }

func (e precedenceExecutor) Name() string { return "precedence-exec" }

func (e precedenceExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	e.runs.Add(1)
	return json.RawMessage(`"earlier"`), nil
}

// A call an OnToolAsync check lets through runs the handler an earlier
// OnToolExecutor, OnToolCtx or OnToolCall registered for the tool, never the
// async execution handler, in every mode; after a plain OnTool, which the
// async registration replaces, it runs the async execution handler.
func TestOnToolAsync_UnheldCallRunsTheSameHandlerInEveryMode(t *testing.T) {
	registrations := map[string]struct {
		register    func(conv *Conversation, earlier *atomic.Int32)
		earlierWins bool
	}{
		"OnToolExecutor": {func(conv *Conversation, earlier *atomic.Int32) {
			conv.OnToolExecutor("lookup", precedenceExecutor{runs: earlier})
		}, true},
		"OnToolCtx": {func(conv *Conversation, earlier *atomic.Int32) {
			conv.OnToolCtx("lookup", func(context.Context, map[string]any) (any, error) {
				earlier.Add(1)
				return "earlier", nil
			})
		}, true},
		"OnToolCall": {func(conv *Conversation, earlier *atomic.Int32) {
			conv.OnToolCall("lookup", func(context.Context, ToolCall) (any, error) {
				earlier.Add(1)
				return "earlier", nil
			})
		}, true},
		"OnTool": {func(conv *Conversation, earlier *atomic.Int32) {
			conv.OnTool("lookup", func(map[string]any) (any, error) {
				earlier.Add(1)
				return "earlier", nil
			})
		}, false},
	}
	for _, mode := range precedenceModes() {
		for regName, reg := range registrations {
			t.Run(mode.name+"/"+regName, func(t *testing.T) {
				conv := mode.open(t)
				t.Cleanup(func() { _ = conv.Close() })
				var earlier, async atomic.Int32
				reg.register(conv, &earlier)
				conv.OnToolAsync("lookup",
					func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{} },
					func(map[string]any) (any, error) {
						async.Add(1)
						return "async", nil
					})

				mode.turn(t, conv, func() bool { return earlier.Load()+async.Load() > 0 })

				if reg.earlierWins {
					assert.EqualValues(t, 1, earlier.Load(), "the earlier handler runs")
					assert.Zero(t, async.Load(), "the async execution handler does not")
				} else {
					assert.EqualValues(t, 1, async.Load(), "the async execution handler replaced OnTool")
					assert.Zero(t, earlier.Load())
				}
			})
		}
	}
}

// A call an OnToolAsyncCall check lets through runs its exec, in every mode,
// even over a handler registered for the tool before it.
func TestOnToolAsyncCall_UnheldCallRunsExecInEveryMode(t *testing.T) {
	for _, mode := range precedenceModes() {
		t.Run(mode.name, func(t *testing.T) {
			conv := mode.open(t)
			t.Cleanup(func() { _ = conv.Close() })
			var earlier, async atomic.Int32
			conv.OnToolCtx("lookup", func(context.Context, map[string]any) (any, error) {
				earlier.Add(1)
				return "earlier", nil
			})
			conv.OnToolAsyncCall("lookup",
				func(context.Context, ToolCall) sdktools.PendingResult { return sdktools.PendingResult{} },
				func(context.Context, ToolCall) (any, error) {
					async.Add(1)
					return "async", nil
				})

			mode.turn(t, conv, func() bool { return earlier.Load()+async.Load() > 0 })

			assert.EqualValues(t, 1, async.Load(), "exec runs")
			assert.Zero(t, earlier.Load())
		})
	}
}

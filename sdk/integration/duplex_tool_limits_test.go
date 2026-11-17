package integration

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// loopingToolSession is a realtime session whose model calls the same tool
// with the same arguments after every tool result, until a result tells it
// its calls were stopped; it then says "gave up". Any other text gets the
// reply "reply: <text>".
type loopingToolSession struct {
	*mock.MockStreamSession
	mu    sync.Mutex
	calls int
}

func (s *loopingToolSession) SendText(_ context.Context, text string) error {
	if text == "loop" {
		go s.callTool()
		return nil
	}
	go s.say("reply: " + text)
	return nil
}

func (s *loopingToolSession) callTool() {
	s.mu.Lock()
	s.calls++
	id := "call_" + string(rune('0'+s.calls))
	s.mu.Unlock()
	fr := "tool_calls"
	s.EmitChunk(&providers.StreamChunk{
		ToolCalls: []types.MessageToolCall{{
			ID: id, Name: "process_refund", Args: json.RawMessage(`{"amount":5,"order_id":"ORD-1"}`),
		}},
		FinishReason: &fr,
	})
}

func (s *loopingToolSession) say(text string) {
	fr := "stop"
	s.EmitChunk(&providers.StreamChunk{Content: text, Delta: text, FinishReason: &fr})
}

func (s *loopingToolSession) SendToolResponse(ctx context.Context, id, result string) error {
	return s.SendToolResponses(ctx, []providers.ToolResponse{{ToolCallID: id, Result: result}})
}

func (s *loopingToolSession) SendToolResponses(_ context.Context, responses []providers.ToolResponse) error {
	for _, r := range responses {
		if strings.Contains(r.Result, "tool calls were stopped") {
			go s.say("gave up")
			return nil
		}
	}
	go s.callTool()
	return nil
}

// loopingToolProvider creates loopingToolSessions.
type loopingToolProvider struct {
	*mock.StreamingProvider
}

func (p *loopingToolProvider) CreateStreamSession(
	context.Context, *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return &loopingToolSession{MockStreamSession: mock.NewMockStreamSession()}, nil
}

// nextChunk returns the next chunk on ch, failing the test after timeout.
func nextChunk(t *testing.T, ch <-chan providers.StreamChunk) (providers.StreamChunk, bool) {
	t.Helper()
	select {
	case c, ok := <-ch:
		return c, ok
	case <-time.After(5 * time.Second):
		t.Fatal("no chunk from the conversation within 5s")
		return providers.StreamChunk{}, false
	}
}

// awaitContent reads ch until a chunk carries content, and returns the error
// chunks read on the way.
func awaitContent(t *testing.T, ch <-chan providers.StreamChunk, content string) []error {
	t.Helper()
	var errs []error
	for {
		c, ok := nextChunk(t, ch)
		require.True(t, ok, "the response channel closed before %q arrived (errors so far: %v)", content, errs)
		if c.Error != nil {
			errs = append(errs, c.Error)
		}
		if c.Content == content {
			return errs
		}
	}
}

// The identical-call breaker stops a realtime tool turn that loops with no
// host policy at all. That is reported to the caller as an error chunk, and
// the conversation goes on: Response() stays open and the next input is
// answered.
func TestDuplexToolLimits_AStoppedToolTurnLeavesTheConversationOpen(t *testing.T) {
	provider := &loopingToolProvider{StreamingProvider: mock.NewStreamingProvider("loop", "loop-model", false)}
	conv, err := sdk.OpenDuplex(writePackFile(t, duplexHITLPackJSON), "chat",
		sdk.WithProvider(provider), sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("process_refund", func(map[string]any) (any, error) { return "refunded", nil })
	responseCh, err := conv.Response()
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, conv.SendText(ctx, "loop"))
	errs := awaitContent(t, responseCh, "gave up")
	require.Len(t, errs, 1, "the stopped turn is reported once")
	assert.Equal(t,
		`provider stage: tool loop persisted after a self-correction nudge: "process_refund" called with identical arguments`,
		errs[0].Error())

	require.NoError(t, conv.SendText(ctx, "hello"))
	assert.Empty(t, awaitContent(t, responseCh, "reply: hello"), "the next turn is answered without error")
}

// A terminal limit ends the conversation: a provider call that takes the
// session past MaxCostUSD is reported as an error chunk, and Response() then
// closes.
func TestDuplexToolLimits_ASpentBudgetEndsTheResponse(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_1", 50)}, "ok",
		sdk.WithToolPolicy(&rtpipeline.ToolPolicy{MaxCostUSD: 0.01}))
	provider.toolCallCost = &types.CostInfo{InputTokens: 10, OutputTokens: 5, TotalCost: 0.02}
	conv.OnTool("process_refund", func(map[string]any) (any, error) { return "refunded", nil })
	responseCh, err := conv.Response()
	require.NoError(t, err)

	require.NoError(t, conv.SendText(context.Background(), "refund it"))
	var errs []error
	for {
		c, ok := nextChunk(t, responseCh)
		if !ok {
			break
		}
		if c.Error != nil {
			errs = append(errs, c.Error)
		}
	}
	require.Len(t, errs, 1)
	assert.Equal(t, "provider stage: cost budget exceeded (0.0200 > 0.0100 USD)", errs[0].Error())
}

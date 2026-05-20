package mock

import (
	"context"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Compile-time interface check — MockStreamSession is the optional
// tool-response surface advertised by ToolResponseSupport, which the
// duplex provider stage uses to feed tool execution results back into
// the streaming session. Without this, every duplex scenario with a
// scripted tool_call hangs: the runtime emits "session does not support
// tool responses" and waits forever for a follow-up the mock never
// produces.
var _ providers.ToolResponseSupport = (*MockStreamSession)(nil)

// SendToolResponse records a single tool result on the mock session and
// triggers the next scripted turn so the conversation can advance.
//
// Real streaming providers (OpenAI Realtime, Gemini Live) handle tool
// responses by sending a `function_call_output` event followed by a
// `response.create` trigger that wakes the model up to produce its
// continuation. The mock equivalent is: store the response for test
// introspection, then call emitAutoResponse — which advances
// responseCount and emits whatever the repository has scripted as the
// next turn (typically the agent's text follow-up after the tool result).
func (m *MockStreamSession) SendToolResponse(ctx context.Context, toolCallID, result string) error {
	return m.SendToolResponses(ctx, []providers.ToolResponse{
		{ToolCallID: toolCallID, Result: result},
	})
}

// SendToolResponses records a batch of tool results and triggers a
// single continuation. Parallel tool calls in one agent turn produce
// one batch; the mock collapses them into a single emit just like the
// real providers do (one `response.create` after all function_call_output
// items have been queued).
func (m *MockStreamSession) SendToolResponses(_ context.Context, responses []providers.ToolResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closeCalled {
		return errors.New(errSessionClosed)
	}

	m.toolResponses = append(m.toolResponses, responses...)
	logger.Debug("MockStreamSession.SendToolResponses: received tool results",
		"count", len(responses), "total_received", len(m.toolResponses))

	// Tool results from a real provider unconditionally trigger
	// continuation — there's no separate "should I respond now?" flag,
	// the response.create event IS the trigger. Mirror that: emit the
	// next turn even when autoRespond is off, because if a test is
	// driving tool responses at all it's expecting the agent to react
	// to them. Tests that want to inspect tool_responses without
	// triggering can use the (unexported) field directly.
	m.emitAutoResponse()
	return nil
}

// ReceivedToolResponses returns a snapshot of every tool response the
// session has received via SendToolResponse/SendToolResponses. Tests
// use this to verify the runtime sent back the expected payload for a
// scripted tool_call.
func (m *MockStreamSession) ReceivedToolResponses() []providers.ToolResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]providers.ToolResponse, len(m.toolResponses))
	copy(out, m.toolResponses)
	return out
}

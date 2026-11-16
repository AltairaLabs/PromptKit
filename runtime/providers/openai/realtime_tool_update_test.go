package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// newClientEventServer is a fake Realtime WebSocket server that completes the
// handshake and forwards every client event, the initial session.update
// included, to received in order.
func newClientEventServer(t *testing.T, received chan<- map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if err := conn.WriteJSON(map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess_test", "model": "gpt-realtime"},
		}); err != nil {
			return
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				continue
			}
			received <- m
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// openProviderSession opens a realtime session through the provider (an Azure
// platform provider dialing srv), as the pipeline does, so the session it
// returns is the provider's own wrapper.
func openProviderSession(
	t *testing.T, srv *httptest.Server, req *providers.StreamingInputConfig,
) providers.StreamInputSession {
	t.Helper()
	p := NewProviderFromConfig(&ProviderConfig{
		ID: "azure-realtime", Model: "dep", Credential: &tokenCredential{}, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL + "/"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	session, err := p.CreateStreamSession(ctx, req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func lookupOrderTool() providers.StreamingToolDefinition {
	return providers.StreamingToolDefinition{
		Name:        "lookup_order",
		Description: "Look up an order",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"order_id": map[string]any{"type": "string"}},
			"required":   []any{"order_id"},
		},
	}
}

func lookupOrderWire() map[string]any {
	return map[string]any{
		"type":        "function",
		"name":        "lookup_order",
		"description": "Look up an order",
		"parameters": map[string]any{
			"type":       "object",
			"properties": map[string]any{"order_id": map[string]any{"type": "string"}},
			"required":   []any{"order_id"},
		},
	}
}

// UpdateTools replaces the live session's tools with a session.update that
// carries them, and the session's tool_choice.
func TestRealtimeUpdateTools_SendsSessionUpdateWithTheTools(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newClientEventServer(t, received)
	req := audioStreamRequest()
	req.ToolChoice = "required"
	session := openProviderSession(t, srv, req)
	recvEvent(t, received) // the initial session.update

	updater, ok := session.(providers.StreamToolUpdater)
	require.True(t, ok, "the OpenAI realtime session can change its tools")
	require.NoError(t, updater.UpdateTools(context.Background(),
		[]providers.StreamingToolDefinition{lookupOrderTool()}))

	ev := recvEvent(t, received)
	assert.Equal(t, "session.update", ev["type"])
	assert.Equal(t, map[string]any{
		"type":        "realtime",
		"tools":       []any{lookupOrderWire()},
		"tool_choice": "required",
	}, ev["session"])
}

// Updating to no tools sends an empty tools array, which clears them: an
// omitted array would leave the old tools in place. The server merges a
// partial session.update, so tool_choice is sent too, reset to "auto".
func TestRealtimeUpdateTools_EmptyClearsTheTools(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newClientEventServer(t, received)
	req := audioStreamRequest()
	req.Tools = []providers.StreamingToolDefinition{lookupOrderTool()}
	session := openProviderSession(t, srv, req)
	recvEvent(t, received)

	require.NoError(t, session.(providers.StreamToolUpdater).UpdateTools(context.Background(), nil))

	ev := recvEvent(t, received)
	assert.Equal(t, "session.update", ev["type"])
	assert.Equal(t, map[string]any{"type": "realtime", "tools": []any{}, "tool_choice": "auto"}, ev["session"])
}

// A closed session refuses the update.
func TestRealtimeUpdateTools_ClosedSessionErrors(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newClientEventServer(t, received)
	session := openProviderSession(t, srv, audioStreamRequest())
	require.NoError(t, session.Close())

	err := session.(providers.StreamToolUpdater).UpdateTools(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, "session is closed", err.Error())
}

// The session is created with the request's tool_choice: a mode as is, a
// tool name as a function choice.
func TestRealtimeCreate_SendsTheToolChoice(t *testing.T) {
	cases := map[string]struct {
		choice string
		want   any
	}{
		"none":      {"none", "none"},
		"required":  {"required", "required"},
		"auto":      {"auto", "auto"},
		"tool name": {"lookup_order", map[string]any{"type": "function", "name": "lookup_order"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			received := make(chan map[string]any, 16)
			srv := newClientEventServer(t, received)
			req := audioStreamRequest()
			req.Tools = []providers.StreamingToolDefinition{lookupOrderTool()}
			req.ToolChoice = tc.choice
			openProviderSession(t, srv, req)

			ev := recvEvent(t, received)
			require.Equal(t, "session.update", ev["type"])
			session := ev["session"].(map[string]any)
			assert.Equal(t, tc.want, session["tool_choice"])
			assert.Equal(t, []any{lookupOrderWire()}, session["tools"])
		})
	}
}

// Without a tool_choice the session leaves the server's default in place.
func TestRealtimeCreate_NoToolChoiceSendsNone(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newClientEventServer(t, received)
	openProviderSession(t, srv, audioStreamRequest())

	ev := recvEvent(t, received)
	session := ev["session"].(map[string]any)
	_, has := session["tool_choice"]
	assert.False(t, has, "tool_choice is omitted: %v", session["tool_choice"])
}

// An update that leaves no tools resets tool_choice to "auto": "required" (or
// a function) with no tools is an invalid session the server rejects, and an
// omitted tool_choice would keep the session's "required" (the server merges a
// partial session.update).
func TestRealtimeUpdateTools_NoToolsResetsToolChoiceToAuto(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newClientEventServer(t, received)
	req := audioStreamRequest()
	req.Tools = []providers.StreamingToolDefinition{lookupOrderTool()}
	req.ToolChoice = "required"
	session := openProviderSession(t, srv, req)
	recvEvent(t, received)

	require.NoError(t, session.(providers.StreamToolUpdater).UpdateTools(context.Background(), nil))

	ev := recvEvent(t, received)
	assert.Equal(t, map[string]any{"type": "realtime", "tools": []any{}, "tool_choice": "auto"}, ev["session"])
}

// A function tool_choice is sent only while the session has that tool; while
// it lacks the tool, the update resets the choice to "auto" rather than
// leaving the session forcing a tool it no longer has.
func TestRealtimeUpdateTools_FunctionChoiceOnlyWithItsTool(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newClientEventServer(t, received)
	req := audioStreamRequest()
	req.Tools = []providers.StreamingToolDefinition{lookupOrderTool()}
	req.ToolChoice = "lookup_order"
	session := openProviderSession(t, srv, req)
	recvEvent(t, received)
	updater := session.(providers.StreamToolUpdater)
	other := providers.StreamingToolDefinition{Name: "cancel_order"}

	require.NoError(t, updater.UpdateTools(context.Background(), []providers.StreamingToolDefinition{other}))
	require.NoError(t, updater.UpdateTools(context.Background(),
		[]providers.StreamingToolDefinition{other, lookupOrderTool()}))

	without := recvEvent(t, received)["session"].(map[string]any)
	assert.Equal(t, "auto", without["tool_choice"], "a choice naming a tool the session lacks resets to auto")
	with := recvEvent(t, received)["session"].(map[string]any)
	assert.Equal(t, map[string]any{"type": "function", "name": "lookup_order"}, with["tool_choice"])
}

// A session created without tools sends no tool_choice either.
func TestRealtimeCreate_NoToolsSendsNoToolChoice(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newClientEventServer(t, received)
	req := audioStreamRequest()
	req.ToolChoice = "required"
	openProviderSession(t, srv, req)

	session := recvEvent(t, received)["session"].(map[string]any)
	_, has := session["tool_choice"]
	assert.False(t, has, "tool_choice without tools: %v", session["tool_choice"])
}

// A canceled context sends nothing.
func TestRealtimeUpdateTools_CanceledContextSendsNothing(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newClientEventServer(t, received)
	session := openProviderSession(t, srv, audioStreamRequest())
	recvEvent(t, received)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := session.(providers.StreamToolUpdater).UpdateTools(ctx, nil)

	require.ErrorIs(t, err, context.Canceled)
	select {
	case ev := <-received:
		t.Fatalf("an event was sent: %v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	gosdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// progressServer has "steady", which works for 400ms and reports progress
// every 50ms, and "quiet", which works as long and reports none.
func progressServer() *gosdk.Server {
	s := gosdk.NewServer(&gosdk.Implementation{Name: "progress", Version: "1"}, nil)
	work := func(report bool) gosdk.ToolHandlerFor[struct{}, any] {
		return func(ctx context.Context, r *gosdk.CallToolRequest, _ struct{}) (*gosdk.CallToolResult, any, error) {
			token := r.Params.GetProgressToken()
			for i := 1; i <= 8; i++ {
				time.Sleep(50 * time.Millisecond)
				if report && token != nil {
					_ = r.Session.NotifyProgress(ctx, &gosdk.ProgressNotificationParams{
						ProgressToken: token, Progress: float64(i), Total: 8,
					})
				}
			}
			return &gosdk.CallToolResult{Content: []gosdk.Content{&gosdk.TextContent{Text: "done"}}}, nil, nil
		}
	}
	gosdk.AddTool(s, &gosdk.Tool{Name: "steady"}, work(true))
	gosdk.AddTool(s, &gosdk.Tool{Name: "quiet"}, work(false))
	return s
}

func TestClient_ProgressRestartsTheRequestTimeout(t *testing.T) {
	// Both tools outlast RequestTimeout. The one that reports progress is
	// given the time; the one that stays silent times out.
	srv := httptest.NewServer(gosdk.NewStreamableHTTPHandler(
		func(*http.Request) *gosdk.Server { return progressServer() }, nil))
	defer srv.Close()
	opts := testOptions()
	opts.RequestTimeout = 150 * time.Millisecond
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: srv.URL}, opts)
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	res, err := c.CallTool(context.Background(), "steady", json.RawMessage(`{}`))
	require.NoError(t, err, "progress restarts the timeout")
	assert.Equal(t, "done", firstText(t, res))

	_, err = c.CallTool(context.Background(), "quiet", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, ErrServerUnresponsive, "without progress the call still times out")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = c.CallTool(ctx, "steady", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, context.DeadlineExceeded, "the caller's deadline bounds the call whatever the progress")
}

func toolNames(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func TestRegistry_ToolListChangeIsReindexedAndReported(t *testing.T) {
	// One server instance behind every session, so a tool added to it
	// reaches the connected client as notifications/tools/list_changed.
	server := gosdk.NewServer(&gosdk.Implementation{Name: "changing", Version: "1"}, nil)
	gosdk.AddTool(server, &gosdk.Tool{Name: "first"},
		func(context.Context, *gosdk.CallToolRequest, struct{}) (*gosdk.CallToolResult, any, error) {
			return &gosdk.CallToolResult{}, nil, nil
		})
	srv := httptest.NewServer(gosdk.NewStreamableHTTPHandler(func(*http.Request) *gosdk.Server { return server }, nil))
	defer srv.Close()

	changed := make(chan []string, 4)
	reg := NewRegistryWithOptions(RegistryOptions{
		OnToolsChanged: func(name string, tools []Tool) {
			assert.Equal(t, "s", name)
			changed <- toolNames(tools)
		},
	})
	defer reg.Close()
	require.NoError(t, reg.RegisterServer(ServerConfig{Name: "s", URL: srv.URL, TransportName: TransportStreamableHTTP}))
	_, err := reg.GetClient(context.Background(), "s")
	require.NoError(t, err)

	gosdk.AddTool(server, &gosdk.Tool{Name: "second"},
		func(context.Context, *gosdk.CallToolRequest, struct{}) (*gosdk.CallToolResult, any, error) {
			return &gosdk.CallToolResult{}, nil, nil
		})
	select {
	case names := <-changed:
		assert.Equal(t, []string{"first", "second"}, names)
	case <-time.After(5 * time.Second):
		t.Fatal("no tool list change reported after the server added a tool")
	}
	reg.mu.RLock()
	assert.Equal(t, "s", reg.toolIndex["second"], "the new tool is indexed to its server")
	reg.mu.RUnlock()

	server.RemoveTools("first")
	select {
	case names := <-changed:
		assert.Equal(t, []string{"second"}, names)
	case <-time.After(5 * time.Second):
		t.Fatal("no tool list change reported after the server removed a tool")
	}
	reg.mu.RLock()
	_, stillIndexed := reg.toolIndex["first"]
	reg.mu.RUnlock()
	assert.False(t, stillIndexed, "a removed tool leaves the index")
}

func TestRegistry_ToolsChangedFromAReplacedClientIsDropped(t *testing.T) {
	var reported []string
	reg := NewRegistryWithOptions(RegistryOptions{
		OnToolsChanged: func(name string, _ []Tool) { reported = append(reported, name) },
	})
	current := newSDKClient(ServerConfig{Name: "s"}, DefaultClientOptions(), TransportStdio)
	stale := newSDKClient(ServerConfig{Name: "s"}, DefaultClientOptions(), TransportStdio)
	reg.clients["s"] = current
	reg.toolIndex["kept"] = "s"

	reg.toolsChanged("s", stale, []Tool{{Name: "late"}})
	assert.Empty(t, reported, "a list from a client the registry no longer holds is not reported")
	assert.Equal(t, map[string]string{"kept": "s"}, reg.toolIndex)

	reg.toolsChanged("s", current, []Tool{{Name: "fresh"}})
	assert.Equal(t, []string{"s"}, reported)
	assert.Equal(t, map[string]string{"fresh": "s"}, reg.toolIndex)
}

func TestClient_UnreadableToolListIsNotReported(t *testing.T) {
	// A re-read that fails keeps the previous tools: it is not degraded to
	// an empty list, which would withdraw every tool.
	opts := testOptions()
	opts.EnableGracefulDegradation = true
	c := newSDKClient(ServerConfig{Name: "s"}, opts, TransportStreamableHTTP)
	called := false
	c.onToolsChanged(func([]Tool) { called = true })
	c.refreshTools(context.Background())
	assert.False(t, called)
}

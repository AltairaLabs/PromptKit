package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// seamAgent serves an agent card with a text skill and an image skill, and
// answers message/send with a completed task echoing the query. A request
// for the "draw" skill also gets an image part back.
type seamAgent struct {
	mu       sync.Mutex
	requests []SendMessageRequest
}

func (a *seamAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if r.URL.Path != AgentCardPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(AgentCard{
			Name: "Research Agent",
			SupportedInterfaces: []AgentInterface{
				{URL: "http://" + r.Host + "/", ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "1.0"},
			},
			DefaultInputModes:  []string{"text/plain"},
			DefaultOutputModes: []string{"text/plain"},
			Skills: []AgentSkill{
				{ID: "search", Name: "Search", Description: "Search papers"},
				{ID: "draw", Name: "Draw", InputModes: []string{"text/plain", "image/png"},
					OutputModes: []string{"text/plain", "image/png"}},
			},
		})
		return
	}
	req := decodeRPC(r)
	var params SendMessageRequest
	_ = json.Unmarshal(req.Params, &params)
	a.mu.Lock()
	a.requests = append(a.requests, params)
	a.mu.Unlock()

	text := "echo: " + *params.Message.Parts[0].Text
	parts := []Part{{Text: &text}}
	if params.Message.Metadata["skillId"] == "draw" {
		url := "http://agent.example/out.png"
		parts = append(parts, Part{URL: &url, MediaType: "image/png"})
	}
	rpcResult(w, req.ID, &Task{ID: "t1", Status: TaskStatus{
		State: TaskStateCompleted, Message: &Message{Role: RoleAgent, Parts: parts},
	}})
}

// TestToolBridge_ToolsRunThroughTheToolRegistry registers the descriptors the
// bridge builds from a real agent card into a tools.Registry beside the A2A
// Executor, and calls them the way a conversation does. The bridge's input
// and output schemas are enforced by the registry, so a schema that disagreed
// with what the executor sends or returns would fail here, not in either
// side's own tests.
func TestToolBridge_ToolsRunThroughTheToolRegistry(t *testing.T) {
	agent := &seamAgent{}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	bridge := NewToolBridge(NewClient(srv.URL))
	descs, err := bridge.RegisterAgent(context.Background())
	require.NoError(t, err)
	exec := NewExecutor()
	defer exec.Close()
	bridge.ShareCards(exec)

	reg := tools.NewRegistry()
	reg.RegisterExecutor(exec)
	for _, d := range descs {
		require.NoError(t, reg.Register(d))
	}
	require.Equal(t, []string{"a2a__research_agent__draw", "a2a__research_agent__search"}, reg.List())

	t.Run("text skill", func(t *testing.T) {
		res, err := reg.Execute(context.Background(), "a2a__research_agent__search",
			json.RawMessage(`{"query":"transformers"}`))
		require.NoError(t, err)
		require.Empty(t, res.Error, "the executor's result must satisfy the bridge's output schema")
		assert.JSONEq(t, `{"response":"echo: transformers"}`, string(res.Result))
		require.Len(t, res.Parts, 1)
		assert.Equal(t, "echo: transformers", *res.Parts[0].Text)

		agent.mu.Lock()
		last := agent.requests[len(agent.requests)-1]
		agent.mu.Unlock()
		assert.Equal(t, "search", last.Message.Metadata["skillId"])
	})

	t.Run("image skill sends and returns media", func(t *testing.T) {
		res, err := reg.Execute(context.Background(), "a2a__research_agent__draw",
			json.RawMessage(`{"query":"a cat","image_url":"http://in.example/cat.png"}`))
		require.NoError(t, err)
		require.Empty(t, res.Error)
		assert.JSONEq(t, `{"response":"echo: a cat"}`, string(res.Result))
		require.Len(t, res.Parts, 2)
		require.NotNil(t, res.Parts[1].Media)
		require.NotNil(t, res.Parts[1].Media.URL)
		assert.Equal(t, "http://agent.example/out.png", *res.Parts[1].Media.URL)

		agent.mu.Lock()
		last := agent.requests[len(agent.requests)-1]
		agent.mu.Unlock()
		require.Len(t, last.Message.Parts, 2)
		require.NotNil(t, last.Message.Parts[1].URL)
		assert.Equal(t, "http://in.example/cat.png", *last.Message.Parts[1].URL)
	})

	t.Run("a call without the required query never reaches the agent", func(t *testing.T) {
		agent.mu.Lock()
		before := len(agent.requests)
		agent.mu.Unlock()

		_, err := reg.Execute(context.Background(), "a2a__research_agent__search", json.RawMessage(`{}`))
		var verr *tools.ValidationError
		require.True(t, errors.As(err, &verr), "want *tools.ValidationError, got %v", err)
		assert.Contains(t, verr.Detail, "query is required")

		agent.mu.Lock()
		defer agent.mu.Unlock()
		assert.Len(t, agent.requests, before)
	})
}

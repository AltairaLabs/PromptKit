package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// a2aTestServer creates an httptest.Server that serves an agent card and
// handles JSON-RPC message/send requests. It returns a completed task with
// the given response text.
func a2aTestServer(t *testing.T, agentName, skillID, responseText string) *httptest.Server {
	t.Helper()
	card := a2a.AgentCard{
		Name: agentName,
		Skills: []a2a.AgentSkill{
			{ID: skillID, Name: skillID, Description: "Test skill"},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent.json":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(card)

		case "/a2a":
			var rpcReq a2a.JSONRPCRequest
			json.NewDecoder(r.Body).Decode(&rpcReq)

			text := responseText
			task := a2a.Task{
				ID:        "task-1",
				ContextID: "ctx-1",
				Status: a2a.TaskStatus{
					State: a2a.TaskStateCompleted,
					Message: &a2a.Message{
						Role:  a2a.RoleAgent,
						Parts: []a2a.Part{{Text: &text}},
					},
				},
			}

			taskJSON, _ := json.Marshal(task)
			resp := a2a.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      rpcReq.ID,
				Result:  taskJSON,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		default:
			http.NotFound(w, r)
		}
	}))
}

// setupA2ABridge creates a ToolBridge with a registered agent from the test server.
func setupA2ABridge(t *testing.T, serverURL string) *a2a.ToolBridge {
	t.Helper()
	client := a2a.NewClient(serverURL)
	bridge := a2a.NewToolBridge(client)
	_, err := bridge.RegisterAgent(context.Background())
	require.NoError(t, err)
	return bridge
}

func TestA2AExecutor_Name(t *testing.T) {
	exec := a2a.NewExecutor()
	assert.Equal(t, "a2a", exec.Name())
}

func TestA2AExecutor_Execute(t *testing.T) {
	srv := a2aTestServer(t, "TestAgent", "greet", "Hello from remote agent!")
	defer srv.Close()

	exec := a2a.NewExecutor()
	desc := &tools.ToolDescriptor{
		Name: "a2a__testagent__greet",
		Mode: "a2a",
		A2AConfig: &tools.A2AConfig{
			AgentURL: srv.URL,
			SkillID:  "greet",
		},
	}

	args := json.RawMessage(`{"query":"Hi there"}`)
	result, err := exec.Execute(context.Background(), desc, args)
	require.NoError(t, err)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "Hello from remote agent!", parsed["response"])
}

func TestA2AExecutor_Execute_NoA2AConfig(t *testing.T) {
	exec := a2a.NewExecutor()
	desc := &tools.ToolDescriptor{Name: "bad_tool", Mode: "a2a"}

	_, err := exec.Execute(context.Background(), desc, json.RawMessage(`{"query":"test"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no A2AConfig")
}

func TestA2AExecutor_Execute_Timeout(t *testing.T) {
	srv := a2aTestServer(t, "TestAgent", "greet", "ok")
	defer srv.Close()

	exec := a2a.NewExecutor()
	desc := &tools.ToolDescriptor{
		Name: "a2a__testagent__greet",
		Mode: "a2a",
		A2AConfig: &tools.A2AConfig{
			AgentURL:  srv.URL,
			SkillID:   "greet",
			TimeoutMs: 5000, // generous timeout for test
		},
	}

	result, err := exec.Execute(context.Background(), desc, json.RawMessage(`{"query":"Hi"}`))
	require.NoError(t, err)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "ok", parsed["response"])
}

func TestWithA2ATools_Option(t *testing.T) {
	srv := a2aTestServer(t, "MyAgent", "do_stuff", "result")
	defer srv.Close()

	bridge := setupA2ABridge(t, srv.URL)

	cfg := &config{}
	opt := WithA2ATools(bridge)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Same(t, bridge, cfg.a2aBridge)
}

func TestWithA2ATools_NilBridge(t *testing.T) {
	cap := NewA2ACapability()
	// No bridge, no agents — RegisterTools should be a no-op
	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	allTools := registry.GetTools()
	assert.Empty(t, allTools)
}

func TestWithA2ATools_ToolsRegistered(t *testing.T) {
	srv := a2aTestServer(t, "MyAgent", "summarize", "summary")
	defer srv.Close()

	bridge := setupA2ABridge(t, srv.URL)
	cap := NewA2ACapability()
	cap.bridge = bridge

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	// The tool name is "a2a__myagent__summarize" (sanitized)
	tool, err := registry.GetTool("a2a__myagent__summarize")
	require.NoError(t, err)
	assert.Equal(t, "a2a__myagent__summarize", tool.Name)
	assert.Equal(t, "a2a", tool.Mode)
	assert.NotNil(t, tool.A2AConfig)
	assert.Equal(t, srv.URL, tool.A2AConfig.AgentURL)
	assert.Equal(t, "summarize", tool.A2AConfig.SkillID)
}

func TestWithA2ATools_ExecutorRegistered(t *testing.T) {
	srv := a2aTestServer(t, "MyAgent", "summarize", "summary")
	defer srv.Close()

	bridge := setupA2ABridge(t, srv.URL)
	cap := NewA2ACapability()
	cap.bridge = bridge

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	// The registry should be able to resolve the "a2a" executor for this tool.
	// We verify by calling Execute on the registry directly.
	result, err := registry.Execute(context.Background(), "a2a__myagent__summarize", json.RawMessage(`{"query":"test"}`))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Error)
	assert.NotNil(t, result.Result)
}

func TestWithA2ATools_AlongsidePackTools(t *testing.T) {
	srv := a2aTestServer(t, "RemoteAgent", "translate", "translated")
	defer srv.Close()

	bridge := setupA2ABridge(t, srv.URL)

	// Create a registry with a local tool already present
	registry := tools.NewRegistry()
	_ = registry.Register(&tools.ToolDescriptor{
		Name:        "local_tool",
		Description: "A local tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        "local",
	})

	// Register A2A tools via capability
	cap := NewA2ACapability()
	cap.bridge = bridge
	cap.RegisterTools(registry)

	// Both should be present
	_, err := registry.GetTool("local_tool")
	assert.NoError(t, err)

	_, err = registry.GetTool("a2a__remoteagent__translate")
	assert.NoError(t, err)
}

func TestExtractResponseText(t *testing.T) {
	t.Run("from status message", func(t *testing.T) {
		text := "hello"
		task := &a2a.Task{
			Status: a2a.TaskStatus{
				Message: &a2a.Message{
					Parts: []a2a.Part{{Text: &text}},
				},
			},
		}
		assert.Equal(t, "hello", a2a.ExtractResponseText(task))
	})

	t.Run("from artifacts", func(t *testing.T) {
		text := "artifact text"
		task := &a2a.Task{
			Artifacts: []a2a.Artifact{
				{Parts: []a2a.Part{{Text: &text}}},
			},
		}
		assert.Equal(t, "artifact text", a2a.ExtractResponseText(task))
	})

	t.Run("empty task", func(t *testing.T) {
		task := &a2a.Task{}
		assert.Equal(t, "", a2a.ExtractResponseText(task))
	})

	t.Run("status message preferred over artifacts", func(t *testing.T) {
		statusText := "from status"
		artifactText := "from artifact"
		task := &a2a.Task{
			Status: a2a.TaskStatus{
				Message: &a2a.Message{
					Parts: []a2a.Part{{Text: &statusText}},
				},
			},
			Artifacts: []a2a.Artifact{
				{Parts: []a2a.Part{{Text: &artifactText}}},
			},
		}
		assert.Equal(t, "from status", a2a.ExtractResponseText(task))
	})
}

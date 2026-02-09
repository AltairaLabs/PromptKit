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
		Name: "a2a_testagent_greet",
		Mode: "a2a",
		A2AConfig: &tools.A2AConfig{
			AgentURL: srv.URL,
			SkillID:  "greet",
		},
	}

	args := json.RawMessage(`{"query":"Hi there"}`)
	result, err := exec.Execute(desc, args)
	require.NoError(t, err)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "Hello from remote agent!", parsed["response"])
}

func TestA2AExecutor_Execute_NoA2AConfig(t *testing.T) {
	exec := a2a.NewExecutor()
	desc := &tools.ToolDescriptor{Name: "bad_tool", Mode: "a2a"}

	_, err := exec.Execute(desc, json.RawMessage(`{"query":"test"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no A2AConfig")
}

func TestA2AExecutor_Execute_Timeout(t *testing.T) {
	srv := a2aTestServer(t, "TestAgent", "greet", "ok")
	defer srv.Close()

	exec := a2a.NewExecutor()
	desc := &tools.ToolDescriptor{
		Name: "a2a_testagent_greet",
		Mode: "a2a",
		A2AConfig: &tools.A2AConfig{
			AgentURL:  srv.URL,
			SkillID:   "greet",
			TimeoutMs: 5000, // generous timeout for test
		},
	}

	result, err := exec.Execute(desc, json.RawMessage(`{"query":"Hi"}`))
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
	conv := newTestConversation()
	conv.config.a2aBridge = nil

	// registerA2ATools should be a no-op
	conv.registerA2ATools()

	// Verify no extra tools were registered
	allTools := conv.toolRegistry.GetTools()
	assert.Empty(t, allTools)
}

func TestWithA2ATools_ToolsRegistered(t *testing.T) {
	srv := a2aTestServer(t, "MyAgent", "summarize", "summary")
	defer srv.Close()

	bridge := setupA2ABridge(t, srv.URL)
	conv := newTestConversation()
	conv.config.a2aBridge = bridge

	conv.registerA2ATools()

	// The tool name is "a2a_myagent_summarize" (sanitized)
	tool, err := conv.toolRegistry.GetTool("a2a_myagent_summarize")
	require.NoError(t, err)
	assert.Equal(t, "a2a_myagent_summarize", tool.Name)
	assert.Equal(t, "a2a", tool.Mode)
	assert.NotNil(t, tool.A2AConfig)
	assert.Equal(t, srv.URL, tool.A2AConfig.AgentURL)
	assert.Equal(t, "summarize", tool.A2AConfig.SkillID)
}

func TestWithA2ATools_ExecutorRegistered(t *testing.T) {
	srv := a2aTestServer(t, "MyAgent", "summarize", "summary")
	defer srv.Close()

	bridge := setupA2ABridge(t, srv.URL)
	conv := newTestConversation()
	conv.config.a2aBridge = bridge

	conv.registerA2ATools()

	// The registry should be able to resolve the "a2a" executor for this tool.
	// We verify by calling Execute on the registry directly.
	result, err := conv.toolRegistry.Execute("a2a_myagent_summarize", json.RawMessage(`{"query":"test"}`))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Error)
	assert.NotNil(t, result.Result)
}

func TestWithA2ATools_AlongsidePackTools(t *testing.T) {
	srv := a2aTestServer(t, "RemoteAgent", "translate", "translated")
	defer srv.Close()

	bridge := setupA2ABridge(t, srv.URL)
	conv := newTestConversation()
	conv.config.a2aBridge = bridge

	// Register a local tool handler
	conv.OnTool("local_tool", func(args map[string]any) (any, error) {
		return "local result", nil
	})

	// Register local executor + A2A tools
	localExec := &localExecutor{handlers: conv.handlers}
	conv.toolRegistry.RegisterExecutor(localExec)

	// Also register the local tool descriptor
	_ = conv.toolRegistry.Register(&tools.ToolDescriptor{
		Name:        "local_tool",
		Description: "A local tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        "local",
	})

	conv.registerA2ATools()

	// Both should be present
	_, err := conv.toolRegistry.GetTool("local_tool")
	assert.NoError(t, err)

	_, err = conv.toolRegistry.GetTool("a2a_remoteagent_translate")
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

package sdk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
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

// openWithA2AAgent opens a conversation on the template test pack with the
// given A2A agent builders and options.
func openWithA2AAgent(t *testing.T, opts ...Option) (*Conversation, error) {
	t.Helper()
	base := []Option{
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
	}
	conv, err := Open(writeTestPack(t), "chat", append(base, opts...)...)
	if conv != nil {
		t.Cleanup(func() { _ = conv.Close() })
	}
	return conv, err
}

// TestWithA2AAgent_DiscoversAndRegistersTools is the #2087 regression: the
// builder path used to hand the capability an undiscovered bridge, so the
// agent's skills never became tools.
func TestWithA2AAgent_DiscoversAndRegistersTools(t *testing.T) {
	srv := a2aTestServer(t, "Remote", "summarize", "done")
	defer srv.Close()

	conv, err := openWithA2AAgent(t, WithA2AAgent(NewA2AAgent(srv.URL)))
	require.NoError(t, err)

	td := conv.ToolRegistry().Get("a2a__remote__summarize")
	require.NotNil(t, td, "the agent's skill must be registered as a tool")
	assert.Equal(t, "a2a", td.Mode)
	require.NotNil(t, td.A2AConfig)
	assert.Equal(t, "summarize", td.A2AConfig.SkillID)
}

func TestWithA2AAgent_UnreachableAgentDoesNotFailOpen(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	conv, err := openWithA2AAgent(t, WithA2AAgent(NewA2AAgent(srv.URL).WithTimeout(2000)))
	require.NoError(t, err)
	for _, td := range conv.ToolRegistry().GetTools() {
		assert.NotEqual(t, "a2a", td.Mode, "no a2a tools expected from an unreachable agent")
	}
}

func TestWithA2AAgent_RequiredAgentFailsOpen(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	_, err := openWithA2AAgent(t, WithA2AAgent(NewA2AAgent(srv.URL).Required()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), srv.URL)
}

// TestA2ACapability_DiscoversOnceAtInit pins that discovery happens in Init
// and nowhere else: RegisterTools runs on the first pipeline build, and
// repeating a failed discovery there would stall that build for another
// timeout; repeating a successful one would duplicate tools (RegisterAgent
// appends).
func TestA2ACapability_DiscoversOnceAtInit(t *testing.T) {
	var cardFetches atomic.Int32
	inner := a2aTestServer(t, "Flaky", "lookup", "ok")
	defer inner.Close()
	var up atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cardFetches.Add(1)
		}
		if !up.Load() {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		resp, err := http.Get(inner.URL + r.URL.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer srv.Close()

	newCap := func() *A2ACapability {
		cap := NewA2ACapability()
		wireA2AConfig([]Capability{cap}, &config{a2aAgents: []a2aAgentConfig{
			{url: srv.URL, config: NewA2AAgent(srv.URL).Build()},
		}})
		require.NoError(t, cap.Init(CapabilityContext{Pack: &pack.Pack{}}))
		return cap
	}

	down := newCap()
	fetchesAtInit := cardFetches.Load()
	up.Store(true)
	registry := tools.NewRegistry()
	down.RegisterTools(registry)
	assert.Nil(t, registry.Get("a2a__flaky__lookup"), "an agent down at Init contributes no tools")
	assert.Equal(t, fetchesAtInit, cardFetches.Load(), "RegisterTools must not repeat discovery")

	healthy := newCap()
	for i := 0; i < 2; i++ {
		registry = tools.NewRegistry()
		healthy.RegisterTools(registry)
		assert.Len(t, registry.GetTools(), 1, "a discovered agent's tools, once")
	}
}

// okA2AExecutor answers every call with a fixed result.
type okA2AExecutor struct{}

func (okA2AExecutor) Name() string { return "ok" }

func (okA2AExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"response":"ok"}`), nil
}

// A pack's agents section must not replace the host's executor: agent tools
// run on it just as bridge tools do.
func TestWithA2AToolExecutor_CoversPackAgentsToo(t *testing.T) {
	host := &policyExecutor{next: okA2AExecutor{}}
	cap := NewA2ACapability()
	cap.toolExecutor = host
	cap.endpointResolver = &StaticEndpointResolver{BaseURL: "http://localhost:9000"}
	p := &pack.Pack{Pack: packspec.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"orchestrator": {ID: "orchestrator", Tools: []string{"worker"}}},
		Agents: &pack.AgentsConfig{
			Entry:   "orchestrator",
			Members: map[string]*pack.AgentDef{"worker": {Description: "A worker agent"}},
		},
	}}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "orchestrator"}))

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)
	res, err := registry.Execute(context.Background(), "a2a__worker", json.RawMessage(`{"query":"hi"}`))
	require.NoError(t, err)
	assert.Empty(t, res.Error)
	assert.Equal(t, int32(1), host.calls.Load(), "the pack agent's call bypassed the host executor")
}

// policyExecutor records calls and delegates, standing in for a host's
// governance layer.
type policyExecutor struct {
	next  tools.Executor
	calls atomic.Int32
}

func (p *policyExecutor) Name() string { return "host-policy" }

func (p *policyExecutor) Execute(
	ctx context.Context, d *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	p.calls.Add(1)
	return p.next.Execute(ctx, d, args)
}

func TestWithA2AToolExecutor_RoutesBridgeTools(t *testing.T) {
	srv := a2aTestServer(t, "Remote", "summarize", "remote says hi")
	defer srv.Close()

	host := &policyExecutor{next: a2a.NewExecutor()}
	conv, err := openWithA2AAgent(t,
		WithA2AAgent(NewA2AAgent(srv.URL)),
		WithA2AToolExecutor(host),
	)
	require.NoError(t, err)

	res, err := conv.ToolRegistry().Execute(context.Background(), "a2a__remote__summarize",
		json.RawMessage(`{"query":"hi"}`))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Empty(t, res.Error)
	assert.Contains(t, string(res.Result), "remote says hi")
	assert.Equal(t, int32(1), host.calls.Load(), "the host executor must see the call")
}

func TestWithA2AToolExecutor_RejectsNil(t *testing.T) {
	assert.Error(t, WithA2AToolExecutor(nil)(&config{}))
}

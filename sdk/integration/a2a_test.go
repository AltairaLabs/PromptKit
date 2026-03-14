package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	a2amock "github.com/AltairaLabs/PromptKit/runtime/a2a/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// 9.1 — A2A tool bridge round-trip
// ---------------------------------------------------------------------------

func TestA2A_BridgeDiscoveryAndExecution(t *testing.T) {
	// Start a mock A2A server with a skill response.
	card := &a2a.AgentCard{
		Name:        "WeatherAgent",
		Description: "Provides weather information",
		Skills: []a2a.AgentSkill{
			{
				ID:          "get-weather",
				Name:        "Get Weather",
				Description: "Get weather for a location",
			},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}

	responseText := "It is 22°C and sunny in London"
	srv := a2amock.NewA2AServer(card,
		a2amock.WithSkillResponse("get-weather", a2amock.Response{
			Parts: []a2a.Part{{Text: &responseText}},
		}),
	)
	url, err := srv.Start()
	require.NoError(t, err)
	t.Cleanup(srv.Close)

	// Create a ToolBridge and discover the agent.
	client := a2a.NewClient(url)
	bridge := a2a.NewToolBridge(client)

	ctx := context.Background()
	descriptors, err := bridge.RegisterAgent(ctx)
	require.NoError(t, err)
	require.Len(t, descriptors, 1, "should discover one skill")

	td := descriptors[0]
	assert.Equal(t, "a2a__weatheragent__get_weather", td.Name)
	assert.Equal(t, "a2a", td.Mode)
	assert.Equal(t, url, td.A2AConfig.AgentURL)
	assert.Equal(t, "get-weather", td.A2AConfig.SkillID)

	// Register the tool and executor, then execute through the registry.
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(td))

	executor := a2a.NewExecutor(a2a.WithNoRetry())
	t.Cleanup(func() { _ = executor.Close() })
	registry.RegisterExecutor(executor)

	args, _ := json.Marshal(map[string]string{"query": "What is the weather in London?"})
	result, err := registry.ExecuteAsync(ctx, td.Name, args)
	require.NoError(t, err)
	assert.Equal(t, tools.ToolStatusComplete, result.Status)

	// Parse the response JSON and verify content.
	var response map[string]string
	require.NoError(t, json.Unmarshal(result.Content, &response))
	assert.Contains(t, response["response"], "22°C")
}

func TestA2A_BridgeDiscoveryWithSkillFilter(t *testing.T) {
	card := &a2a.AgentCard{
		Name:        "MultiSkillAgent",
		Description: "Agent with multiple skills",
		Skills: []a2a.AgentSkill{
			{ID: "weather", Name: "Weather", Description: "Get weather"},
			{ID: "news", Name: "News", Description: "Get news"},
			{ID: "translate", Name: "Translate", Description: "Translate text"},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}

	srv := a2amock.NewA2AServer(card)
	url, err := srv.Start()
	require.NoError(t, err)
	t.Cleanup(srv.Close)

	// Use skill filter to include only specific skills.
	client := a2a.NewClient(url)
	bridge := a2a.NewToolBridgeWithConfig(client, &tools.A2AConfig{
		SkillFilter: &tools.A2ASkillFilter{
			Allowlist: []string{"weather", "translate"},
		},
	})

	ctx := context.Background()
	descriptors, err := bridge.RegisterAgent(ctx)
	require.NoError(t, err)
	assert.Len(t, descriptors, 2, "should only include filtered skills")

	names := make([]string, len(descriptors))
	for i, d := range descriptors {
		names[i] = d.Name
	}
	assert.Contains(t, names, "a2a__multiskillagent__weather")
	assert.Contains(t, names, "a2a__multiskillagent__translate")
}

// ---------------------------------------------------------------------------
// 9.2 — Local agent executor
// ---------------------------------------------------------------------------

func TestA2A_LocalAgentExecutorDispatch(t *testing.T) {
	// Create a member conversation with a mock provider.
	member := openTestConv(t)

	// Create a LocalAgentExecutor with the member keyed by bare name.
	members := map[string]*testConvWrapper{"worker": {conv: member}}
	executor := newTestLocalAgentExecutor(members)

	// Create a descriptor with qualified name (as the pipeline would use).
	descriptor := &tools.ToolDescriptor{
		Name: "a2a__worker",
		Mode: "a2a",
	}

	// Execute with a query.
	args, _ := json.Marshal(map[string]string{"query": "Hello worker"})
	ctx := context.Background()
	result, err := executor.Execute(ctx, descriptor, args)
	require.NoError(t, err)

	// Parse response and verify the member conversation was called.
	var response map[string]string
	require.NoError(t, json.Unmarshal(result, &response))
	assert.NotEmpty(t, response["response"], "should get a response from the member")
}

func TestA2A_LocalAgentExecutorUnknownMember(t *testing.T) {
	// Empty members map — any dispatch should fail.
	executor := newTestLocalAgentExecutor(nil)

	descriptor := &tools.ToolDescriptor{
		Name: "a2a__nonexistent",
		Mode: "a2a",
	}

	args, _ := json.Marshal(map[string]string{"query": "hello"})
	_, err := executor.Execute(context.Background(), descriptor, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent member")
}

func TestA2A_LocalAgentExecutorNameResolution(t *testing.T) {
	// Verify that the executor correctly strips the a2a__ prefix.
	member := openTestConv(t)

	members := map[string]*testConvWrapper{"summarizer": {conv: member}}
	executor := newTestLocalAgentExecutor(members)

	// Use the full qualified name the pipeline would generate.
	descriptor := &tools.ToolDescriptor{
		Name: "a2a__summarizer",
		Mode: "a2a",
	}

	args, _ := json.Marshal(map[string]string{"query": "Summarize this"})
	result, err := executor.Execute(context.Background(), descriptor, args)
	require.NoError(t, err)

	var response map[string]string
	require.NoError(t, json.Unmarshal(result, &response))
	assert.NotEmpty(t, response["response"])
}

// ---------------------------------------------------------------------------
// 9.3 — A2A tool naming and resolution
// ---------------------------------------------------------------------------

func TestA2A_ToolNamingConventions(t *testing.T) {
	// Verify tool name qualification and parsing round-trips.
	qualified := tools.QualifyToolName("a2a", "weather_agent")
	assert.Equal(t, "a2a__weather_agent", qualified)

	ns, local := tools.ParseToolName(qualified)
	assert.Equal(t, "a2a", ns)
	assert.Equal(t, "weather_agent", local)

	// System tool detection
	assert.True(t, tools.IsSystemTool("a2a__my_agent"))
}

// ---------------------------------------------------------------------------
// Test helpers for A2A tests
// ---------------------------------------------------------------------------

// testConvWrapper wraps an sdk.Conversation to implement a minimal
// Send interface for the test local agent executor.
type testConvWrapper struct {
	conv *sdk.Conversation
}

// testLocalAgentExecutor is a simplified version of sdk.LocalAgentExecutor
// for integration testing. It mirrors the real implementation but works
// with testConvWrapper to avoid circular dependency issues.
type testLocalAgentExecutor struct {
	members map[string]*testConvWrapper
}

func newTestLocalAgentExecutor(members map[string]*testConvWrapper) *testLocalAgentExecutor {
	if members == nil {
		members = make(map[string]*testConvWrapper)
	}
	return &testLocalAgentExecutor{members: members}
}

func (e *testLocalAgentExecutor) Name() string { return "a2a" }

func (e *testLocalAgentExecutor) Execute(
	ctx context.Context,
	descriptor *tools.ToolDescriptor,
	args json.RawMessage,
) (json.RawMessage, error) {
	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}

	// Strip namespace prefix: "a2a__worker" → "worker"
	_, memberName := tools.ParseToolName(descriptor.Name)
	wrapper, ok := e.members[memberName]
	if !ok {
		return nil, fmt.Errorf("unknown agent member: %s (resolved from %s)", memberName, descriptor.Name)
	}

	resp, err := wrapper.conv.Send(ctx, input.Query)
	if err != nil {
		return nil, fmt.Errorf("agent %s failed: %w", memberName, err)
	}

	result := map[string]string{"response": resp.Text()}
	return json.Marshal(result)
}

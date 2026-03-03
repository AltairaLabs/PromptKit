package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolExecutionStatus_Constants(t *testing.T) {
	assert.Equal(t, ToolExecutionStatus("complete"), ToolStatusComplete)
	assert.Equal(t, ToolExecutionStatus("pending"), ToolStatusPending)
	assert.Equal(t, ToolExecutionStatus("failed"), ToolStatusFailed)
}

func TestToolExecutionResult_MarshalJSON(t *testing.T) {
	tests := []struct {
		name   string
		result ToolExecutionResult
	}{
		{
			name: "complete result",
			result: ToolExecutionResult{
				Status:  ToolStatusComplete,
				Content: json.RawMessage(`{"success": true}`),
			},
		},
		{
			name: "failed result",
			result: ToolExecutionResult{
				Status: ToolStatusFailed,
				Error:  "execution failed",
			},
		},
		{
			name: "pending result",
			result: ToolExecutionResult{
				Status: ToolStatusPending,
				PendingInfo: &PendingToolInfo{
					Reason:   "requires_approval",
					Message:  "Needs admin approval",
					ToolName: "delete_account",
					Args:     json.RawMessage(`{"account_id": "123"}`),
					Metadata: map[string]interface{}{
						"risk_level": "high",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.result)
			require.NoError(t, err)

			var decoded ToolExecutionResult
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)

			assert.Equal(t, tt.result.Status, decoded.Status)
			// For JSON comparison, unmarshal both to compare structure
			if len(tt.result.Content) > 0 {
				var expected, actual interface{}
				require.NoError(t, json.Unmarshal(tt.result.Content, &expected))
				require.NoError(t, json.Unmarshal(decoded.Content, &actual))
				assert.Equal(t, expected, actual)
			}
			assert.Equal(t, tt.result.Error, decoded.Error)

			if tt.result.PendingInfo != nil {
				require.NotNil(t, decoded.PendingInfo)
				assert.Equal(t, tt.result.PendingInfo.Reason, decoded.PendingInfo.Reason)
				assert.Equal(t, tt.result.PendingInfo.Message, decoded.PendingInfo.Message)
				assert.Equal(t, tt.result.PendingInfo.ToolName, decoded.PendingInfo.ToolName)
			}
		})
	}
}

func TestPendingToolInfo_MarshalJSON(t *testing.T) {
	expiresAt := time.Now().Add(1 * time.Hour)

	info := PendingToolInfo{
		Reason:      "requires_approval",
		Message:     "Admin approval required",
		ToolName:    "delete_user",
		Args:        json.RawMessage(`{"user_id": "456"}`),
		ExpiresAt:   &expiresAt,
		CallbackURL: "https://example.com/approve/123",
		Metadata: map[string]interface{}{
			"priority":   "high",
			"department": "finance",
		},
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var decoded PendingToolInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, info.Reason, decoded.Reason)
	assert.Equal(t, info.Message, decoded.Message)
	assert.Equal(t, info.ToolName, decoded.ToolName)
	// Compare JSON structure, not raw strings
	if len(info.Args) > 0 {
		var expectedArgs, actualArgs interface{}
		require.NoError(t, json.Unmarshal(info.Args, &expectedArgs))
		require.NoError(t, json.Unmarshal(decoded.Args, &actualArgs))
		assert.Equal(t, expectedArgs, actualArgs)
	}
	assert.Equal(t, info.CallbackURL, decoded.CallbackURL)
	assert.NotNil(t, decoded.ExpiresAt)
	assert.Equal(t, info.Metadata["priority"], decoded.Metadata["priority"])
}

func TestPendingToolInfo_OptionalFields(t *testing.T) {
	// Test that optional fields can be omitted
	info := PendingToolInfo{
		Reason:   "requires_approval",
		Message:  "Approval needed",
		ToolName: "test_tool",
		Args:     json.RawMessage(`{}`),
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var decoded PendingToolInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, info.Reason, decoded.Reason)
	assert.Nil(t, decoded.ExpiresAt)
	assert.Empty(t, decoded.CallbackURL)
	assert.Nil(t, decoded.Metadata)
}

func TestParseToolName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNS    string
		wantLocal string
	}{
		{"empty string", "", "", ""},
		{"no separator", "get_weather", "", "get_weather"},
		{"single underscore", "a2a_weather", "", "a2a_weather"},
		{"simple namespace", "a2a__forecast", "a2a", "forecast"},
		{"nested namespace", "a2a__weather__forecast", "a2a", "weather__forecast"},
		{"mcp namespace", "mcp__filesystem__read_file", "mcp", "filesystem__read_file"},
		{"system namespace", "workflow__transition", "workflow", "transition"},
		{"leading separator", "__orphan", "", "orphan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, local := ParseToolName(tt.input)
			assert.Equal(t, tt.wantNS, ns)
			assert.Equal(t, tt.wantLocal, local)
		})
	}
}

func TestQualifyToolName(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		local     string
		want      string
	}{
		{"empty namespace", "", "get_weather", "get_weather"},
		{"a2a namespace", "a2a", "weather__forecast", "a2a__weather__forecast"},
		{"mcp namespace", "mcp", "fs__read", "mcp__fs__read"},
		{"empty both", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, QualifyToolName(tt.namespace, tt.local))
		})
	}
}

func TestIsSystemTool(t *testing.T) {
	tests := []struct {
		name string
		tool string
		want bool
	}{
		{"a2a tool", "a2a__weather__forecast", true},
		{"mcp tool", "mcp__fs__read_file", true},
		{"workflow tool", "workflow__transition", true},
		{"memory tool", "memory__recall", true},
		{"user tool", "get_weather", false},
		{"empty string", "", false},
		{"unknown namespace", "custom__tool", false},
		{"single underscore prefix", "a2a_weather", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsSystemTool(tt.tool))
		})
	}
}

func TestParseToolName_RoundTrip(t *testing.T) {
	// Verify that QualifyToolName(ParseToolName(name)) == name for namespaced names
	names := []string{
		"a2a__weather__forecast",
		"mcp__filesystem__read_file",
		"workflow__get_state",
	}
	for _, name := range names {
		ns, local := ParseToolName(name)
		assert.Equal(t, name, QualifyToolName(ns, local))
	}
}

func TestClientConfig_MarshalJSON(t *testing.T) {
	cfg := ClientConfig{
		Consent: &ConsentConfig{
			Required:        true,
			Message:         "This app wants to access your location.",
			DeclineStrategy: DeclineStrategyFallback,
		},
		TimeoutMs:      30000,
		Categories:     []string{"location", "sensors"},
		ValidateOutput: true,
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded ClientConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, cfg.TimeoutMs, decoded.TimeoutMs)
	assert.Equal(t, cfg.Categories, decoded.Categories)
	assert.Equal(t, cfg.ValidateOutput, decoded.ValidateOutput)
	require.NotNil(t, decoded.Consent)
	assert.True(t, decoded.Consent.Required)
	assert.Equal(t, "This app wants to access your location.", decoded.Consent.Message)
	assert.Equal(t, DeclineStrategyFallback, decoded.Consent.DeclineStrategy)
}

func TestClientConfig_OptionalFields(t *testing.T) {
	cfg := ClientConfig{}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded ClientConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Nil(t, decoded.Consent)
	assert.Zero(t, decoded.TimeoutMs)
	assert.Nil(t, decoded.Categories)
	assert.False(t, decoded.ValidateOutput)
}

func TestConsentConfig_DeclineStrategies(t *testing.T) {
	strategies := []string{
		DeclineStrategyFallback,
		DeclineStrategyError,
		DeclineStrategyRetry,
	}
	assert.Equal(t, "fallback", strategies[0])
	assert.Equal(t, "error", strategies[1])
	assert.Equal(t, "retry", strategies[2])
}

func TestToolDescriptor_WithClientConfig(t *testing.T) {
	descriptor := ToolDescriptor{
		Name:         "get_location",
		Description:  "Get GPS coordinates",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"lat":{"type":"number"},"lng":{"type":"number"}}}`),
		Mode:         "client",
		ClientConfig: &ClientConfig{
			Consent: &ConsentConfig{
				Required:        true,
				Message:         "Allow location access?",
				DeclineStrategy: DeclineStrategyError,
			},
			TimeoutMs:      15000,
			Categories:     []string{"location"},
			ValidateOutput: true,
		},
		MockResult: json.RawMessage(`{"lat":37.7749,"lng":-122.4194}`),
	}

	data, err := json.Marshal(descriptor)
	require.NoError(t, err)

	var decoded ToolDescriptor
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "client", decoded.Mode)
	require.NotNil(t, decoded.ClientConfig)
	require.NotNil(t, decoded.ClientConfig.Consent)
	assert.True(t, decoded.ClientConfig.Consent.Required)
	assert.Equal(t, "Allow location access?", decoded.ClientConfig.Consent.Message)
	assert.Equal(t, DeclineStrategyError, decoded.ClientConfig.Consent.DeclineStrategy)
	assert.Equal(t, 15000, decoded.ClientConfig.TimeoutMs)
	assert.Equal(t, []string{"location"}, decoded.ClientConfig.Categories)
	assert.True(t, decoded.ClientConfig.ValidateOutput)
	assert.NotEmpty(t, decoded.MockResult)
}

// Mock executor for testing AsyncToolExecutor interface
type mockAsyncExecutor struct {
	name   string
	result *ToolExecutionResult
	err    error
}

func (m *mockAsyncExecutor) Name() string {
	return m.name
}

func (m *mockAsyncExecutor) Execute(ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	result, err := m.ExecuteAsync(ctx, descriptor, args)
	if err != nil {
		return nil, err
	}
	if result.Status == ToolStatusPending {
		return nil, assert.AnError
	}
	return result.Content, nil
}

func (m *mockAsyncExecutor) ExecuteAsync(_ context.Context, _ *ToolDescriptor, _ json.RawMessage) (*ToolExecutionResult, error) {
	return m.result, m.err
}

func TestAsyncToolExecutor_Interface(t *testing.T) {
	// Verify that mockAsyncExecutor implements both interfaces
	var _ Executor = &mockAsyncExecutor{}
	var _ AsyncToolExecutor = &mockAsyncExecutor{}

	executor := &mockAsyncExecutor{
		name: "test_async",
		result: &ToolExecutionResult{
			Status: ToolStatusPending,
			PendingInfo: &PendingToolInfo{
				Reason:   "test",
				Message:  "Test pending",
				ToolName: "test_async",
				Args:     json.RawMessage(`{}`),
			},
		},
	}

	// Test ExecuteAsync
	result, err := executor.ExecuteAsync(context.Background(), &ToolDescriptor{Name: "test"}, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, ToolStatusPending, result.Status)
	assert.NotNil(t, result.PendingInfo)

	// Test Execute falls back correctly
	_, err = executor.Execute(context.Background(), &ToolDescriptor{Name: "test"}, json.RawMessage(`{}`))
	assert.Error(t, err) // Should error because result is pending
}

func TestAsyncToolExecutor_CompleteResult(t *testing.T) {
	executor := &mockAsyncExecutor{
		name: "test_sync",
		result: &ToolExecutionResult{
			Status:  ToolStatusComplete,
			Content: json.RawMessage(`{"result": "success"}`),
		},
	}

	// Test Execute with complete result
	result, err := executor.Execute(context.Background(), &ToolDescriptor{Name: "test"}, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, `{"result": "success"}`, string(result))
}

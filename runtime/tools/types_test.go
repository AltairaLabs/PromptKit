package tools

import (
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

// Mock executor for testing AsyncToolExecutor interface
type mockAsyncExecutor struct {
	name   string
	result *ToolExecutionResult
	err    error
}

func (m *mockAsyncExecutor) Name() string {
	return m.name
}

func (m *mockAsyncExecutor) Execute(descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	result, err := m.ExecuteAsync(descriptor, args)
	if err != nil {
		return nil, err
	}
	if result.Status == ToolStatusPending {
		return nil, assert.AnError
	}
	return result.Content, nil
}

func (m *mockAsyncExecutor) ExecuteAsync(descriptor *ToolDescriptor, args json.RawMessage) (*ToolExecutionResult, error) {
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
	result, err := executor.ExecuteAsync(&ToolDescriptor{Name: "test"}, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, ToolStatusPending, result.Status)
	assert.NotNil(t, result.PendingInfo)

	// Test Execute falls back correctly
	_, err = executor.Execute(&ToolDescriptor{Name: "test"}, json.RawMessage(`{}`))
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
	result, err := executor.Execute(&ToolDescriptor{Name: "test"}, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, `{"result": "success"}`, string(result))
}

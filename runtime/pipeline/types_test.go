package pipeline

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMCall_JSONSerialization(t *testing.T) {
	// Test that LLMCall with error can be serialized to JSON
	startTime := time.Now()
	testError := errors.New("test error message")

	llmCall := LLMCall{
		Sequence:     1,
		MessageIndex: 0,
		Response: &Response{
			Content: "test response",
		},
		StartedAt: startTime,
		Duration:  100 * time.Millisecond,
		Cost: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
	}
	llmCall.SetError(testError)

	// Serialize to JSON
	jsonBytes, err := json.Marshal(llmCall)
	require.NoError(t, err, "should be able to marshal LLMCall to JSON")

	// Deserialize from JSON
	var deserialized LLMCall
	err = json.Unmarshal(jsonBytes, &deserialized)
	require.NoError(t, err, "should be able to unmarshal LLMCall from JSON")

	// Verify fields
	assert.Equal(t, llmCall.Sequence, deserialized.Sequence)
	assert.Equal(t, llmCall.MessageIndex, deserialized.MessageIndex)
	assert.Equal(t, llmCall.Response.Content, deserialized.Response.Content)

	// The error field should now serialize/deserialize properly
	assert.NotNil(t, deserialized.Error)
	assert.Equal(t, "test error message", *deserialized.Error)

	// Test GetError helper
	deserializedErr := deserialized.GetError()
	assert.NotNil(t, deserializedErr)
	assert.Equal(t, "test error message", deserializedErr.Error())
}

func TestLLMCall_JSONSerialization_NilError(t *testing.T) {
	// Test that LLMCall without error serializes properly
	startTime := time.Now()

	llmCall := LLMCall{
		Sequence:     1,
		MessageIndex: 0,
		Response: &Response{
			Content: "test response",
		},
		StartedAt: startTime,
		Duration:  100 * time.Millisecond,
		Cost: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
		Error: nil,
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(llmCall)
	require.NoError(t, err, "should be able to marshal LLMCall to JSON")

	// Check JSON doesn't contain error field
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonBytes, &jsonMap)
	require.NoError(t, err)

	// error field should be omitted when nil
	_, hasError := jsonMap["error"]
	assert.False(t, hasError, "error field should be omitted when nil")
}

func TestExecutionTrace_JSONSerialization(t *testing.T) {
	// Test that ExecutionTrace with LLMCalls containing errors serializes properly
	startTime := time.Now()
	completedTime := startTime.Add(1 * time.Second)

	llmCall1 := LLMCall{
		Sequence:     1,
		MessageIndex: 0,
		Response: &Response{
			Content: "success",
		},
		StartedAt: startTime,
		Duration:  100 * time.Millisecond,
	}

	llmCall2 := LLMCall{
		Sequence:     2,
		MessageIndex: 1,
		Response:     nil,
		StartedAt:    startTime.Add(200 * time.Millisecond),
		Duration:     50 * time.Millisecond,
	}
	llmCall2.SetError(errors.New("API rate limit exceeded"))

	trace := ExecutionTrace{
		StartedAt:   startTime,
		CompletedAt: &completedTime,
		LLMCalls: []LLMCall{
			llmCall1,
			llmCall2,
		},
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(trace)
	require.NoError(t, err, "should be able to marshal ExecutionTrace to JSON")

	// Deserialize from JSON
	var deserialized ExecutionTrace
	err = json.Unmarshal(jsonBytes, &deserialized)
	require.NoError(t, err, "should be able to unmarshal ExecutionTrace from JSON")

	// Verify structure
	assert.Len(t, deserialized.LLMCalls, 2)
	assert.Equal(t, 1, deserialized.LLMCalls[0].Sequence)
	assert.Equal(t, 2, deserialized.LLMCalls[1].Sequence)

	// Verify error handling
	assert.Nil(t, deserialized.LLMCalls[0].Error)
	assert.NotNil(t, deserialized.LLMCalls[1].Error)
	assert.Contains(t, *deserialized.LLMCalls[1].Error, "rate limit")

	// Test GetError helper
	err1 := deserialized.LLMCalls[0].GetError()
	assert.Nil(t, err1)

	err2 := deserialized.LLMCalls[1].GetError()
	assert.NotNil(t, err2)
	assert.Contains(t, err2.Error(), "rate limit")
}

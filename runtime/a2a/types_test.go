package a2a

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func TestTaskState_JSON(t *testing.T) {
	tests := []struct {
		name  string
		state TaskState
		json  string
	}{
		{"submitted", TaskStateSubmitted, `"submitted"`},
		{"working", TaskStateWorking, `"working"`},
		{"completed", TaskStateCompleted, `"completed"`},
		{"failed", TaskStateFailed, `"failed"`},
		{"canceled", TaskStateCanceled, `"canceled"`},
		{"input_required", TaskStateInputRequired, `"input_required"`},
		{"rejected", TaskStateRejected, `"rejected"`},
		{"auth_required", TaskStateAuthRequired, `"auth_required"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.state)
			require.NoError(t, err)
			assert.Equal(t, tt.json, string(data))

			var got TaskState
			err = json.Unmarshal(data, &got)
			require.NoError(t, err)
			assert.Equal(t, tt.state, got)
		})
	}
}

func TestTaskState_InvalidJSON(t *testing.T) {
	var s TaskState
	err := json.Unmarshal([]byte(`"bogus"`), &s)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid task state")

	_, err = TaskState("bogus").MarshalJSON()
	assert.Error(t, err)
}

func TestPart_TextRoundTrip(t *testing.T) {
	p := Part{Text: ptr("hello world")}

	data, err := json.Marshal(p)
	require.NoError(t, err)

	var got Part
	require.NoError(t, json.Unmarshal(data, &got))
	require.NotNil(t, got.Text)
	assert.Equal(t, "hello world", *got.Text)
	assert.Nil(t, got.Raw)
	assert.Nil(t, got.URL)
	assert.Nil(t, got.Data)
}

func TestPart_RawRoundTrip(t *testing.T) {
	p := Part{
		Raw:       []byte{0xDE, 0xAD, 0xBE, 0xEF},
		MediaType: "application/octet-stream",
		Filename:  "data.bin",
	}

	data, err := json.Marshal(p)
	require.NoError(t, err)

	var got Part
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, []byte{0xDE, 0xAD, 0xBE, 0xEF}, got.Raw)
	assert.Equal(t, "application/octet-stream", got.MediaType)
	assert.Equal(t, "data.bin", got.Filename)
}

func TestPart_URLRoundTrip(t *testing.T) {
	p := Part{
		URL:       ptr("https://example.com/file.pdf"),
		MediaType: "application/pdf",
	}

	data, err := json.Marshal(p)
	require.NoError(t, err)

	var got Part
	require.NoError(t, json.Unmarshal(data, &got))
	require.NotNil(t, got.URL)
	assert.Equal(t, "https://example.com/file.pdf", *got.URL)
	assert.Equal(t, "application/pdf", got.MediaType)
}

func TestPart_DataRoundTrip(t *testing.T) {
	p := Part{
		Data: map[string]any{
			"key":   "value",
			"count": float64(42),
		},
	}

	data, err := json.Marshal(p)
	require.NoError(t, err)

	var got Part
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "value", got.Data["key"])
	assert.Equal(t, float64(42), got.Data["count"])
}

func TestMessage_RoundTrip(t *testing.T) {
	msg := Message{
		MessageID: "msg-1",
		ContextID: "ctx-1",
		TaskID:    "task-1",
		Role:      RoleUser,
		Parts:     []Part{{Text: ptr("What is the weather?")}},
		Metadata:  map[string]any{"source": "test"},
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var got Message
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "msg-1", got.MessageID)
	assert.Equal(t, "ctx-1", got.ContextID)
	assert.Equal(t, RoleUser, got.Role)
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "What is the weather?", *got.Parts[0].Text)
	assert.Equal(t, "test", got.Metadata["source"])
}

func TestArtifact_RoundTrip(t *testing.T) {
	a := Artifact{
		ArtifactID:  "art-1",
		Name:        "result",
		Description: "The generated output",
		Parts:       []Part{{Text: ptr("Generated text")}},
	}

	data, err := json.Marshal(a)
	require.NoError(t, err)

	var got Artifact
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "art-1", got.ArtifactID)
	assert.Equal(t, "result", got.Name)
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "Generated text", *got.Parts[0].Text)
}

func TestTask_RoundTrip(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	task := Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status: TaskStatus{
			State:     TaskStateWorking,
			Timestamp: &now,
			Message: &Message{
				MessageID: "status-msg",
				Role:      RoleAgent,
				Parts:     []Part{{Text: ptr("Processing your request")}},
			},
		},
		Artifacts: []Artifact{
			{
				ArtifactID: "art-1",
				Name:       "output",
				Parts:      []Part{{Text: ptr("Result data")}},
			},
		},
		History: []Message{
			{
				MessageID: "msg-1",
				Role:      RoleUser,
				Parts:     []Part{{Text: ptr("Do something")}},
			},
		},
	}

	data, err := json.Marshal(task)
	require.NoError(t, err)

	var got Task
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "task-1", got.ID)
	assert.Equal(t, TaskStateWorking, got.Status.State)
	require.NotNil(t, got.Status.Message)
	assert.Equal(t, "status-msg", got.Status.Message.MessageID)
	require.Len(t, got.Artifacts, 1)
	assert.Equal(t, "art-1", got.Artifacts[0].ArtifactID)
	require.Len(t, got.History, 1)
	assert.Equal(t, "msg-1", got.History[0].MessageID)
}

func TestAgentCard_Deserialize(t *testing.T) {
	raw := `{
		"name": "Weather Agent",
		"description": "Provides weather information",
		"version": "1.0.0",
		"provider": {
			"organization": "Acme Corp",
			"url": "https://acme.example.com"
		},
		"capabilities": {
			"streaming": true,
			"pushNotifications": false
		},
		"skills": [
			{
				"id": "weather-lookup",
				"name": "Weather Lookup",
				"description": "Look up current weather",
				"tags": ["weather", "forecast"],
				"examples": ["What is the weather in London?"],
				"inputModes": ["text"],
				"outputModes": ["text"]
			}
		],
		"defaultInputModes": ["text"],
		"defaultOutputModes": ["text"],
		"supportedInterfaces": [
			{
				"url": "https://agent.example.com/a2a",
				"protocolBinding": "jsonrpc+http",
				"protocolVersion": "0.2.1"
			}
		],
		"iconUrl": "https://agent.example.com/icon.png",
		"documentationUrl": "https://agent.example.com/docs"
	}`

	var card AgentCard
	require.NoError(t, json.Unmarshal([]byte(raw), &card))

	assert.Equal(t, "Weather Agent", card.Name)
	assert.Equal(t, "Provides weather information", card.Description)
	assert.Equal(t, "1.0.0", card.Version)
	require.NotNil(t, card.Provider)
	assert.Equal(t, "Acme Corp", card.Provider.Organization)
	assert.True(t, card.Capabilities.Streaming)
	assert.False(t, card.Capabilities.PushNotifications)
	require.Len(t, card.Skills, 1)
	assert.Equal(t, "weather-lookup", card.Skills[0].ID)
	assert.Equal(t, []string{"weather", "forecast"}, card.Skills[0].Tags)
	require.Len(t, card.SupportedInterfaces, 1)
	assert.Equal(t, "https://agent.example.com/a2a", card.SupportedInterfaces[0].URL)
	assert.Equal(t, "https://agent.example.com/icon.png", card.IconURL)
}

func TestJSONRPCRequest_RoundTrip(t *testing.T) {
	params := json.RawMessage(`{"message":{"messageId":"m1","role":"user","parts":[{"text":"hello"}]}}`)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  MethodSendMessage,
		Params:  params,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var got JSONRPCRequest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "2.0", got.JSONRPC)
	assert.Equal(t, float64(1), got.ID)
	assert.Equal(t, MethodSendMessage, got.Method)

	var sendReq SendMessageRequest
	require.NoError(t, json.Unmarshal(got.Params, &sendReq))
	assert.Equal(t, "m1", sendReq.Message.MessageID)
}

func TestJSONRPCResponse_RoundTrip(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		result := json.RawMessage(`{"id":"task-1","contextId":"ctx-1","status":{"state":"submitted"}}`)
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      float64(1),
			Result:  result,
		}

		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var got JSONRPCResponse
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Nil(t, got.Error)

		var task Task
		require.NoError(t, json.Unmarshal(got.Result, &task))
		assert.Equal(t, "task-1", task.ID)
		assert.Equal(t, TaskStateSubmitted, task.Status.State)
	})

	t.Run("error", func(t *testing.T) {
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      float64(1),
			Error:   &JSONRPCError{Code: -32600, Message: "Invalid Request"},
		}

		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var got JSONRPCResponse
		require.NoError(t, json.Unmarshal(data, &got))
		require.NotNil(t, got.Error)
		assert.Equal(t, -32600, got.Error.Code)
		assert.Equal(t, "Invalid Request", got.Error.Message)
	})
}

func TestSendMessageRequest_RoundTrip(t *testing.T) {
	req := SendMessageRequest{
		Message: Message{
			MessageID: "msg-1",
			Role:      RoleUser,
			Parts:     []Part{{Text: ptr("hello")}},
		},
		Configuration: &SendMessageConfiguration{
			AcceptedOutputModes: []string{"text"},
			HistoryLength:       ptr(10),
			Blocking:            true,
		},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var got SendMessageRequest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "msg-1", got.Message.MessageID)
	require.NotNil(t, got.Configuration)
	assert.Equal(t, []string{"text"}, got.Configuration.AcceptedOutputModes)
	assert.Equal(t, 10, *got.Configuration.HistoryLength)
	assert.True(t, got.Configuration.Blocking)
}

func TestListTasksResponse_RoundTrip(t *testing.T) {
	resp := ListTasksResponse{
		Tasks: []Task{
			{ID: "t1", ContextID: "c1", Status: TaskStatus{State: TaskStateCompleted}},
			{ID: "t2", ContextID: "c1", Status: TaskStatus{State: TaskStateWorking}},
		},
		NextPageToken: "token-abc",
		PageSize:      10,
		TotalSize:     25,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var got ListTasksResponse
	require.NoError(t, json.Unmarshal(data, &got))
	require.Len(t, got.Tasks, 2)
	assert.Equal(t, "t1", got.Tasks[0].ID)
	assert.Equal(t, TaskStateCompleted, got.Tasks[0].Status.State)
	assert.Equal(t, "token-abc", got.NextPageToken)
	assert.Equal(t, 25, got.TotalSize)
}

func TestStreamingEvents_RoundTrip(t *testing.T) {
	t.Run("status update", func(t *testing.T) {
		now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
		evt := TaskStatusUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Status: TaskStatus{
				State:     TaskStateWorking,
				Timestamp: &now,
			},
		}

		data, err := json.Marshal(evt)
		require.NoError(t, err)

		var got TaskStatusUpdateEvent
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, "task-1", got.TaskID)
		assert.Equal(t, TaskStateWorking, got.Status.State)
	})

	t.Run("artifact update", func(t *testing.T) {
		evt := TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact: Artifact{
				ArtifactID: "art-1",
				Parts:      []Part{{Text: ptr("chunk")}},
			},
			Append:    true,
			LastChunk: false,
		}

		data, err := json.Marshal(evt)
		require.NoError(t, err)

		var got TaskArtifactUpdateEvent
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, "art-1", got.Artifact.ArtifactID)
		assert.True(t, got.Append)
		assert.False(t, got.LastChunk)
	})
}

func TestMethodConstants(t *testing.T) {
	assert.Equal(t, "message/send", MethodSendMessage)
	assert.Equal(t, "message/stream", MethodSendStreamingMessage)
	assert.Equal(t, "tasks/get", MethodGetTask)
	assert.Equal(t, "tasks/cancel", MethodCancelTask)
	assert.Equal(t, "tasks/list", MethodListTasks)
}

package events

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileEventStore(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFileEventStore(dir)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	assert.Equal(t, dir, store.dir)
}

func TestNewFileEventStore_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "events")

	store, err := NewFileEventStore(dir)
	require.NoError(t, err)
	defer store.Close()

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFileEventStore_Append(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	event := &Event{
		Type:      EventMessageCreated,
		Timestamp: time.Now(),
		SessionID: "session-123",
		Data: &MessageCreatedData{
			Role:    "user",
			Content: "Hello, world!",
		},
	}

	err = store.Append(context.Background(), event)
	require.NoError(t, err)

	// Verify file was created
	path := store.sessionPath("session-123")
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestFileEventStore_Append_RequiresSessionID(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	event := &Event{
		Type:      EventMessageCreated,
		Timestamp: time.Now(),
		// No SessionID
	}

	err = store.Append(context.Background(), event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session ID")
}

func TestFileEventStore_Query(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	sessionID := "session-query-test"
	now := time.Now()

	// Append multiple events
	events := []*Event{
		{Type: EventMessageCreated, Timestamp: now, SessionID: sessionID, ConversationID: "conv-1"},
		{Type: EventToolCallStarted, Timestamp: now.Add(time.Second), SessionID: sessionID, ConversationID: "conv-1"},
		{Type: EventToolCallCompleted, Timestamp: now.Add(2 * time.Second), SessionID: sessionID, ConversationID: "conv-1"},
		{Type: EventMessageCreated, Timestamp: now.Add(3 * time.Second), SessionID: sessionID, ConversationID: "conv-2"},
	}

	for _, e := range events {
		require.NoError(t, store.Append(context.Background(), e))
	}
	require.NoError(t, store.Sync())

	t.Run("all events for session", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{SessionID: sessionID})
		require.NoError(t, err)
		assert.Len(t, result, 4)
	})

	t.Run("filter by conversation", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID:      sessionID,
			ConversationID: "conv-1",
		})
		require.NoError(t, err)
		assert.Len(t, result, 3)
	})

	t.Run("filter by type", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID: sessionID,
			Types:     []EventType{EventMessageCreated},
		})
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("limit results", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID: sessionID,
			Limit:     2,
		})
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("non-existent session", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{SessionID: "no-such-session"})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("requires session ID", func(t *testing.T) {
		_, err := store.Query(context.Background(), &EventFilter{})
		require.Error(t, err)
	})
}

func TestFileEventStore_QueryRaw(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	sessionID := "session-queryraw-test"
	now := time.Now()

	// Append events with data
	events := []*Event{
		{
			Type:      EventMessageCreated,
			Timestamp: now,
			SessionID: sessionID,
			Data:      &MessageCreatedData{Role: "user", Content: "Hello"},
		},
		{
			Type:      EventMessageCreated,
			Timestamp: now.Add(time.Second),
			SessionID: sessionID,
			Data:      &MessageCreatedData{Role: "assistant", Content: "Hi there!"},
		},
	}

	for _, e := range events {
		require.NoError(t, store.Append(context.Background(), e))
	}
	require.NoError(t, store.Sync())

	t.Run("returns stored events with raw data", func(t *testing.T) {
		result, err := store.QueryRaw(context.Background(), &EventFilter{SessionID: sessionID})
		require.NoError(t, err)
		assert.Len(t, result, 2)

		// Verify raw data is preserved
		assert.NotEmpty(t, result[0].Event.Data)
		assert.NotEmpty(t, result[0].Event.DataType)
		assert.Equal(t, "*events.MessageCreatedData", result[0].Event.DataType)
	})

	t.Run("non-existent session returns nil", func(t *testing.T) {
		result, err := store.QueryRaw(context.Background(), &EventFilter{SessionID: "no-such-session"})
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("requires session ID", func(t *testing.T) {
		_, err := store.QueryRaw(context.Background(), &EventFilter{})
		require.Error(t, err)
	})
}

func TestFileEventStore_Stream(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	sessionID := "session-stream-test"

	// Append events
	for i := 0; i < 5; i++ {
		require.NoError(t, store.Append(context.Background(), &Event{
			Type:      EventMessageCreated,
			Timestamp: time.Now(),
			SessionID: sessionID,
		}))
	}

	// Close the file to ensure data is flushed
	require.NoError(t, store.Close())

	// Reopen for reading
	store, err = NewFileEventStore(store.dir)
	require.NoError(t, err)
	defer store.Close()

	ch, err := store.Stream(context.Background(), sessionID)
	require.NoError(t, err)

	var count int
	for range ch {
		count++
	}
	assert.Equal(t, 5, count)
}

func TestFileEventStore_Stream_NonExistentSession(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	ch, err := store.Stream(context.Background(), "no-such-session")
	require.NoError(t, err)

	var count int
	for range ch {
		count++
	}
	assert.Equal(t, 0, count)
}

func TestFileEventStore_Stream_ContextCancellation(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	sessionID := "session-cancel-test"

	// Append many events
	for i := 0; i < 100; i++ {
		require.NoError(t, store.Append(context.Background(), &Event{
			Type:      EventMessageCreated,
			Timestamp: time.Now(),
			SessionID: sessionID,
		}))
	}

	require.NoError(t, store.Close())
	store, err = NewFileEventStore(store.dir)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := store.Stream(ctx, sessionID)
	require.NoError(t, err)

	// Read a few then cancel
	<-ch
	<-ch
	cancel()

	// Channel should close eventually
	for range ch {
		// drain
	}
}

func TestEventBus_WithStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileEventStore(dir)
	require.NoError(t, err)
	defer store.Close()

	bus := NewEventBus().WithStore(store)
	assert.Equal(t, store, bus.Store())

	sessionID := "session-bus-test"

	// Publish an event
	event := &Event{
		Type:      EventMessageCreated,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      &MessageCreatedData{Role: "user", Content: "test"},
	}
	bus.Publish(event)

	// Sync to disk
	require.NoError(t, store.Sync())

	// Query the store
	events, err := store.Query(context.Background(), &EventFilter{SessionID: sessionID})
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, EventMessageCreated, events[0].Type)
}

func TestEventBus_WithStore_SkipsEventsWithoutSessionID(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	bus := NewEventBus().WithStore(store)

	// Publish event without session ID
	event := &Event{
		Type:      EventPipelineStarted,
		Timestamp: time.Now(),
		// No SessionID
	}
	bus.Publish(event)

	time.Sleep(50 * time.Millisecond)

	// No files should be created
	entries, _ := os.ReadDir(store.dir)
	assert.Empty(t, entries)
}

func TestSerializableEvent_RawData(t *testing.T) {
	rawJSON := json.RawMessage(`{"role":"user","content":"test"}`)
	se := &SerializableEvent{
		Data:     rawJSON,
		DataType: "*events.MessageCreatedData",
	}

	result := se.RawData()
	assert.Equal(t, rawJSON, result)
}

func TestDeserializeEventData(t *testing.T) {
	tests := []struct {
		name     string
		dataType string
		data     string
		check    func(t *testing.T, result EventData)
	}{
		{
			name:     "AudioInputData",
			dataType: "*events.AudioInputData",
			data:     `{"actor":"user","chunk_index":1}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*AudioInputData)
				require.True(t, ok)
				assert.Equal(t, "user", data.Actor)
				assert.Equal(t, 1, data.ChunkIndex)
			},
		},
		{
			name:     "AudioOutputData",
			dataType: "*events.AudioOutputData",
			data:     `{"chunk_index":2}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*AudioOutputData)
				require.True(t, ok)
				assert.Equal(t, 2, data.ChunkIndex)
			},
		},
		{
			name:     "MessageCreatedData",
			dataType: "*events.MessageCreatedData",
			data:     `{"role":"assistant","content":"Hello!"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*MessageCreatedData)
				require.True(t, ok)
				assert.Equal(t, "assistant", data.Role)
				assert.Equal(t, "Hello!", data.Content)
			},
		},
		{
			name:     "ToolCallStartedData",
			dataType: "*events.ToolCallStartedData",
			data:     `{"ToolName":"get_weather","CallID":"call-1"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ToolCallStartedData)
				require.True(t, ok)
				assert.Equal(t, "get_weather", data.ToolName)
				assert.Equal(t, "call-1", data.CallID)
			},
		},
		{
			name:     "ProviderCallCompletedData",
			dataType: "*events.ProviderCallCompletedData",
			data:     `{"Provider":"openai","InputTokens":100,"OutputTokens":50}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ProviderCallCompletedData)
				require.True(t, ok)
				assert.Equal(t, "openai", data.Provider)
				assert.Equal(t, 100, data.InputTokens)
				assert.Equal(t, 50, data.OutputTokens)
			},
		},
		{
			name:     "PipelineStartedData",
			dataType: "*events.PipelineStartedData",
			data:     `{"MiddlewareCount":3}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*PipelineStartedData)
				require.True(t, ok)
				assert.Equal(t, 3, data.MiddlewareCount)
			},
		},
		{
			name:     "ConversationStartedData",
			dataType: "*events.ConversationStartedData",
			data:     `{"SystemPrompt":"You are a helpful assistant"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ConversationStartedData)
				require.True(t, ok)
				assert.Equal(t, "You are a helpful assistant", data.SystemPrompt)
			},
		},
		{
			name:     "AudioTranscriptionData",
			dataType: "*events.AudioTranscriptionData",
			data:     `{"text":"Hello world","language":"en-US","confidence":0.95}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*AudioTranscriptionData)
				require.True(t, ok)
				assert.Equal(t, "Hello world", data.Text)
				assert.Equal(t, "en-US", data.Language)
				assert.Equal(t, 0.95, data.Confidence)
			},
		},
		{
			name:     "VideoFrameData",
			dataType: "*events.VideoFrameData",
			data:     `{"frame_index":42}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*VideoFrameData)
				require.True(t, ok)
				assert.Equal(t, int64(42), data.FrameIndex)
			},
		},
		{
			name:     "ScreenshotData",
			dataType: "*events.ScreenshotData",
			data:     `{"window_title":"Terminal"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ScreenshotData)
				require.True(t, ok)
				assert.Equal(t, "Terminal", data.WindowTitle)
			},
		},
		{
			name:     "ImageInputData",
			dataType: "*events.ImageInputData",
			data:     `{"actor":"user"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ImageInputData)
				require.True(t, ok)
				assert.Equal(t, "user", data.Actor)
			},
		},
		{
			name:     "ImageOutputData",
			dataType: "*events.ImageOutputData",
			data:     `{"generated_from":"dalle"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ImageOutputData)
				require.True(t, ok)
				assert.Equal(t, "dalle", data.GeneratedFrom)
			},
		},
		{
			name:     "MessageUpdatedData",
			dataType: "*events.MessageUpdatedData",
			data:     `{"Index":5,"LatencyMs":150}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*MessageUpdatedData)
				require.True(t, ok)
				assert.Equal(t, 5, data.Index)
				assert.Equal(t, int64(150), data.LatencyMs)
			},
		},
		{
			name:     "PipelineCompletedData",
			dataType: "*events.PipelineCompletedData",
			data:     `{"InputTokens":100,"OutputTokens":50}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*PipelineCompletedData)
				require.True(t, ok)
				assert.Equal(t, 100, data.InputTokens)
				assert.Equal(t, 50, data.OutputTokens)
			},
		},
		{
			name:     "PipelineFailedData",
			dataType: "*events.PipelineFailedData",
			data:     `{}`,
			check: func(t *testing.T, result EventData) {
				_, ok := result.(*PipelineFailedData)
				require.True(t, ok)
			},
		},
		{
			name:     "ProviderCallStartedData",
			dataType: "*events.ProviderCallStartedData",
			data:     `{"Provider":"openai","Model":"gpt-4"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ProviderCallStartedData)
				require.True(t, ok)
				assert.Equal(t, "openai", data.Provider)
				assert.Equal(t, "gpt-4", data.Model)
			},
		},
		{
			name:     "ProviderCallFailedData",
			dataType: "*events.ProviderCallFailedData",
			data:     `{"Provider":"anthropic","Model":"claude-3"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ProviderCallFailedData)
				require.True(t, ok)
				assert.Equal(t, "anthropic", data.Provider)
				assert.Equal(t, "claude-3", data.Model)
			},
		},
		{
			name:     "ToolCallCompletedData",
			dataType: "*events.ToolCallCompletedData",
			data:     `{"ToolName":"calculator","CallID":"call-2","Status":"success"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ToolCallCompletedData)
				require.True(t, ok)
				assert.Equal(t, "calculator", data.ToolName)
				assert.Equal(t, "call-2", data.CallID)
				assert.Equal(t, "success", data.Status)
			},
		},
		{
			name:     "ToolCallFailedData",
			dataType: "*events.ToolCallFailedData",
			data:     `{"ToolName":"search","CallID":"call-3"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ToolCallFailedData)
				require.True(t, ok)
				assert.Equal(t, "search", data.ToolName)
				assert.Equal(t, "call-3", data.CallID)
			},
		},
		{
			name:     "CustomEventData",
			dataType: "*events.CustomEventData",
			data:     `{"MiddlewareName":"logger","EventName":"log.info","Message":"test message"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*CustomEventData)
				require.True(t, ok)
				assert.Equal(t, "logger", data.MiddlewareName)
				assert.Equal(t, "log.info", data.EventName)
				assert.Equal(t, "test message", data.Message)
			},
		},
		// Consolidated canonical type names
		{
			name:     "MiddlewareEventData canonical",
			dataType: "*events.MiddlewareEventData",
			data:     `{"Name":"auth","Index":0}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*MiddlewareEventData)
				require.True(t, ok)
				assert.Equal(t, "auth", data.Name)
			},
		},
		{
			name:     "StageEventData canonical",
			dataType: "*events.StageEventData",
			data:     `{"Name":"provider","Index":1,"StageType":"generate"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*StageEventData)
				require.True(t, ok)
				assert.Equal(t, "provider", data.Name)
				assert.Equal(t, "generate", data.StageType)
			},
		},
		{
			name:     "ToolCallEventData canonical",
			dataType: "*events.ToolCallEventData",
			data:     `{"ToolName":"search","CallID":"call-x"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ToolCallEventData)
				require.True(t, ok)
				assert.Equal(t, "search", data.ToolName)
			},
		},
		{
			name:     "ValidationEventData canonical",
			dataType: "*events.ValidationEventData",
			data:     `{"ValidatorName":"content_filter","ValidatorType":"output"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ValidationEventData)
				require.True(t, ok)
				assert.Equal(t, "content_filter", data.ValidatorName)
				assert.Equal(t, "output", data.ValidatorType)
			},
		},
		{
			name:     "StateEventData canonical",
			dataType: "*events.StateEventData",
			data:     `{"ConversationID":"conv-1","MessageCount":5}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*StateEventData)
				require.True(t, ok)
				assert.Equal(t, "conv-1", data.ConversationID)
				assert.Equal(t, 5, data.MessageCount)
			},
		},
		{
			name:     "AudioEventData canonical",
			dataType: "*events.AudioEventData",
			data:     `{"direction":"input","actor":"user","chunk_index":3}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*AudioEventData)
				require.True(t, ok)
				assert.Equal(t, "input", data.Direction)
				assert.Equal(t, "user", data.Actor)
				assert.Equal(t, 3, data.ChunkIndex)
			},
		},
		{
			name:     "ImageEventData canonical",
			dataType: "*events.ImageEventData",
			data:     `{"direction":"output","generated_from":"dalle"}`,
			check: func(t *testing.T, result EventData) {
				data, ok := result.(*ImageEventData)
				require.True(t, ok)
				assert.Equal(t, "output", data.Direction)
				assert.Equal(t, "dalle", data.GeneratedFrom)
			},
		},
		{
			name:     "unknown type returns nil",
			dataType: "*events.UnknownType",
			data:     `{"foo":"bar"}`,
			check: func(t *testing.T, result EventData) {
				assert.Nil(t, result)
			},
		},
		{
			name:     "invalid JSON returns nil",
			dataType: "*events.MessageCreatedData",
			data:     `{invalid json}`,
			check: func(t *testing.T, result EventData) {
				assert.Nil(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deserializeEventData(tt.dataType, json.RawMessage(tt.data))
			tt.check(t, result)
		})
	}
}

func TestFileEventStore_Close_AlreadyClosed(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)

	// First close
	err = store.Close()
	require.NoError(t, err)

	// Second close should also succeed
	err = store.Close()
	require.NoError(t, err)
}

func TestFileEventStore_Sync_NoFiles(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	// Sync with no files open should succeed
	err = store.Sync()
	require.NoError(t, err)
}

func TestFileEventStore_Query_AdvancedFilters(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	sessionID := "session-advanced-filter"
	baseTime := time.Now()

	// Create events with different properties
	events := []*Event{
		{Type: EventMessageCreated, Timestamp: baseTime, SessionID: sessionID, RunID: "run-1", ConversationID: "conv-1"},
		{Type: EventMessageCreated, Timestamp: baseTime.Add(time.Second), SessionID: sessionID, RunID: "run-2", ConversationID: "conv-1"},
		{Type: EventToolCallStarted, Timestamp: baseTime.Add(2 * time.Second), SessionID: sessionID, RunID: "run-1", ConversationID: "conv-2"},
	}

	for _, e := range events {
		require.NoError(t, store.Append(context.Background(), e))
	}
	require.NoError(t, store.Sync())

	t.Run("filter by RunID", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID: sessionID,
			RunID:     "run-1",
		})
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("filter by time range Since", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID: sessionID,
			Since:     baseTime.Add(500 * time.Millisecond),
		})
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("filter by time range Until", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID: sessionID,
			Until:     baseTime.Add(500 * time.Millisecond),
		})
		require.NoError(t, err)
		assert.Len(t, result, 1)
	})

	t.Run("combined filters", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID:      sessionID,
			RunID:          "run-1",
			ConversationID: "conv-1",
		})
		require.NoError(t, err)
		assert.Len(t, result, 1)
	})
}

func TestFileEventStore_Sync_WithFiles(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	// Write an event to create a file
	event := &Event{
		Type:      EventMessageCreated,
		Timestamp: time.Now(),
		SessionID: "session-sync",
	}
	require.NoError(t, store.Append(context.Background(), event))

	// Sync should succeed with open files
	err = store.Sync()
	require.NoError(t, err)
}

func TestFileEventStore_toSerializable_WithData(t *testing.T) {
	event := &Event{
		Type:           EventMessageCreated,
		Timestamp:      time.Now(),
		SessionID:      "test-session",
		ConversationID: "test-conv",
		RunID:          "test-run",
		Data: &MessageCreatedData{
			Role:    "user",
			Content: "Hello",
		},
	}

	se, err := toSerializable(event)
	require.NoError(t, err)
	assert.Equal(t, "*events.MessageCreatedData", se.DataType)
	assert.NotEmpty(t, se.Data)
}

func TestFileEventStore_toSerializable_NilData(t *testing.T) {
	event := &Event{
		Type:      EventPipelineStarted,
		Timestamp: time.Now(),
		SessionID: "test-session",
		Data:      nil,
	}

	se, err := toSerializable(event)
	require.NoError(t, err)
	assert.Empty(t, se.DataType)
	assert.Empty(t, se.Data)
}

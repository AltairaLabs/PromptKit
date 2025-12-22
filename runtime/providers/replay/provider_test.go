package replay

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/recording"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ensure imports are used
var (
	_ providers.Provider
	_ types.Message
)

func createTestRecording(t *testing.T, turns int) *recording.SessionRecording {
	t.Helper()

	baseTime := time.Now()
	var recEvents []recording.RecordedEvent

	for i := 0; i < turns; i++ {
		// User message
		userContent := "Hello " + string(rune('A'+i))
		userMsgData, _ := json.Marshal(events.MessageCreatedData{
			Role:    "user",
			Content: userContent,
		})
		recEvents = append(recEvents, recording.RecordedEvent{
			Sequence:  int64(i*3 + 1),
			Type:      events.EventMessageCreated,
			Timestamp: baseTime.Add(time.Duration(i*3) * time.Second),
			Offset:    time.Duration(i*3) * time.Second,
			SessionID: "test-session",
			Data:      userMsgData,
		})

		// Assistant message
		assistantContent := "Response " + string(rune('A'+i))
		assistantMsgData, _ := json.Marshal(events.MessageCreatedData{
			Role:    "assistant",
			Content: assistantContent,
		})
		recEvents = append(recEvents, recording.RecordedEvent{
			Sequence:  int64(i*3 + 2),
			Type:      events.EventMessageCreated,
			Timestamp: baseTime.Add(time.Duration(i*3+1) * time.Second),
			Offset:    time.Duration(i*3+1) * time.Second,
			SessionID: "test-session",
			Data:      assistantMsgData,
		})

		// Provider call completed (with cost info)
		callData, _ := json.Marshal(events.ProviderCallCompletedData{
			Duration:     100 * time.Millisecond,
			InputTokens:  10,
			OutputTokens: 20,
			Cost:         0.001,
		})
		recEvents = append(recEvents, recording.RecordedEvent{
			Sequence:  int64(i*3 + 3),
			Type:      events.EventProviderCallCompleted,
			Timestamp: baseTime.Add(time.Duration(i*3+2) * time.Second),
			Offset:    time.Duration(i*3+2) * time.Second,
			SessionID: "test-session",
			Data:      callData,
		})
	}

	return &recording.SessionRecording{
		Metadata: recording.Metadata{
			SessionID:  "test-session",
			StartTime:  baseTime,
			EndTime:    baseTime.Add(time.Duration(turns*3) * time.Second),
			Duration:   time.Duration(turns*3) * time.Second,
			EventCount: len(recEvents),
			Version:    "1.0",
		},
		Events: recEvents,
	}
}

func TestNewProvider(t *testing.T) {
	rec := createTestRecording(t, 3)

	t.Run("creates provider with default config", func(t *testing.T) {
		p, err := NewProvider(rec, nil)
		require.NoError(t, err)
		assert.NotNil(t, p)
		assert.Equal(t, "replay", p.ID())
		assert.Equal(t, 3, p.TurnCount())
	})

	t.Run("creates provider with custom config", func(t *testing.T) {
		cfg := &Config{
			Timing: TimingRealTime,
			Speed:  4.0,
		}
		p, err := NewProvider(rec, cfg)
		require.NoError(t, err)
		assert.Equal(t, TimingRealTime, p.config.Timing)
		assert.Equal(t, 4.0, p.config.Speed)
	})

	t.Run("fails with nil recording", func(t *testing.T) {
		_, err := NewProvider(nil, nil)
		require.Error(t, err)
	})

	t.Run("fails with empty recording", func(t *testing.T) {
		emptyRec := &recording.SessionRecording{
			Metadata: recording.Metadata{Version: "1.0"},
		}
		_, err := NewProvider(emptyRec, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no assistant responses")
	})
}

func TestProvider_Predict(t *testing.T) {
	rec := createTestRecording(t, 3)
	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	t.Run("returns recorded responses in order", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			resp, err := p.Predict(context.Background(), providers.PredictionRequest{})
			require.NoError(t, err)
			assert.Equal(t, "Response "+string(rune('A'+i)), resp.Content)
		}
	})

	t.Run("returns error when exhausted", func(t *testing.T) {
		// Provider already at turn 3, one more should fail
		_, err := p.Predict(context.Background(), providers.PredictionRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "replay exhausted")
	})
}

func TestProvider_Reset(t *testing.T) {
	rec := createTestRecording(t, 3)
	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	// Consume some turns
	_, _ = p.Predict(context.Background(), providers.PredictionRequest{})
	_, _ = p.Predict(context.Background(), providers.PredictionRequest{})
	assert.Equal(t, 2, p.CurrentTurn())

	// Reset
	p.Reset()
	assert.Equal(t, 0, p.CurrentTurn())

	// Should replay from beginning
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{})
	require.NoError(t, err)
	assert.Equal(t, "Response A", resp.Content)
}

func TestProvider_PredictStream(t *testing.T) {
	rec := createTestRecording(t, 2)
	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{})
	require.NoError(t, err)

	var chunks []providers.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	require.Len(t, chunks, 1)
	assert.Equal(t, "Response A", chunks[0].Content)
	assert.NotNil(t, chunks[0].FinalResult)
	assert.Equal(t, "stop", *chunks[0].FinishReason)
}

func TestProvider_MatchByContent(t *testing.T) {
	rec := createTestRecording(t, 3)
	cfg := &Config{MatchMode: MatchByContent}
	p, err := NewProvider(rec, cfg)
	require.NoError(t, err)

	// Request with specific content should get matching response
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Hello B"},
		},
	}

	resp, err := p.Predict(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "Response B", resp.Content)
}

func TestProvider_SupportsStreaming(t *testing.T) {
	rec := createTestRecording(t, 1)
	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	assert.True(t, p.SupportsStreaming())
}

func TestProvider_ShouldIncludeRawOutput(t *testing.T) {
	rec := createTestRecording(t, 1)
	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	assert.False(t, p.ShouldIncludeRawOutput())
}

func TestProvider_CalculateCost(t *testing.T) {
	rec := createTestRecording(t, 1)
	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	cost := p.CalculateCost(100, 50, 10)
	assert.Equal(t, 100, cost.InputTokens)
	assert.Equal(t, 50, cost.OutputTokens)
	assert.Equal(t, 10, cost.CachedTokens)
	assert.Equal(t, 0.0, cost.TotalCost) // Replays are free
}

func TestProvider_Close(t *testing.T) {
	rec := createTestRecording(t, 1)
	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	err = p.Close()
	assert.NoError(t, err)
}

func TestProvider_TimingInstant(t *testing.T) {
	rec := createTestRecording(t, 2)
	cfg := &Config{Timing: TimingInstant}
	p, err := NewProvider(rec, cfg)
	require.NoError(t, err)

	start := time.Now()
	_, _ = p.Predict(context.Background(), providers.PredictionRequest{})
	_, _ = p.Predict(context.Background(), providers.PredictionRequest{})
	elapsed := time.Since(start)

	// Should be nearly instant (< 100ms for both)
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func TestProvider_ContextCancellation(t *testing.T) {
	rec := createTestRecording(t, 2)
	cfg := &Config{Timing: TimingRealTime}
	p, err := NewProvider(rec, cfg)
	require.NoError(t, err)

	// First turn to set up timing
	_, _ = p.Predict(context.Background(), providers.PredictionRequest{})

	// Cancel context during second turn
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = p.Predict(ctx, providers.PredictionRequest{})
	// Should return quickly due to context cancellation
	assert.Error(t, err)
}

func TestProvider_CostInfoPreserved(t *testing.T) {
	rec := createTestRecording(t, 1)
	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	resp, err := p.Predict(context.Background(), providers.PredictionRequest{})
	require.NoError(t, err)

	// Cost info should be preserved from recording
	require.NotNil(t, resp.CostInfo)
	assert.Equal(t, 10, resp.CostInfo.InputTokens)
	assert.Equal(t, 20, resp.CostInfo.OutputTokens)
	assert.Equal(t, 0.001, resp.CostInfo.TotalCost)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, TimingInstant, cfg.Timing)
	assert.Equal(t, 2.0, cfg.Speed)
	assert.Equal(t, MatchByTurn, cfg.MatchMode)
}

func TestProvider_TimingAccelerated(t *testing.T) {
	rec := createTestRecording(t, 2)
	cfg := &Config{Timing: TimingAccelerated, Speed: 10.0} // 10x speed
	p, err := NewProvider(rec, cfg)
	require.NoError(t, err)

	start := time.Now()
	_, _ = p.Predict(context.Background(), providers.PredictionRequest{})
	_, _ = p.Predict(context.Background(), providers.PredictionRequest{})
	elapsed := time.Since(start)

	// Should be much faster than real-time
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestProvider_EstimateCost(t *testing.T) {
	// Create a recording without cost info to trigger estimation
	baseTime := time.Now()
	assistantMsgData, _ := json.Marshal(events.MessageCreatedData{
		Role:    "assistant",
		Content: "This is a test response with some content",
	})
	rec := &recording.SessionRecording{
		Metadata: recording.Metadata{
			SessionID: "test-session",
			StartTime: baseTime,
			EndTime:   baseTime.Add(time.Second),
			Duration:  time.Second,
			Version:   "1.0",
		},
		Events: []recording.RecordedEvent{
			{
				Sequence:  1,
				Type:      events.EventMessageCreated,
				Timestamp: baseTime,
				SessionID: "test-session",
				Data:      assistantMsgData,
			},
		},
	}

	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	resp, err := p.Predict(context.Background(), providers.PredictionRequest{})
	require.NoError(t, err)

	// Cost should be estimated (zero cost but non-zero output tokens)
	require.NotNil(t, resp.CostInfo)
	assert.Equal(t, 0.0, resp.CostInfo.TotalCost)
	assert.Greater(t, resp.CostInfo.OutputTokens, 0)
}

func TestNewProviderFromFile(t *testing.T) {
	// Create a test recording and save it
	rec := createTestRecording(t, 2)
	path := filepath.Join(t.TempDir(), "test.recording.json")
	require.NoError(t, rec.SaveTo(path, recording.FormatJSON))

	t.Run("loads from valid file", func(t *testing.T) {
		p, err := NewProviderFromFile(path, nil)
		require.NoError(t, err)
		assert.Equal(t, 2, p.TurnCount())
	})

	t.Run("fails with invalid path", func(t *testing.T) {
		_, err := NewProviderFromFile("/nonexistent/path.json", nil)
		require.Error(t, err)
	})
}

func TestProvider_ToolCallsPreserved(t *testing.T) {
	// Create recording with tool calls
	baseTime := time.Now()
	assistantMsgData, _ := json.Marshal(events.MessageCreatedData{
		Role:    "assistant",
		Content: "Let me check that",
		ToolCalls: []events.MessageToolCall{
			{ID: "call_1", Name: "get_weather", Args: `{"location":"SF"}`},
		},
	})
	rec := &recording.SessionRecording{
		Metadata: recording.Metadata{
			SessionID: "test-session",
			StartTime: baseTime,
			EndTime:   baseTime.Add(time.Second),
			Duration:  time.Second,
			Version:   "1.0",
		},
		Events: []recording.RecordedEvent{
			{
				Sequence:  1,
				Type:      events.EventMessageCreated,
				Timestamp: baseTime,
				SessionID: "test-session",
				Data:      assistantMsgData,
			},
		},
	}

	p, err := NewProvider(rec, nil)
	require.NoError(t, err)

	resp, err := p.Predict(context.Background(), providers.PredictionRequest{})
	require.NoError(t, err)

	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "call_1", resp.ToolCalls[0].ID)
	assert.Equal(t, "get_weather", resp.ToolCalls[0].Name)
}

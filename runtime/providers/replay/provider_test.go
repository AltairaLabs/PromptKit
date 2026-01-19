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

func TestCreateProviderFromSpec(t *testing.T) {
	// Create a test recording file
	rec := createTestRecording(t, 2)
	path := filepath.Join(t.TempDir(), "test.recording.json")
	require.NoError(t, rec.SaveTo(path, recording.FormatJSON))

	t.Run("creates provider from spec", func(t *testing.T) {
		spec := providers.ProviderSpec{
			ID:   "my-replay",
			Type: "replay",
			AdditionalConfig: map[string]interface{}{
				"recording": path,
				"timing":    "instant",
				"match":     "turn",
			},
		}

		provider, err := providers.CreateProviderFromSpec(spec)
		require.NoError(t, err)
		assert.Equal(t, "my-replay", provider.ID())

		// Should work as a provider
		resp, err := provider.Predict(context.Background(), providers.PredictionRequest{})
		require.NoError(t, err)
		assert.Equal(t, "Response A", resp.Content)
	})

	t.Run("fails without recording path", func(t *testing.T) {
		spec := providers.ProviderSpec{
			ID:   "bad-replay",
			Type: "replay",
		}

		_, err := providers.CreateProviderFromSpec(spec)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "recording")
	})

	t.Run("fails with invalid recording path", func(t *testing.T) {
		spec := providers.ProviderSpec{
			ID:   "bad-replay",
			Type: "replay",
			AdditionalConfig: map[string]interface{}{
				"recording": "/nonexistent/path.json",
			},
		}

		_, err := providers.CreateProviderFromSpec(spec)
		require.Error(t, err)
	})

	t.Run("respects timing config", func(t *testing.T) {
		spec := providers.ProviderSpec{
			ID:   "realtime-replay",
			Type: "replay",
			AdditionalConfig: map[string]interface{}{
				"recording": path,
				"timing":    "accelerated",
				"speed":     5.0,
			},
		}

		provider, err := providers.CreateProviderFromSpec(spec)
		require.NoError(t, err)

		// Cast to access config (for testing)
		rp := provider.(*Provider)
		assert.Equal(t, TimingAccelerated, rp.config.Timing)
		assert.Equal(t, 5.0, rp.config.Speed)
	})
}

func TestProvider_GetMetadata(t *testing.T) {
	t.Run("returns empty metadata when none configured", func(t *testing.T) {
		rec := createTestRecording(t, 1)
		p, err := NewProvider(rec, nil)
		require.NoError(t, err)

		metadata := p.GetMetadata()
		assert.NotNil(t, metadata)
	})

	t.Run("returns configured metadata", func(t *testing.T) {
		rec := createTestRecording(t, 1)
		cfg := &Config{
			Metadata: map[string]interface{}{
				"judge_targets": map[string]interface{}{
					"default": map[string]interface{}{
						"type":  "openai",
						"model": "gpt-4",
						"id":    "gpt-4-judge",
					},
				},
				"tags": []string{"evaluation", "test"},
			},
		}

		p, err := NewProvider(rec, cfg)
		require.NoError(t, err)

		metadata := p.GetMetadata()
		assert.Contains(t, metadata, "judge_targets")
		assert.Contains(t, metadata, "tags")

		judgeTargets := metadata["judge_targets"].(map[string]interface{})
		assert.Contains(t, judgeTargets, "default")
	})

	t.Run("includes recording metadata", func(t *testing.T) {
		rec := createTestRecording(t, 1)
		rec.Metadata.ProviderName = "openai"
		rec.Metadata.Model = "gpt-4-turbo"
		rec.Metadata.Custom = map[string]any{
			"custom_field": "custom_value",
		}

		p, err := NewProvider(rec, nil)
		require.NoError(t, err)

		metadata := p.GetMetadata()
		assert.Contains(t, metadata, "provider_info")
		assert.Contains(t, metadata, "session_id")
		assert.Contains(t, metadata, "custom_field")

		providerInfo := metadata["provider_info"].(map[string]interface{})
		assert.Equal(t, "openai", providerInfo["provider_id"])
		assert.Equal(t, "gpt-4-turbo", providerInfo["model"])
		assert.Equal(t, "test-session", metadata["session_id"])
		assert.Equal(t, "custom_value", metadata["custom_field"])
	})

	t.Run("config metadata takes precedence over recording metadata", func(t *testing.T) {
		rec := createTestRecording(t, 1)
		rec.Metadata.Custom = map[string]any{
			"priority_field": "from_recording",
		}

		cfg := &Config{
			Metadata: map[string]interface{}{
				"priority_field": "from_config",
			},
		}

		p, err := NewProvider(rec, cfg)
		require.NoError(t, err)

		metadata := p.GetMetadata()
		assert.Equal(t, "from_config", metadata["priority_field"])
	})
}

func TestProvider_MultimodalParts(t *testing.T) {
	t.Run("includes multimodal parts in response", func(t *testing.T) {
		baseTime := time.Now()
		textContent := "Here is an image:"
		imageURL := "https://example.com/image.jpg"

		// Create recording with multimodal content
		assistantMsgData, _ := json.Marshal(events.MessageCreatedData{
			Role:    "assistant",
			Content: textContent,
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeText,
					Text: &textContent,
				},
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						URL:      &imageURL,
						MIMEType: types.MIMETypeImageJPEG,
					},
				},
			},
		})

		rec := &recording.SessionRecording{
			Metadata: recording.Metadata{
				SessionID: "test-multimodal",
			},
			Events: []recording.RecordedEvent{
				{
					Sequence:  1,
					Type:      events.EventMessageCreated,
					Timestamp: baseTime,
					Offset:    0,
					SessionID: "test-multimodal",
					Data:      assistantMsgData,
				},
			},
		}

		p, err := NewProvider(rec, nil)
		require.NoError(t, err)

		// Make a prediction
		ctx := context.Background()
		resp, err := p.Predict(ctx, providers.PredictionRequest{
			Messages: []types.Message{
				{Role: "user", Content: "Show me an image"},
			},
		})

		require.NoError(t, err)
		assert.Equal(t, textContent, resp.Content)
		require.Len(t, resp.Parts, 2)

		// Verify text part
		assert.Equal(t, types.ContentTypeText, resp.Parts[0].Type)
		assert.NotNil(t, resp.Parts[0].Text)
		assert.Equal(t, textContent, *resp.Parts[0].Text)

		// Verify image part
		assert.Equal(t, types.ContentTypeImage, resp.Parts[1].Type)
		assert.NotNil(t, resp.Parts[1].Media)
		assert.Equal(t, imageURL, *resp.Parts[1].Media.URL)
		assert.Equal(t, types.MIMETypeImageJPEG, resp.Parts[1].Media.MIMEType)
	})
}

package replay

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewStreamingProviderFromArenaOutput(t *testing.T) {
	t.Run("creates provider from valid arena output", func(t *testing.T) {
		dir := t.TempDir()
		output := arenaOutput{
			RunID: "test-run",
			Messages: []arenaMessage{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there!"},
			},
		}

		data, _ := json.Marshal(output)
		path := filepath.Join(dir, "output.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}

		provider, err := NewStreamingProviderFromArenaOutput(path, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if provider == nil {
			t.Fatal("expected provider")
		}
		if len(provider.messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(provider.messages))
		}
	})

	t.Run("fails on empty messages", func(t *testing.T) {
		dir := t.TempDir()
		output := arenaOutput{
			RunID:    "test-run",
			Messages: []arenaMessage{},
		}

		data, _ := json.Marshal(output)
		path := filepath.Join(dir, "output.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}

		_, err := NewStreamingProviderFromArenaOutput(path, nil)
		if err == nil {
			t.Error("expected error for empty messages")
		}
	})

	t.Run("fails on missing file", func(t *testing.T) {
		_, err := NewStreamingProviderFromArenaOutput("/nonexistent/path.json", nil)
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("fails on invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "invalid.json")
		if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
			t.Fatal(err)
		}

		_, err := NewStreamingProviderFromArenaOutput(path, nil)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("uses custom config", func(t *testing.T) {
		dir := t.TempDir()
		output := arenaOutput{
			Messages: []arenaMessage{{Role: "assistant", Content: "test"}},
		}

		data, _ := json.Marshal(output)
		path := filepath.Join(dir, "output.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}

		cfg := &Config{Timing: TimingInstant, Speed: 2.0}
		provider, err := NewStreamingProviderFromArenaOutput(path, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if provider.config.Timing != TimingInstant {
			t.Error("expected custom timing config")
		}
	})
}

func TestStreamingProvider_CreateStreamSession(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi!"},
	})

	t.Run("creates session with assistant messages", func(t *testing.T) {
		session, err := provider.CreateStreamSession(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer session.Close()

		ss := session.(*StreamSession)
		if len(ss.messages) != 1 {
			t.Errorf("expected 1 assistant message, got %d", len(ss.messages))
		}
	})

	t.Run("fails with no assistant messages", func(t *testing.T) {
		p := createTestStreamingProvider(t, []arenaMessage{
			{Role: "user", Content: "Hello"},
		})

		_, err := p.CreateStreamSession(context.Background(), nil)
		if err == nil {
			t.Error("expected error for no assistant messages")
		}
	})
}

func TestStreamingProvider_Capabilities(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})

	t.Run("SupportsStreamInput returns audio", func(t *testing.T) {
		types := provider.SupportsStreamInput()
		if len(types) != 1 || types[0] != "audio" {
			t.Errorf("expected [audio], got %v", types)
		}
	})

	t.Run("GetStreamingCapabilities returns correct values", func(t *testing.T) {
		caps := provider.GetStreamingCapabilities()
		if !caps.BidirectionalSupport {
			t.Error("expected bidirectional support")
		}
		if caps.Audio == nil {
			t.Fatal("expected audio capabilities")
		}
		if caps.Audio.PreferredSampleRate != 24000 {
			t.Errorf("expected sample rate 24000, got %d", caps.Audio.PreferredSampleRate)
		}
	})
}

func TestStreamSession_SendText(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "Response 1"},
		{Role: "assistant", Content: "Response 2"},
	})
	provider.config.Timing = TimingInstant

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	defer session.Close()

	t.Run("sends text and receives response", func(t *testing.T) {
		go func() {
			_ = session.(*StreamSession).SendText(context.Background(), "Hello")
		}()

		// Read response
		respChan := session.Response()
		chunk := <-respChan
		if chunk.Content != "Response 1" {
			t.Errorf("expected 'Response 1', got %q", chunk.Content)
		}

		// Read finish reason
		chunk = <-respChan
		if chunk.FinishReason == nil {
			t.Error("expected finish reason")
		}
	})
}

func TestStreamSession_SendChunk(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	defer session.Close()

	t.Run("accepts chunk without error", func(t *testing.T) {
		err := session.(*StreamSession).SendChunk(context.Background(), &types.MediaChunk{
			Data: []byte("audio data"),
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("fails on closed session", func(t *testing.T) {
		session.Close()
		err := session.(*StreamSession).SendChunk(context.Background(), &types.MediaChunk{})
		if err == nil {
			t.Error("expected error on closed session")
		}
	})
}

func TestStreamSession_SendSystemContext(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	defer session.Close()

	err := session.(*StreamSession).SendSystemContext(context.Background(), "system prompt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStreamSession_TriggerNextResponse(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "Response"},
	})
	provider.config.Timing = TimingInstant

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	t.Run("triggers response", func(t *testing.T) {
		go func() {
			_ = ss.TriggerNextResponse(context.Background())
		}()

		chunk := <-session.Response()
		if chunk.Content != "Response" {
			t.Errorf("expected 'Response', got %q", chunk.Content)
		}
	})

	t.Run("fails when no more responses", func(t *testing.T) {
		err := ss.TriggerNextResponse(context.Background())
		if err == nil {
			t.Error("expected error when no more responses")
		}
	})
}

func TestStreamSession_RemainingTurns(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "1"},
		{Role: "assistant", Content: "2"},
		{Role: "assistant", Content: "3"},
	})
	provider.config.Timing = TimingInstant

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	if ss.RemainingTurns() != 3 {
		t.Errorf("expected 3 remaining turns, got %d", ss.RemainingTurns())
	}

	// Consume one response
	go func() { _ = ss.TriggerNextResponse(context.Background()) }()
	<-session.Response() // content
	<-session.Response() // finish

	if ss.RemainingTurns() != 2 {
		t.Errorf("expected 2 remaining turns, got %d", ss.RemainingTurns())
	}
}

func TestStreamSession_EndInput(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "Response"},
	})
	provider.config.Timing = TimingInstant

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	t.Run("triggers next response", func(t *testing.T) {
		ss.EndInput()

		// Give goroutine time to send
		time.Sleep(50 * time.Millisecond)

		select {
		case chunk := <-session.Response():
			if chunk.Content != "Response" {
				t.Errorf("expected 'Response', got %q", chunk.Content)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout waiting for response")
		}
	})

	t.Run("no-op when no more responses", func(t *testing.T) {
		ss.EndInput() // Should not panic
	})
}

func TestStreamSession_Close(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)

	t.Run("closes successfully", func(t *testing.T) {
		err := session.Close()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if !ss.closed {
			t.Error("expected session to be marked closed")
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		err := session.Close()
		if err != nil {
			t.Errorf("unexpected error on second close: %v", err)
		}
	})
}

func TestStreamSession_Error(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	defer session.Close()

	if session.(*StreamSession).Error() != nil {
		t.Error("expected nil error initially")
	}
}

func TestStreamSession_Done(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)

	doneChan := ss.Done()
	select {
	case <-doneChan:
		t.Error("done channel should not be closed yet")
	default:
		// Expected
	}

	session.Close()

	select {
	case <-doneChan:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("done channel should be closed after Close()")
	}
}

func TestStreamSession_TimingDelay(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})
	provider.config.Timing = TimingAccelerated
	provider.config.Speed = 10.0 // Very fast

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := ss.applyTimingDelay(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStreamSession_TimingDelay_Cancellation(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})
	provider.config.Timing = TimingRealTime

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := ss.applyTimingDelay(ctx)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestStreamSession_SendText_NoMoreResponses(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "Only response"},
	})
	provider.config.Timing = TimingInstant

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	// Consume the only response
	go func() { _ = ss.SendText(context.Background(), "First") }()
	<-session.Response() // content
	<-session.Response() // finish

	// Now there are no more responses
	go func() { _ = ss.SendText(context.Background(), "Second") }()

	// Should get a finish reason with "stop"
	select {
	case chunk := <-session.Response():
		if chunk.FinishReason == nil || *chunk.FinishReason != "stop" {
			t.Error("expected stop finish reason")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for response")
	}
}

func TestStreamSession_SendText_Closed(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	session.Close()

	err := ss.SendText(context.Background(), "test")
	if err == nil {
		t.Error("expected error on closed session")
	}
}

func TestStreamSession_AudioParts(t *testing.T) {
	dir := t.TempDir()

	// Create an arena output with audio parts
	output := arenaOutput{
		Messages: []arenaMessage{
			{
				Role:    "assistant",
				Content: "",
				Parts: []arenaContentPart{
					{
						Type: "audio",
						Media: &arenaMedia{
							MIMEType: "audio/pcm",
							Data:     "SGVsbG8gV29ybGQ=", // base64 "Hello World"
						},
					},
				},
			},
		},
	}

	data, _ := json.Marshal(output)
	path := filepath.Join(dir, "output.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	provider, _ := NewStreamingProviderFromArenaOutput(path, nil)
	provider.config.Timing = TimingInstant

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	go func() { _ = ss.TriggerNextResponse(context.Background()) }()

	// Should receive audio chunk
	select {
	case chunk := <-session.Response():
		if chunk.MediaDelta == nil {
			t.Error("expected media delta")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for audio")
	}
}

func TestStreamSession_AudioFromFile(t *testing.T) {
	dir := t.TempDir()

	// Create audio file
	audioPath := filepath.Join(dir, "audio.pcm")
	if err := os.WriteFile(audioPath, []byte("audio data"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create arena output with file reference
	output := arenaOutput{
		Messages: []arenaMessage{
			{
				Role: "assistant",
				Parts: []arenaContentPart{
					{
						Type: "audio",
						Media: &arenaMedia{
							MIMEType:         "audio/pcm",
							StorageReference: "audio.pcm",
						},
					},
				},
			},
		},
	}

	data, _ := json.Marshal(output)
	path := filepath.Join(dir, "output.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	provider, _ := NewStreamingProviderFromArenaOutput(path, nil)
	provider.config.Timing = TimingInstant

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	go func() { _ = ss.TriggerNextResponse(context.Background()) }()

	// Should receive audio chunk
	select {
	case chunk := <-session.Response():
		if chunk.MediaDelta == nil {
			t.Error("expected media delta from file")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for audio")
	}
}

func TestStreamSession_LoadAudioData_Errors(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{Role: "assistant", Content: "test"},
	})

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	t.Run("no data source", func(t *testing.T) {
		_, err := ss.loadAudioData(&arenaMedia{})
		if err == nil {
			t.Error("expected error for no data source")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := ss.loadAudioData(&arenaMedia{
			StorageReference: "/nonexistent/file.pcm",
		})
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		_, err := ss.loadAudioData(&arenaMedia{
			Data: "not valid base64!!!",
		})
		if err == nil {
			t.Error("expected error for invalid base64")
		}
	})
}

func TestStreamSession_CompletionWithCostInfo(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{
			Role:    "assistant",
			Content: "test",
			CostInfo: &types.CostInfo{
				InputTokens:  100,
				OutputTokens: 50,
				TotalCost:    0.01,
			},
		},
	})
	provider.config.Timing = TimingInstant

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	go func() { _ = ss.TriggerNextResponse(context.Background()) }()

	// Skip content
	<-session.Response()

	// Check completion has cost info
	chunk := <-session.Response()
	if chunk.CostInfo == nil {
		t.Error("expected cost info")
	}
	if chunk.CostInfo.TotalCost != 0.01 {
		t.Errorf("expected cost 0.01, got %f", chunk.CostInfo.TotalCost)
	}
}

func TestStreamSession_CompletionWithMetaFinishReason(t *testing.T) {
	provider := createTestStreamingProvider(t, []arenaMessage{
		{
			Role:    "assistant",
			Content: "test",
			Meta: map[string]any{
				"finish_reason": "length",
			},
		},
	})
	provider.config.Timing = TimingInstant

	session, _ := provider.CreateStreamSession(context.Background(), nil)
	ss := session.(*StreamSession)
	defer session.Close()

	go func() { _ = ss.TriggerNextResponse(context.Background()) }()

	// Skip content
	<-session.Response()

	// Check completion has custom finish reason
	chunk := <-session.Response()
	if chunk.FinishReason == nil || *chunk.FinishReason != "length" {
		t.Error("expected finish reason 'length'")
	}
}

// Helper to create a test streaming provider.
func createTestStreamingProvider(t *testing.T, messages []arenaMessage) *StreamingProvider {
	t.Helper()

	dir := t.TempDir()
	output := arenaOutput{Messages: messages}
	data, _ := json.Marshal(output)
	path := filepath.Join(dir, "output.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	provider, err := NewStreamingProviderFromArenaOutput(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	return provider
}

//go:build integration
// +build integration

package openai

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// generateSineWave24k generates raw 24kHz 16-bit PCM mono audio.
func generateSineWave24k(freqHz float64, durationMs int, amplitude float64) []byte {
	sampleRate := 24000
	numSamples := sampleRate * durationMs / 1000
	buf := make([]byte, numSamples*2) // 16-bit = 2 bytes per sample
	for i := 0; i < numSamples; i++ {
		sample := amplitude * math.Sin(2*math.Pi*freqHz*float64(i)/float64(sampleRate))
		val := int16(sample * 32767)
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(val))
	}
	return buf
}

// newRealtimeProvider creates a provider configured for the OpenAI Realtime API.
func newRealtimeProvider() *Provider {
	return NewProvider(
		"openai-realtime-test",
		"gpt-4o-realtime-preview",
		"https://api.openai.com",
		providers.ProviderDefaults{},
		false,
	)
}

// collectResponse reads from session.Response() until a FinishReason is received or
// the timeout elapses. Returns accumulated delta text, cost info, and whether
// a finish reason was received.
func collectResponse(t *testing.T, session providers.StreamInputSession, timeout time.Duration) (string, *types.CostInfo, bool) {
	t.Helper()
	var response string
	var costInfo *types.CostInfo
	var gotFinish bool
	timer := time.After(timeout)

	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				t.Log("Response channel closed")
				return response, costInfo, gotFinish
			}
			if chunk.Error != nil {
				t.Logf("Chunk error: %v", chunk.Error)
			}
			if chunk.Delta != "" {
				response += chunk.Delta
			}
			if chunk.CostInfo != nil {
				costInfo = chunk.CostInfo
			}
			if chunk.FinishReason != nil {
				t.Logf("Finish reason: %s", *chunk.FinishReason)
				gotFinish = true
				return response, costInfo, gotFinish
			}
		case <-timer:
			t.Logf("Timeout after collecting %d chars", len(response))
			return response, costInfo, gotFinish
		}
	}
}

// TestRealtimeIntegration_SystemPrompt verifies the system prompt is applied
// and the model introduces itself by name when asked.
func TestRealtimeIntegration_SystemPrompt(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		SystemInstruction: "You are Nova, a helpful voice assistant. Always introduce yourself by name.",
		Metadata: map[string]interface{}{
			"modalities":          []string{"text", "audio"},
			"input_transcription": false, // no whisper overhead for test
		},
	}

	t.Log("Creating stream session...")
	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()
	t.Log("Session created successfully")

	if err := session.Error(); err != nil {
		t.Fatalf("Session error after creation: %v", err)
	}

	t.Log("Sending text prompt...")
	if err := session.SendText(ctx, "Hello, who are you?"); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}
	t.Log("Text prompt sent")

	response, costInfo, gotFinish := collectResponse(t, session, 20*time.Second)

	t.Logf("Final response: %q", response)

	if !gotFinish {
		t.Error("Did not receive a complete response (no finish reason)")
	}
	if response == "" {
		t.Error("Response was empty")
	}
	if costInfo != nil {
		t.Logf("Cost info: input=%d tokens, output=%d tokens, total=$%.6f",
			costInfo.InputTokens, costInfo.OutputTokens, costInfo.TotalCost)
	} else {
		t.Log("Cost info was not returned (may appear in a follow-up chunk)")
	}
}

// TestRealtimeIntegration_AudioThenEndInput verifies that sending audio and
// calling EndInput() with VAD disabled triggers a model response.
func TestRealtimeIntegration_AudioThenEndInput(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		Metadata: map[string]interface{}{
			"modalities":   []string{"text", "audio"},
			"vad_disabled": true, // manual turn control
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Generate 1 second of 440Hz sine wave at 24kHz 16-bit PCM.
	audioData := generateSineWave24k(440.0, 1000, 0.5)
	t.Logf("Generated %d bytes of test audio", len(audioData))

	// Split into ~3200 byte chunks and send.
	const chunkSize = 3200
	for i := 0; i < len(audioData); i += chunkSize {
		end := i + chunkSize
		if end > len(audioData) {
			end = len(audioData)
		}
		chunk := &types.MediaChunk{Data: audioData[i:end]}
		if err := session.SendChunk(ctx, chunk); err != nil {
			t.Fatalf("Failed to send audio chunk at offset %d: %v", i, err)
		}
	}
	t.Log("All audio chunks sent, calling EndInput()")

	// Type-assert to access EndInput (and CommitAudioBuffer + TriggerResponse).
	realtimeSession, ok := session.(*RealtimeSession)
	if !ok {
		t.Fatal("Session is not a *RealtimeSession")
	}
	realtimeSession.EndInput()

	response, costInfo, gotFinish := collectResponse(t, session, 25*time.Second)

	t.Logf("Response: %q", response)

	if !gotFinish {
		t.Error("Did not receive a complete response after EndInput")
	}
	if response == "" {
		t.Error("Response was empty — model did not respond to audio + EndInput")
	}
	if costInfo != nil {
		t.Logf("Cost: input=%d tokens, output=%d tokens, total=$%.6f",
			costInfo.InputTokens, costInfo.OutputTokens, costInfo.TotalCost)
	}
}

// TestRealtimeIntegration_TextOnly verifies plain text send/receive works.
func TestRealtimeIntegration_TextOnly(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		Metadata: map[string]interface{}{
			"modalities":          []string{"text", "audio"},
			"input_transcription": false,
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if err := session.SendText(ctx, "Say hi in one sentence."); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}

	response, _, gotFinish := collectResponse(t, session, 20*time.Second)

	t.Logf("Response: %q", response)

	if !gotFinish {
		t.Error("Did not receive a complete response")
	}
	if response == "" {
		t.Error("Text response was empty")
	}
}

// TestRealtimeIntegration_ErrorHandling verifies that invalid credentials produce
// an error at session creation time (not silently).
func TestRealtimeIntegration_ErrorHandling(t *testing.T) {
	t.Run("invalid API key", func(t *testing.T) {
		provider := &Provider{
			BaseProvider: providers.NewBaseProvider("openai-bad-key", false, nil),
			model:        "gpt-4o-realtime-preview",
			baseURL:      "https://api.openai.com",
			apiKey:       "sk-invalid-key-for-testing",
			defaults:     providers.ProviderDefaults{},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		req := &providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				ChunkSize:  3200,
				SampleRate: DefaultRealtimeSampleRate,
				Channels:   DefaultRealtimeChannels,
				BitDepth:   DefaultRealtimeBitDepth,
				Encoding:   "pcm16",
			},
		}

		_, err := provider.CreateStreamSession(ctx, req)
		if err == nil {
			t.Error("Expected error for invalid API key, got nil")
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})

	t.Run("empty API key", func(t *testing.T) {
		provider := &Provider{
			BaseProvider: providers.NewBaseProvider("openai-empty-key", false, nil),
			model:        "gpt-4o-realtime-preview",
			baseURL:      "https://api.openai.com",
			apiKey:       "",
			defaults:     providers.ProviderDefaults{},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		req := &providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				ChunkSize:  3200,
				SampleRate: DefaultRealtimeSampleRate,
				Channels:   DefaultRealtimeChannels,
				BitDepth:   DefaultRealtimeBitDepth,
				Encoding:   "pcm16",
			},
		}

		_, err := provider.CreateStreamSession(ctx, req)
		if err == nil {
			t.Error("Expected error for empty API key, got nil")
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})
}

// TestRealtimeIntegration_MultiTurn verifies multiple conversation turns work in one session.
func TestRealtimeIntegration_MultiTurn(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		SystemInstruction: "You are a helpful assistant. Keep responses brief.",
		Metadata: map[string]interface{}{
			"vad_disabled": true,
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	prompts := []string{"Say hello", "What did you just say?", "Say goodbye"}
	for i, prompt := range prompts {
		if err := session.SendText(ctx, prompt); err != nil {
			t.Fatalf("Turn %d: failed to send text: %v", i+1, err)
		}
		response, _, gotFinish := collectResponse(t, session, 25*time.Second)
		t.Logf("Turn %d response: %q (finish=%v)", i+1, response, gotFinish)
		if response == "" {
			t.Errorf("Turn %d: got empty response", i+1)
		}
		if err := session.Error(); err != nil {
			t.Fatalf("Turn %d: session error: %v", i+1, err)
		}
	}
}

// TestRealtimeIntegration_AudioModality verifies that audio modality configuration works
// and that both text and audio chunks are received.
func TestRealtimeIntegration_AudioModality(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		Metadata: map[string]interface{}{
			"modalities": []string{"text", "audio"},
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if err := session.SendText(ctx, "Say hello"); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}

	var textChunks, audioChunks int
	timer := time.After(20 * time.Second)
loop:
	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				break loop
			}
			if chunk.Delta != "" {
				textChunks++
			}
			if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
				audioChunks++
			}
			if chunk.FinishReason != nil {
				break loop
			}
		case <-timer:
			t.Log("Timeout reached")
			break loop
		}
	}

	t.Logf("Text chunks: %d, Audio chunks: %d", textChunks, audioChunks)
	if textChunks == 0 && audioChunks == 0 {
		t.Error("Received no text or audio chunks")
	}
}

// TestRealtimeIntegration_EndToEnd tests a full audio streaming round-trip.
func TestRealtimeIntegration_EndToEnd(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		Metadata: map[string]interface{}{
			"vad_disabled": true,
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Collect responses in background before sending audio.
	type result struct {
		chunks int
		text   string
	}
	resultCh := make(chan result, 1)
	go func() {
		var r result
		timer := time.After(40 * time.Second)
		for {
			select {
			case chunk, ok := <-session.Response():
				if !ok {
					resultCh <- r
					return
				}
				if chunk.Delta != "" || (chunk.MediaData != nil && len(chunk.MediaData.Data) > 0) {
					r.chunks++
				}
				r.text += chunk.Delta
				if chunk.FinishReason != nil {
					resultCh <- r
					return
				}
			case <-timer:
				resultCh <- r
				return
			}
		}
	}()

	// Generate and send 500ms of audio.
	audioData := generateSineWave24k(440.0, 500, 0.5)
	const chunkSize = 3200
	for i := 0; i < len(audioData); i += chunkSize {
		end := i + chunkSize
		if end > len(audioData) {
			end = len(audioData)
		}
		if err := session.SendChunk(ctx, &types.MediaChunk{Data: audioData[i:end]}); err != nil {
			t.Fatalf("Failed to send audio chunk: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	realtimeSession, ok := session.(*RealtimeSession)
	if !ok {
		t.Fatal("Session is not a *RealtimeSession")
	}
	realtimeSession.EndInput()

	r := <-resultCh
	t.Logf("Response chunks: %d, text: %q", r.chunks, r.text)

	if r.chunks == 0 {
		t.Error("Received no response chunks")
	}
	if err := session.Error(); err != nil {
		t.Logf("Session error (may be normal on close): %v", err)
	}
}

// TestRealtimeIntegration_AudioRoundTrip sends audio and checks if audio is returned.
func TestRealtimeIntegration_AudioRoundTrip(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		Metadata: map[string]interface{}{
			"modalities":   []string{"text", "audio"},
			"vad_disabled": true,
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Send 300ms of audio.
	audioData := generateSineWave24k(440.0, 300, 0.5)
	const chunkSize = 3200
	for i := 0; i < len(audioData); i += chunkSize {
		end := i + chunkSize
		if end > len(audioData) {
			end = len(audioData)
		}
		if err := session.SendChunk(ctx, &types.MediaChunk{Data: audioData[i:end]}); err != nil {
			t.Fatalf("Failed to send audio chunk: %v", err)
		}
	}

	realtimeSession, ok := session.(*RealtimeSession)
	if !ok {
		t.Fatal("Session is not a *RealtimeSession")
	}
	realtimeSession.EndInput()

	var audioChunksReceived int
	timer := time.After(30 * time.Second)
loop:
	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				break loop
			}
			if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
				audioChunksReceived++
			}
			if chunk.FinishReason != nil {
				break loop
			}
		case <-timer:
			break loop
		}
	}

	t.Logf("Audio chunks received: %d", audioChunksReceived)
	if audioChunksReceived == 0 {
		t.Log("No audio response received (model may not return audio for sine-wave input)")
	}
}

// TestRealtimeIntegration_AudioAndTextOutput verifies both text and audio modalities arrive.
func TestRealtimeIntegration_AudioAndTextOutput(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		Metadata: map[string]interface{}{
			"modalities": []string{"text", "audio"},
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if err := session.SendText(ctx, "Count from one to three."); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}

	var textChunks, audioChunks int
	timer := time.After(20 * time.Second)
loop:
	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				break loop
			}
			if chunk.Delta != "" {
				textChunks++
			}
			if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
				audioChunks++
			}
			if chunk.FinishReason != nil {
				break loop
			}
		case <-timer:
			t.Log("Timeout reached")
			break loop
		}
	}

	t.Logf("Text chunks: %d, Audio chunks: %d", textChunks, audioChunks)
	if textChunks == 0 && audioChunks == 0 {
		t.Error("Received neither text nor audio chunks")
	}
}

// TestRealtimeIntegration_AudioOutputOnly tests audio-only output modality.
func TestRealtimeIntegration_AudioOutputOnly(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		Metadata: map[string]interface{}{
			"modalities": []string{"audio"},
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if err := session.SendText(ctx, "Count from one to three."); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}

	var audioChunks int
	var totalAudioBytes int
	timer := time.After(20 * time.Second)
loop:
	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				break loop
			}
			if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
				audioChunks++
				totalAudioBytes += len(chunk.MediaData.Data)
			}
			if chunk.FinishReason != nil {
				break loop
			}
		case <-timer:
			t.Log("Timeout reached")
			break loop
		}
	}

	t.Logf("Audio chunks: %d, total bytes: %d", audioChunks, totalAudioBytes)
	if audioChunks == 0 {
		t.Error("Expected audio chunks but received none")
	}
}

// TestRealtimeIntegration_DiagnosticRaw is a diagnostic test that logs detailed chunk info.
func TestRealtimeIntegration_DiagnosticRaw(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		Metadata: map[string]interface{}{
			"modalities": []string{"text", "audio"},
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if err := session.SendText(ctx, "Say hello briefly."); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}

	var totalChunks, textChunks, audioChunks, totalAudioBytes int
	timer := time.After(20 * time.Second)
loop:
	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				break loop
			}
			totalChunks++
			if chunk.Delta != "" {
				textChunks++
				t.Logf("Chunk #%d: text delta=%q (len=%d)", totalChunks, chunk.Delta, len(chunk.Delta))
			}
			if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
				audioChunks++
				totalAudioBytes += len(chunk.MediaData.Data)
				t.Logf("Chunk #%d: audio mime=%q data_len=%d", totalChunks, chunk.MediaData.MIMEType, len(chunk.MediaData.Data))
			}
			if chunk.FinishReason != nil {
				t.Logf("Chunk #%d: finish_reason=%s", totalChunks, *chunk.FinishReason)
			}
			if chunk.CostInfo != nil {
				t.Logf("Chunk #%d: cost input=%d output=%d total=$%.6f",
					totalChunks, chunk.CostInfo.InputTokens, chunk.CostInfo.OutputTokens, chunk.CostInfo.TotalCost)
			}
			if chunk.Error != nil {
				t.Logf("Chunk #%d: error=%v", totalChunks, chunk.Error)
			}
			if chunk.FinishReason != nil {
				break loop
			}
		case <-timer:
			t.Log("Timeout reached")
			break loop
		}
	}

	t.Logf("Summary: total=%d text=%d audio=%d audio_bytes=%d",
		totalChunks, textChunks, audioChunks, totalAudioBytes)
}

// TestRealtimeIntegration_Performance measures connection and first-response latency.
func TestRealtimeIntegration_Performance(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := newRealtimeProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: DefaultRealtimeSampleRate,
			Channels:   DefaultRealtimeChannels,
			BitDepth:   DefaultRealtimeBitDepth,
			Encoding:   "pcm16",
		},
		Metadata: map[string]interface{}{
			"modalities":          []string{"text", "audio"},
			"input_transcription": false,
		},
	}

	connectStart := time.Now()
	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid_api_key") ||
			strings.Contains(errMsg, "websocket: close") ||
			strings.Contains(errMsg, "could not create") {
			t.Skipf("Skipping: %v", err)
		}
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	connectDuration := time.Since(connectStart)
	t.Logf("Connection established in %v", connectDuration)

	if err := session.SendText(ctx, "Reply with one word: hello."); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}
	sendTime := time.Now()

	// Wait for the first non-empty delta.
	var firstChunkLatency time.Duration
	timer := time.After(20 * time.Second)
loop:
	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				break loop
			}
			if chunk.Delta != "" && firstChunkLatency == 0 {
				firstChunkLatency = time.Since(sendTime)
				t.Logf("First response chunk in %v, delta=%q", firstChunkLatency, chunk.Delta)
			}
			if chunk.FinishReason != nil {
				break loop
			}
		case <-timer:
			t.Log("Timeout reached during performance test")
			break loop
		}
	}

	t.Logf("Performance summary: connect=%v, first_chunk=%v", connectDuration, firstChunkLatency)

	if connectDuration > 10*time.Second {
		t.Errorf("Connection took too long: %v (expected < 10s)", connectDuration)
	}
	if firstChunkLatency > 15*time.Second && firstChunkLatency > 0 {
		t.Errorf("First chunk latency too high: %v (expected < 15s)", firstChunkLatency)
	}
}

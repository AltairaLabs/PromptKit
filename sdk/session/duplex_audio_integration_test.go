package session

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestDuplexSessionWithStreaming creates a DuplexSession configured for ASM mode
// with a mock streaming provider. Returns both the session and the mock provider for verification.
func createTestDuplexSessionWithStreaming(t *testing.T) (DuplexSession, *mock.StreamingProvider) {
	t.Helper()
	ctx := context.Background()

	// Create streaming provider
	provider := mock.NewStreamingProvider("test", "test-model", false)

	// Pipeline builder that creates a DuplexProviderStage
	// The DuplexProviderStage creates the session lazily using system_prompt from element metadata
	pipelineBuilder := func(
		ctx context.Context,
		p providers.Provider,
		streamProvider providers.StreamInputSupport,
		streamConfig *providers.StreamingInputConfig,
		cid string,
		s statestore.Store,
	) (*stage.StreamPipeline, error) {
		// streamProvider is the streaming provider passed for ASM mode
		if streamProvider == nil {
			t.Log("WARNING: stream provider is nil in pipeline builder")
			// For testing, create a provider stage instead
			providerStage := stage.NewProviderStage(p, nil, nil, nil)
			return stage.NewPipelineBuilder().Chain(providerStage).Build()
		}
		// DuplexProviderStage creates session lazily using system_prompt from element metadata
		duplexStage := stage.NewDuplexProviderStage(streamProvider, streamConfig)
		return stage.NewPipelineBuilder().Chain(duplexStage).Build()
	}

	// Create session with Config to trigger ASM mode
	session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
		Provider:        provider,
		Config:          &providers.StreamingInputConfig{}, // Triggers ASM mode
		PipelineBuilder: pipelineBuilder,
	})
	require.NoError(t, err)
	require.NotNil(t, session)

	return session, provider
}

// generateTestAudioData creates test audio data with a known pattern.
func generateTestAudioData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		// Simple pattern: alternating values
		data[i] = byte(i % 256)
	}
	return data
}

// TestAudioFlowThroughPipeline verifies that audio chunks sent via SendChunk
// reach the mock provider session.
func TestAudioFlowThroughPipeline(t *testing.T) {
	session, provider := createTestDuplexSessionWithStreaming(t)
	defer session.Close()

	ctx := context.Background()

	// Generate test audio data
	audioData := generateTestAudioData(1600) // 100ms at 16kHz mono
	audioStr := string(audioData)

	// Create audio chunk
	chunk := &providers.StreamChunk{
		MediaDelta: &types.MediaContent{
			MIMEType: types.MIMETypeAudioWAV,
			Data:     &audioStr,
		},
	}

	// Send the chunk
	t.Log("Sending audio chunk...")
	err := session.SendChunk(ctx, chunk)
	require.NoError(t, err)
	t.Log("Audio chunk sent successfully")

	// Wait for pipeline to process
	time.Sleep(200 * time.Millisecond)

	// Verify chunk reached the mock session
	mockSession := provider.GetSession()
	if mockSession == nil {
		t.Fatal("No mock session created - ASM mode may not have been triggered")
	}

	receivedChunks := mockSession.GetChunks()
	t.Logf("Mock session received %d chunks", len(receivedChunks))

	if len(receivedChunks) == 0 {
		t.Log("FAILURE: No chunks received by mock session")
		t.Log("This indicates audio is NOT flowing through the pipeline to the provider")

		// Additional diagnostics
		texts := mockSession.GetTexts()
		t.Logf("Mock session received %d text messages", len(texts))
		for i, txt := range texts {
			t.Logf("  Text %d: %s", i, txt)
		}

		t.FailNow()
	}

	// Verify audio data matches
	assert.Equal(t, 1, len(receivedChunks), "Expected exactly 1 chunk")
	assert.Equal(t, audioData, receivedChunks[0].Data, "Audio data should match")
	t.Log("SUCCESS: Audio chunk reached mock session with correct data")
}

// TestResponseFlowsBack verifies that responses from the provider
// flow back through the pipeline to the session's Response channel.
// Note: Uses SendText because the mock session's auto-respond is triggered by SendText,
// not by SendChunk (which only responds when EndInput is called).
func TestResponseFlowsBack(t *testing.T) {
	ctx := context.Background()

	// Create streaming provider and configure auto-respond BEFORE session creation
	// With lazy session creation, the provider must be configured first
	provider := mock.NewStreamingProvider("test", "test-model", false).
		WithAutoRespond("Hello from mock provider")

	// Pipeline builder
	pipelineBuilder := func(
		ctx context.Context,
		p providers.Provider,
		streamProvider providers.StreamInputSupport,
		streamConfig *providers.StreamingInputConfig,
		cid string,
		s statestore.Store,
	) (*stage.StreamPipeline, error) {
		if streamProvider == nil {
			providerStage := stage.NewProviderStage(p, nil, nil, nil)
			return stage.NewPipelineBuilder().Chain(providerStage).Build()
		}
		duplexStage := stage.NewDuplexProviderStage(streamProvider, streamConfig)
		return stage.NewPipelineBuilder().Chain(duplexStage).Build()
	}

	// Create session
	session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
		Provider:        provider,
		Config:          &providers.StreamingInputConfig{},
		PipelineBuilder: pipelineBuilder,
	})
	require.NoError(t, err)
	defer session.Close()

	// Send text to trigger auto-respond
	// Note: SendText triggers auto-respond, SendChunk does not (requires EndInput)
	err = session.SendText(ctx, "Hello")
	require.NoError(t, err)

	// Read response from session
	responseChan := session.Response()
	require.NotNil(t, responseChan)

	select {
	case response, ok := <-responseChan:
		if !ok {
			t.Fatal("Response channel closed without receiving response")
		}
		t.Logf("Received response: Delta=%q, Content=%q, FinishReason=%v",
			response.Delta, response.Content, response.FinishReason)
		assert.Contains(t, response.Content, "Hello from mock provider")
		t.Log("SUCCESS: Response flowed back through pipeline")
	case <-time.After(2 * time.Second):
		t.Fatal("FAILURE: Timeout waiting for response - responses may not be flowing back")
	}
}

// TestMultipleAudioChunksFlow verifies that multiple audio chunks
// all reach the provider in order.
func TestMultipleAudioChunksFlow(t *testing.T) {
	session, provider := createTestDuplexSessionWithStreaming(t)
	defer session.Close()

	ctx := context.Background()
	numChunks := 10

	// Send multiple chunks
	for i := 0; i < numChunks; i++ {
		audioData := generateTestAudioData(320)
		// Mark each chunk with its index
		audioData[0] = byte(i)
		audioStr := string(audioData)

		err := session.SendChunk(ctx, &providers.StreamChunk{
			MediaDelta: &types.MediaContent{
				MIMEType: types.MIMETypeAudioWAV,
				Data:     &audioStr,
			},
		})
		require.NoError(t, err)
	}

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Verify all chunks received
	mockSession := provider.GetSession()
	if mockSession == nil {
		t.Fatal("No mock session created")
	}

	receivedChunks := mockSession.GetChunks()
	t.Logf("Sent %d chunks, received %d chunks", numChunks, len(receivedChunks))

	if len(receivedChunks) != numChunks {
		t.Fatalf("FAILURE: Expected %d chunks, got %d - some chunks were lost", numChunks, len(receivedChunks))
	}

	// Verify order preserved
	for i, chunk := range receivedChunks {
		assert.Equal(t, byte(i), chunk.Data[0], "Chunk %d should have marker %d", i, i)
	}
	t.Log("SUCCESS: All chunks received in order")
}

// TestAudioDataIntegrity verifies that audio data is not corrupted
// during the conversion process through the pipeline.
func TestAudioDataIntegrity(t *testing.T) {
	session, provider := createTestDuplexSessionWithStreaming(t)
	defer session.Close()

	ctx := context.Background()

	// Generate a specific audio pattern
	audioData := make([]byte, 1600)
	for i := range audioData {
		// Create a recognizable pattern
		audioData[i] = byte((i * 7) % 256)
	}
	audioStr := string(audioData)

	// Send chunk
	err := session.SendChunk(ctx, &providers.StreamChunk{
		MediaDelta: &types.MediaContent{
			MIMEType: types.MIMETypeAudioWAV,
			Data:     &audioStr,
		},
	})
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify data integrity
	mockSession := provider.GetSession()
	if mockSession == nil {
		t.Fatal("No mock session created")
	}

	receivedChunks := mockSession.GetChunks()
	require.Equal(t, 1, len(receivedChunks), "Expected 1 chunk")

	if !bytes.Equal(audioData, receivedChunks[0].Data) {
		t.Log("FAILURE: Audio data was corrupted during transmission")
		t.Logf("Original length: %d, Received length: %d", len(audioData), len(receivedChunks[0].Data))

		// Find first difference
		for i := 0; i < len(audioData) && i < len(receivedChunks[0].Data); i++ {
			if audioData[i] != receivedChunks[0].Data[i] {
				t.Logf("First difference at byte %d: expected %d, got %d", i, audioData[i], receivedChunks[0].Data[i])
				break
			}
		}
		t.FailNow()
	}

	t.Log("SUCCESS: Audio data integrity preserved")
}

// TestTextFlowThroughPipeline verifies that text messages also flow correctly
// (for comparison with audio flow).
func TestTextFlowThroughPipeline(t *testing.T) {
	session, provider := createTestDuplexSessionWithStreaming(t)
	defer session.Close()

	ctx := context.Background()

	// Send text
	err := session.SendText(ctx, "Hello, this is a test message")
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify text reached mock session
	mockSession := provider.GetSession()
	if mockSession == nil {
		t.Fatal("No mock session created")
	}

	texts := mockSession.GetTexts()
	t.Logf("Mock session received %d text messages", len(texts))

	if len(texts) == 0 {
		t.Fatal("FAILURE: No text messages received by mock session")
	}

	assert.Contains(t, texts[0], "Hello, this is a test message")
	t.Log("SUCCESS: Text message reached mock session")
}

// TestDiagnostics provides detailed diagnostics about the audio flow path.
func TestDiagnostics(t *testing.T) {
	ctx := context.Background()

	t.Log("=== Audio Flow Diagnostics ===")

	// Step 1: Check StreamingProvider creation
	provider := mock.NewStreamingProvider("test", "test-model", false)
	t.Log("1. StreamingProvider created")

	// Step 2: Check if provider supports stream input
	caps := provider.GetStreamingCapabilities()
	t.Logf("2. Streaming capabilities: %+v", caps)

	// Step 3: Create session config
	config := &providers.StreamingInputConfig{}
	t.Logf("3. StreamingInputConfig created: %+v", config)

	// Step 4: Track what happens in pipeline builder
	pipelineBuilder := func(
		ctx context.Context,
		p providers.Provider,
		streamProvider providers.StreamInputSupport,
		streamConfig *providers.StreamingInputConfig,
		cid string,
		s statestore.Store,
	) (*stage.StreamPipeline, error) {
		t.Logf("4. PipelineBuilder called:")
		t.Logf("   - Provider: %T", p)
		t.Logf("   - StreamProvider: %T (nil=%v)", streamProvider, streamProvider == nil)
		t.Logf("   - StreamConfig: %+v", streamConfig)
		t.Logf("   - ConversationID: %s", cid)
		t.Logf("   - StateStore: %T (nil=%v)", s, s == nil)

		if streamProvider == nil {
			t.Log("   WARNING: StreamProvider is nil - not in ASM mode")
			providerStage := stage.NewProviderStage(p, nil, nil, nil)
			return stage.NewPipelineBuilder().Chain(providerStage).Build()
		}

		t.Log("   Creating DuplexProviderStage with provider + config (session created lazily)")
		duplexStage := stage.NewDuplexProviderStage(streamProvider, streamConfig)
		return stage.NewPipelineBuilder().Chain(duplexStage).Build()
	}

	// Step 5: Create DuplexSession
	session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
		Provider:        provider,
		Config:          config, // This should trigger ASM mode
		PipelineBuilder: pipelineBuilder,
	})
	require.NoError(t, err)
	defer session.Close()
	t.Log("5. DuplexSession created")

	// Step 6: Check if mock session was created
	mockSession := provider.GetSession()
	if mockSession == nil {
		t.Log("6. PROBLEM: No MockSession created by provider")
		t.Log("   This means the DuplexSession didn't call CreateStreamSession")
		t.Log("   Check DuplexSession initialization for ASM mode")
	} else {
		t.Log("6. MockSession created successfully")
	}

	// Step 7: Send a test chunk
	audioData := []byte{1, 2, 3, 4, 5}
	audioStr := string(audioData)
	err = session.SendChunk(ctx, &providers.StreamChunk{
		MediaDelta: &types.MediaContent{
			MIMEType: types.MIMETypeAudioWAV,
			Data:     &audioStr,
		},
	})
	require.NoError(t, err)
	t.Log("7. Test chunk sent via SendChunk")

	// Step 8: Wait and check what arrived
	time.Sleep(300 * time.Millisecond)

	if mockSession != nil {
		chunks := mockSession.GetChunks()
		texts := mockSession.GetTexts()
		t.Logf("8. MockSession state:")
		t.Logf("   - Received chunks: %d", len(chunks))
		t.Logf("   - Received texts: %d", len(texts))

		if len(chunks) == 0 && len(texts) == 0 {
			t.Log("   PROBLEM: Nothing reached the MockSession")
			t.Log("   Audio is being lost somewhere in the pipeline")
		} else if len(chunks) > 0 {
			t.Logf("   SUCCESS: %d chunks reached MockSession", len(chunks))
			t.Logf("   First chunk data length: %d bytes", len(chunks[0].Data))
		}
	}

	t.Log("=== End Diagnostics ===")
}

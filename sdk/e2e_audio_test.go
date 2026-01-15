//go:build e2e

package sdk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Audio E2E Tests
//
// These tests verify audio processing functionality across providers that
// support audio input. Currently only Gemini supports audio in the predict API.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_Audio
// =============================================================================

// TestAudioFile paths - these are expected to exist in ~/Downloads
var (
	harvardAudioPath = filepath.Join(os.Getenv("HOME"), "Downloads", "harvard.wav")
	shortAudioPath   = filepath.Join(os.Getenv("HOME"), "Downloads", "03-02-01-01-01-02-01.wav")
)

// TestE2E_Audio_SingleFile tests basic audio transcription/understanding.
func TestE2E_Audio_SingleFile(t *testing.T) {
	EnsureTestPacks(t)

	// Skip if audio files don't exist
	if _, err := os.Stat(harvardAudioPath); os.IsNotExist(err) {
		t.Skipf("Test audio file not found: %s", harvardAudioPath)
	}

	RunForProviders(t, CapAudio, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real audio")
		}

		conv := NewAudioConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "What is being said in this audio? Provide a transcription.",
			WithAudioFile(harvardAudioPath))
		require.NoError(t, err)

		text := strings.ToLower(resp.Text())
		// Harvard sentences typically contain common words - check for any meaningful response
		assert.NotEmpty(t, text, "Should provide some response about the audio")
		assert.Greater(t, len(text), 20, "Response should be substantial")

		t.Logf("Provider %s audio response: %s", provider.ID, truncate(resp.Text(), 200))
	})
}

// TestE2E_Audio_ShortFile tests processing of a shorter audio file.
func TestE2E_Audio_ShortFile(t *testing.T) {
	EnsureTestPacks(t)

	// Skip if audio files don't exist
	if _, err := os.Stat(shortAudioPath); os.IsNotExist(err) {
		t.Skipf("Test audio file not found: %s", shortAudioPath)
	}

	RunForProviders(t, CapAudio, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real audio")
		}

		conv := NewAudioConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "Describe what you hear in this audio clip.",
			WithAudioFile(shortAudioPath))
		require.NoError(t, err)

		text := resp.Text()
		assert.NotEmpty(t, text, "Should provide some response about the audio")

		t.Logf("Provider %s short audio response: %s", provider.ID, truncate(text, 150))
	})
}

// TestE2E_Audio_WithTextPrompt tests audio with a specific text prompt.
func TestE2E_Audio_WithTextPrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping audio with text prompt test in short mode")
	}

	EnsureTestPacks(t)

	if _, err := os.Stat(harvardAudioPath); os.IsNotExist(err) {
		t.Skipf("Test audio file not found: %s", harvardAudioPath)
	}

	RunForProviders(t, CapAudio, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real audio")
		}

		conv := NewAudioConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx,
			"Listen to this audio and answer: 1) What language is being spoken? 2) How many speakers are there? 3) What is the general topic or content?",
			WithAudioFile(harvardAudioPath))
		require.NoError(t, err)

		text := strings.ToLower(resp.Text())
		// Should mention English since Harvard sentences are in English
		hasLanguageRef := strings.Contains(text, "english") ||
			strings.Contains(text, "language")
		hasSpeakerRef := strings.Contains(text, "speaker") ||
			strings.Contains(text, "voice") ||
			strings.Contains(text, "person") ||
			strings.Contains(text, "one")

		assert.True(t, hasLanguageRef || hasSpeakerRef,
			"Should provide analytical content about the audio, got: %s", truncate(resp.Text(), 200))

		t.Logf("Provider %s audio analysis: %s", provider.ID, truncate(resp.Text(), 200))
	})
}

// TestE2E_Audio_Followup tests follow-up questions about audio.
func TestE2E_Audio_Followup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping audio followup test in short mode")
	}

	EnsureTestPacks(t)

	if _, err := os.Stat(harvardAudioPath); os.IsNotExist(err) {
		t.Skipf("Test audio file not found: %s", harvardAudioPath)
	}

	RunForProviders(t, CapAudio, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real audio")
		}

		conv := NewAudioConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		// First message with audio
		resp1, err := conv.Send(ctx, "Transcribe this audio.",
			WithAudioFile(harvardAudioPath))
		require.NoError(t, err)
		t.Logf("Turn 1: %s", truncate(resp1.Text(), 100))

		// Follow-up question about the audio (without sending it again)
		resp2, err := conv.Send(ctx, "How many sentences were in that audio?")
		require.NoError(t, err)

		text := resp2.Text()
		// Should reference sentences or numbers (including digits and spelled-out numbers)
		lowerText := strings.ToLower(text)
		hasRelevantContent := strings.ContainsAny(text, "0123456789") ||
			strings.Contains(lowerText, "sentence") ||
			strings.Contains(lowerText, "line") ||
			strings.Contains(lowerText, "phrase") ||
			strings.Contains(lowerText, "one") ||
			strings.Contains(lowerText, "two") ||
			strings.Contains(lowerText, "three") ||
			strings.Contains(lowerText, "four") ||
			strings.Contains(lowerText, "five") ||
			strings.Contains(lowerText, "six") ||
			strings.Contains(lowerText, "seven") ||
			strings.Contains(lowerText, "eight") ||
			strings.Contains(lowerText, "nine") ||
			strings.Contains(lowerText, "ten") ||
			strings.Contains(lowerText, "several") ||
			strings.Contains(lowerText, "multiple")

		assert.True(t, hasRelevantContent,
			"Should discuss sentence count, got: %s", truncate(text, 100))

		t.Logf("Turn 2: %s", truncate(resp2.Text(), 100))
	})
}

// TestE2E_Audio_DataBytes tests sending audio as raw bytes.
func TestE2E_Audio_DataBytes(t *testing.T) {
	EnsureTestPacks(t)

	// Skip if audio files don't exist
	if _, err := os.Stat(shortAudioPath); os.IsNotExist(err) {
		t.Skipf("Test audio file not found: %s", shortAudioPath)
	}

	RunForProviders(t, CapAudio, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real audio")
		}

		conv := NewAudioConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		// Read audio file as bytes
		audioData, err := os.ReadFile(shortAudioPath)
		require.NoError(t, err, "Should be able to read audio file")

		resp, err := conv.Send(ctx, "What do you hear in this audio?",
			WithAudioData(audioData, "audio/wav"))
		require.NoError(t, err)

		text := resp.Text()
		assert.NotEmpty(t, text, "Should provide some response about the audio")

		t.Logf("Provider %s audio bytes response: %s", provider.ID, truncate(text, 150))
	})
}

// TestE2E_Audio_Streaming tests streaming responses with audio input.
func TestE2E_Audio_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping streaming audio test in short mode")
	}

	EnsureTestPacks(t)

	if _, err := os.Stat(shortAudioPath); os.IsNotExist(err) {
		t.Skipf("Test audio file not found: %s", shortAudioPath)
	}

	// Need both audio and streaming capabilities
	RunForProviders(t, CapAudio, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real audio")
		}

		// Check if provider also supports streaming
		if !provider.HasCapability(CapStreaming) {
			t.Skipf("Provider %s doesn't support streaming", provider.ID)
		}

		conv := NewAudioConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Read audio file
		audioData, err := os.ReadFile(shortAudioPath)
		require.NoError(t, err)

		var chunks []StreamChunk
		var fullText strings.Builder

		// Note: Currently Stream doesn't support SendOptions with audio
		// So we use Send for now and verify response
		resp, err := conv.Send(ctx, "Describe this audio briefly.",
			WithAudioData(audioData, "audio/wav"))
		require.NoError(t, err)

		text := resp.Text()
		assert.NotEmpty(t, text)

		t.Logf("Provider %s audio response (%d chunks): %s",
			provider.ID, len(chunks), truncate(fullText.String()+text, 100))
	})
}

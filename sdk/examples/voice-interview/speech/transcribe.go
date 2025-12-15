// Package speech provides speech-to-text and text-to-speech services
// for voice-enabled LLM interactions.
package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
)

// Transcriber converts audio to text
type Transcriber interface {
	// Transcribe converts audio bytes to text
	// audioFormat should be "wav", "mp3", "pcm", etc.
	Transcribe(ctx context.Context, audio []byte, audioFormat string) (string, error)
}

// OpenAITranscriber uses OpenAI's Whisper API for transcription
type OpenAITranscriber struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewOpenAITranscriber creates a new OpenAI Whisper transcriber
func NewOpenAITranscriber(apiKey string) *OpenAITranscriber {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	return &OpenAITranscriber{
		apiKey:     apiKey,
		model:      "whisper-1",
		httpClient: &http.Client{},
	}
}

// Transcribe converts audio to text using OpenAI Whisper API
func (t *OpenAITranscriber) Transcribe(ctx context.Context, audio []byte, audioFormat string) (string, error) {
	if t.apiKey == "" {
		return "", fmt.Errorf("OpenAI API key not configured")
	}

	// Whisper API expects a file upload, so we need to use multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add the audio file
	// Use appropriate extension based on format
	filename := "audio." + audioFormat
	if audioFormat == "pcm" {
		// PCM needs to be wrapped as WAV for Whisper
		audio = wrapPCMAsWAV(audio, 16000, 1, 16)
		filename = "audio.wav"
	}

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add model field
	if err := writer.WriteField("model", t.model); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	// Add language hint (optional, improves accuracy)
	if err := writer.WriteField("language", "en"); err != nil {
		return "", fmt.Errorf("failed to write language field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/transcriptions", &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transcription failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Text, nil
}

// wrapPCMAsWAV wraps raw PCM audio data in a WAV header
func wrapPCMAsWAV(pcmData []byte, sampleRate, channels, bitsPerSample int) []byte {
	dataSize := len(pcmData)
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	// WAV header is 44 bytes
	wav := make([]byte, 44+dataSize)

	// RIFF header
	copy(wav[0:4], "RIFF")
	putLE32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")

	// fmt subchunk
	copy(wav[12:16], "fmt ")
	putLE32(wav[16:20], 16) // Subchunk1Size for PCM
	putLE16(wav[20:22], 1)  // AudioFormat (1 = PCM)
	putLE16(wav[22:24], uint16(channels))
	putLE32(wav[24:28], uint32(sampleRate))
	putLE32(wav[28:32], uint32(byteRate))
	putLE16(wav[32:34], uint16(blockAlign))
	putLE16(wav[34:36], uint16(bitsPerSample))

	// data subchunk
	copy(wav[36:40], "data")
	putLE32(wav[40:44], uint32(dataSize))
	copy(wav[44:], pcmData)

	return wav
}

// putLE16 writes a uint16 in little-endian format
func putLE16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

// putLE32 writes a uint32 in little-endian format
func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

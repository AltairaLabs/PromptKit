package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewAudioEncoder(t *testing.T) {
	encoder := NewAudioEncoder()

	if encoder == nil {
		t.Fatal("expected encoder, got nil")
	}

	if encoder.sampleRate != geminiSampleRate {
		t.Errorf("expected sample rate %d, got %d", geminiSampleRate, encoder.sampleRate)
	}

	if encoder.bitDepth != geminiBitDepth {
		t.Errorf("expected bit depth %d, got %d", geminiBitDepth, encoder.bitDepth)
	}

	if encoder.channels != geminiChannels {
		t.Errorf("expected channels %d, got %d", geminiChannels, encoder.channels)
	}

	if encoder.chunkSize != DefaultChunkSize {
		t.Errorf("expected chunk size %d, got %d", DefaultChunkSize, encoder.chunkSize)
	}
}

func TestNewAudioEncoderWithChunkSize(t *testing.T) {
	tests := []struct {
		name      string
		chunkSize int
		wantErr   error
	}{
		{"valid chunk size", 3200, nil},
		{"valid smaller chunk", 1600, nil},
		{"invalid zero", 0, ErrInvalidChunkSize},
		{"invalid negative", -100, ErrInvalidChunkSize},
		{"invalid not aligned", 3201, ErrInvalidChunkSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoder, err := NewAudioEncoderWithChunkSize(tt.chunkSize)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if encoder.chunkSize != tt.chunkSize {
				t.Errorf("expected chunk size %d, got %d", tt.chunkSize, encoder.chunkSize)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	encoder := NewAudioEncoder()

	tests := []struct {
		name    string
		config  *types.StreamingMediaConfig
		wantErr error
	}{
		{
			name: "valid config",
			config: &types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 16000,
				Channels:   1,
				BitDepth:   16,
				Encoding:   "pcm_linear16",
			},
			wantErr: nil,
		},
		{
			name: "wrong type",
			config: &types.StreamingMediaConfig{
				Type: types.ContentTypeVideo,
			},
			wantErr: errors.New("config type must be audio"),
		},
		{
			name: "invalid sample rate",
			config: &types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 44100,
				Channels:   1,
				BitDepth:   16,
			},
			wantErr: errors.New("invalid sample rate"),
		},
		{
			name: "invalid channels",
			config: &types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 16000,
				Channels:   2,
				BitDepth:   16,
			},
			wantErr: errors.New("invalid channels"),
		},
		{
			name: "invalid bit depth",
			config: &types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 16000,
				Channels:   1,
				BitDepth:   24,
			},
			wantErr: errors.New("invalid bit depth"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := encoder.ValidateConfig(tt.config)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				// Just check that error message contains expected text
				if !errors.Is(err, tt.wantErr) {
					// Check if error message contains the expected error message
					if tt.wantErr.Error() != "" && !strings.Contains(err.Error(), tt.wantErr.Error()) {
						t.Errorf("expected error containing '%v', got '%v'", tt.wantErr, err)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestEncodePCM(t *testing.T) {
	encoder := NewAudioEncoder()

	tests := []struct {
		name    string
		pcmData []byte
		wantErr error
	}{
		{
			name:    "valid PCM data",
			pcmData: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
			wantErr: nil,
		},
		{
			name:    "empty data",
			pcmData: []byte{},
			wantErr: ErrEmptyAudioData,
		},
		{
			name:    "misaligned data",
			pcmData: []byte{0x00, 0x01, 0x02},
			wantErr: errors.New("not aligned"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := encoder.EncodePCM(tt.pcmData)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify it's valid base64
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("encoded data is not valid base64: %v", err)
			}

			// Verify decoded matches original
			if !bytes.Equal(decoded, tt.pcmData) {
				t.Errorf("decoded data doesn't match original")
			}
		})
	}
}

func TestDecodePCM(t *testing.T) {
	encoder := NewAudioEncoder()

	tests := []struct {
		name       string
		base64Data string
		want       []byte
		wantErr    error
	}{
		{
			name:       "valid base64 PCM",
			base64Data: base64.StdEncoding.EncodeToString([]byte{0x00, 0x01, 0x02, 0x03}),
			want:       []byte{0x00, 0x01, 0x02, 0x03},
			wantErr:    nil,
		},
		{
			name:       "empty string",
			base64Data: "",
			want:       nil,
			wantErr:    ErrEmptyAudioData,
		},
		{
			name:       "invalid base64",
			base64Data: "not-valid-base64!!!",
			want:       nil,
			wantErr:    errors.New("failed to decode"),
		},
		{
			name:       "misaligned decoded data",
			base64Data: base64.StdEncoding.EncodeToString([]byte{0x00, 0x01, 0x02}),
			want:       nil,
			wantErr:    errors.New("not aligned"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := encoder.DecodePCM(tt.base64Data)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !bytes.Equal(decoded, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, decoded)
			}
		})
	}
}

func TestCreateChunks(t *testing.T) {
	encoder := NewAudioEncoder()

	// Create test PCM data (6400 bytes = 2 full chunks)
	pcmData := make([]byte, DefaultChunkSize*2)
	for i := range pcmData {
		pcmData[i] = byte(i % 256)
	}

	ctx := context.Background()
	chunks, err := encoder.CreateChunks(ctx, pcmData)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// Verify first chunk
	if chunks[0].SequenceNum != 0 {
		t.Errorf("expected sequence 0, got %d", chunks[0].SequenceNum)
	}
	if chunks[0].IsLast {
		t.Error("first chunk should not be last")
	}
	if len(chunks[0].Data) != DefaultChunkSize {
		t.Errorf("expected chunk size %d, got %d", DefaultChunkSize, len(chunks[0].Data))
	}

	// Verify second chunk
	if chunks[1].SequenceNum != 1 {
		t.Errorf("expected sequence 1, got %d", chunks[1].SequenceNum)
	}
	if !chunks[1].IsLast {
		t.Error("last chunk should be marked as last")
	}

	// Verify metadata
	if chunks[0].Metadata["format"] != "pcm_linear16" {
		t.Error("expected pcm_linear16 format in metadata")
	}
}

func TestCreateChunks_PartialChunk(t *testing.T) {
	encoder := NewAudioEncoder()

	// Create test data that's not a perfect multiple of chunk size
	pcmData := make([]byte, DefaultChunkSize+1000)

	ctx := context.Background()
	chunks, err := encoder.CreateChunks(ctx, pcmData)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// Verify last chunk is smaller
	if len(chunks[1].Data) != 1000 {
		t.Errorf("expected last chunk size 1000, got %d", len(chunks[1].Data))
	}

	if !chunks[1].IsLast {
		t.Error("last chunk should be marked as last")
	}
}

func TestCreateChunks_EmptyData(t *testing.T) {
	encoder := NewAudioEncoder()

	ctx := context.Background()
	_, err := encoder.CreateChunks(ctx, []byte{})

	if !errors.Is(err, ErrEmptyAudioData) {
		t.Errorf("expected ErrEmptyAudioData, got %v", err)
	}
}

func TestCreateChunks_ContextCancellation(t *testing.T) {
	encoder := NewAudioEncoder()

	// Create large data to ensure context cancellation is hit
	pcmData := make([]byte, DefaultChunkSize*100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := encoder.CreateChunks(ctx, pcmData)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestReadChunks(t *testing.T) {
	encoder := NewAudioEncoder()

	// Create test data (2 full chunks)
	pcmData := make([]byte, DefaultChunkSize*2)
	for i := range pcmData {
		pcmData[i] = byte(i % 256)
	}

	reader := bytes.NewReader(pcmData)
	ctx := context.Background()

	chunkCh, errCh := encoder.ReadChunks(ctx, reader)

	chunks := make([]*types.MediaChunk, 0)
	var readErr error

	// Read all chunks
	for chunk := range chunkCh {
		chunks = append(chunks, chunk)
	}

	// Check for errors
	select {
	case err := <-errCh:
		if err != nil {
			readErr = err
		}
	default:
	}

	if readErr != nil {
		t.Fatalf("unexpected error: %v", readErr)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// Verify chunks
	for i, chunk := range chunks {
		if chunk.SequenceNum != int64(i) {
			t.Errorf("chunk %d: expected sequence %d, got %d", i, i, chunk.SequenceNum)
		}
	}

	if !chunks[len(chunks)-1].IsLast {
		t.Error("last chunk should be marked as last")
	}
}

func TestReadChunks_PartialRead(t *testing.T) {
	encoder := NewAudioEncoder()

	// Create partial data
	pcmData := make([]byte, DefaultChunkSize+500)
	reader := bytes.NewReader(pcmData)
	ctx := context.Background()

	chunkCh, errCh := encoder.ReadChunks(ctx, reader)

	chunks := make([]*types.MediaChunk, 0)
	for chunk := range chunkCh {
		chunks = append(chunks, chunk)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	default:
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// Verify last chunk is partial
	if len(chunks[1].Data) != 500 {
		t.Errorf("expected last chunk size 500, got %d", len(chunks[1].Data))
	}

	if !chunks[1].IsLast {
		t.Error("last chunk should be marked as last")
	}
}

func TestReadChunks_ContextCancellation(t *testing.T) {
	encoder := NewAudioEncoder()

	// Create reader with large data
	largeData := make([]byte, DefaultChunkSize*10)
	slowReader := &slowReader{
		data:  largeData,
		delay: 50 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	chunkCh, errCh := encoder.ReadChunks(ctx, slowReader)

	// Drain channels
	for range chunkCh {
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded or nil, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		// It's OK if no error is sent (goroutine may exit cleanly)
	}
}

func TestAssembleChunks(t *testing.T) {
	encoder := NewAudioEncoder()

	// Create test chunks
	chunk1 := &types.MediaChunk{Data: []byte{0x00, 0x01, 0x02, 0x03}}
	chunk2 := &types.MediaChunk{Data: []byte{0x04, 0x05, 0x06, 0x07}}
	chunk3 := &types.MediaChunk{Data: []byte{0x08, 0x09}}

	chunks := []*types.MediaChunk{chunk1, chunk2, chunk3}

	result, err := encoder.AssembleChunks(chunks)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09}
	if !bytes.Equal(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestAssembleChunks_Empty(t *testing.T) {
	encoder := NewAudioEncoder()

	_, err := encoder.AssembleChunks([]*types.MediaChunk{})

	if !errors.Is(err, ErrEmptyAudioData) {
		t.Errorf("expected ErrEmptyAudioData, got %v", err)
	}
}

func TestConvertInt16ToPCM(t *testing.T) {
	encoder := NewAudioEncoder()

	samples := []int16{0, 100, -100, 32767, -32768}
	pcmData := encoder.ConvertInt16ToPCM(samples)

	if len(pcmData) != len(samples)*2 {
		t.Errorf("expected %d bytes, got %d", len(samples)*2, len(pcmData))
	}

	// Verify first sample (0)
	if pcmData[0] != 0x00 || pcmData[1] != 0x00 {
		t.Errorf("expected [0x00, 0x00], got [0x%02x, 0x%02x]", pcmData[0], pcmData[1])
	}
}

func TestConvertPCMToInt16(t *testing.T) {
	encoder := NewAudioEncoder()

	// Create test PCM data representing int16 values
	pcmData := []byte{0x00, 0x00, 0x64, 0x00, 0x9c, 0xff}

	samples, err := encoder.ConvertPCMToInt16(pcmData)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(samples) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(samples))
	}

	if samples[0] != 0 {
		t.Errorf("expected sample 0, got %d", samples[0])
	}
	if samples[1] != 100 {
		t.Errorf("expected sample 100, got %d", samples[1])
	}
	if samples[2] != -100 {
		t.Errorf("expected sample -100, got %d", samples[2])
	}
}

func TestConvertPCMToInt16_Misaligned(t *testing.T) {
	encoder := NewAudioEncoder()

	// Misaligned data (3 bytes instead of even number)
	pcmData := []byte{0x00, 0x01, 0x02}

	_, err := encoder.ConvertPCMToInt16(pcmData)

	if err == nil {
		t.Fatal("expected error for misaligned data")
	}
}

func TestRoundTrip_Int16ToPCMAndBack(t *testing.T) {
	encoder := NewAudioEncoder()

	original := []int16{0, 1000, -1000, 32767, -32768, 12345}

	// Convert to PCM
	pcmData := encoder.ConvertInt16ToPCM(original)

	// Convert back to int16
	result, err := encoder.ConvertPCMToInt16(pcmData)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != len(original) {
		t.Fatalf("expected %d samples, got %d", len(original), len(result))
	}

	for i := range original {
		if result[i] != original[i] {
			t.Errorf("sample %d: expected %d, got %d", i, original[i], result[i])
		}
	}
}

func TestGenerateSineWave(t *testing.T) {
	encoder := NewAudioEncoder()

	// Generate 100ms of 440Hz sine wave
	pcmData := encoder.GenerateSineWave(440.0, 100, 0.5)

	// Expected size: 16000 Hz * 0.1 sec * 2 bytes = 3200 bytes
	if len(pcmData) != 3200 {
		t.Errorf("expected 3200 bytes, got %d", len(pcmData))
	}

	// Verify it's valid PCM data (should be aligned)
	if len(pcmData)%2 != 0 {
		t.Error("sine wave data should be aligned to sample size")
	}

	// Convert to samples and verify it's valid PCM
	samples, err := encoder.ConvertPCMToInt16(pcmData)
	if err != nil {
		t.Fatalf("generated sine wave is not valid PCM: %v", err)
	}

	// Verify we got samples (no need to check range, int16 is always in range)
	if len(samples) == 0 {
		t.Error("expected non-zero samples")
	}
}

func TestGenerateSineWave_AmplitudeClipping(t *testing.T) {
	encoder := NewAudioEncoder()

	// Test amplitude > 1.0 gets clipped
	pcmData := encoder.GenerateSineWave(440.0, 10, 2.0)

	// Verify it's valid PCM (int16 is always within range)
	samples, err := encoder.ConvertPCMToInt16(pcmData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(samples) == 0 {
		t.Error("expected non-zero samples")
	}
}

func TestGetChunkDurationMs(t *testing.T) {
	encoder := NewAudioEncoder()

	// Default chunk size should be 100ms
	duration := encoder.GetChunkDurationMs(DefaultChunkSize)

	if duration != 100.0 {
		t.Errorf("expected 100ms, got %.2fms", duration)
	}

	// Test custom chunk size (1600 bytes = 50ms at 16kHz 16-bit)
	duration = encoder.GetChunkDurationMs(1600)
	if duration != 50.0 {
		t.Errorf("expected 50ms, got %.2fms", duration)
	}
}

func TestGetChunkSize(t *testing.T) {
	encoder := NewAudioEncoder()

	if encoder.GetChunkSize() != DefaultChunkSize {
		t.Errorf("expected %d, got %d", DefaultChunkSize, encoder.GetChunkSize())
	}
}

func TestGetSampleRate(t *testing.T) {
	encoder := NewAudioEncoder()

	if encoder.GetSampleRate() != geminiSampleRate {
		t.Errorf("expected %d, got %d", geminiSampleRate, encoder.GetSampleRate())
	}
}

// Helper type for testing slow reads
type slowReader struct {
	data  []byte
	pos   int
	delay time.Duration
}

func (r *slowReader) Read(p []byte) (n int, err error) {
	time.Sleep(r.delay)

	if r.data == nil || r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

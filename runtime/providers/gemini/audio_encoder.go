package gemini

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

var (
	// ErrInvalidSampleRate indicates an unsupported sample rate
	ErrInvalidSampleRate = errors.New("invalid sample rate: must be 16000 Hz")
	// ErrInvalidChannels indicates an unsupported channel count
	ErrInvalidChannels = errors.New("invalid channels: must be mono (1 channel)")
	// ErrInvalidBitDepth indicates an unsupported bit depth
	ErrInvalidBitDepth = errors.New("invalid bit depth: must be 16 bits")
	// ErrInvalidChunkSize indicates chunk size is not aligned
	ErrInvalidChunkSize = errors.New("invalid chunk size: must be multiple of sample size")
	// ErrEmptyAudioData indicates no audio data provided
	ErrEmptyAudioData = errors.New("empty audio data")
)

const (
	// Audio format constants for Gemini Live API
	geminiSampleRate = 16000 // Hz
	geminiBitDepth   = 16    // bits per sample
	geminiChannels   = 1     // mono
	bytesPerSample   = geminiBitDepth / 8

	// DefaultChunkDuration is 100ms of audio
	DefaultChunkDuration = 100 // milliseconds
	// DefaultChunkSize is the number of bytes for 100ms at 16kHz 16-bit mono
	// 16000 Hz * 0.1 sec * 2 bytes/sample = 3200 bytes
	DefaultChunkSize = (geminiSampleRate * DefaultChunkDuration / 1000) * bytesPerSample
)

// AudioEncoder handles PCM Linear16 audio encoding for Gemini Live API
type AudioEncoder struct {
	sampleRate int
	bitDepth   int
	channels   int
	chunkSize  int
}

// NewAudioEncoder creates a new audio encoder with Gemini Live API specifications
func NewAudioEncoder() *AudioEncoder {
	return &AudioEncoder{
		sampleRate: geminiSampleRate,
		bitDepth:   geminiBitDepth,
		channels:   geminiChannels,
		chunkSize:  DefaultChunkSize,
	}
}

// NewAudioEncoderWithChunkSize creates an encoder with custom chunk size
func NewAudioEncoderWithChunkSize(chunkSize int) (*AudioEncoder, error) {
	if chunkSize <= 0 || chunkSize%bytesPerSample != 0 {
		return nil, ErrInvalidChunkSize
	}
	return &AudioEncoder{
		sampleRate: geminiSampleRate,
		bitDepth:   geminiBitDepth,
		channels:   geminiChannels,
		chunkSize:  chunkSize,
	}, nil
}

// ValidateConfig validates audio configuration against Gemini requirements
func (e *AudioEncoder) ValidateConfig(config *types.StreamingMediaConfig) error {
	if config.Type != types.ContentTypeAudio {
		return errors.New("config type must be audio")
	}

	if config.SampleRate != geminiSampleRate {
		return fmt.Errorf("invalid sample rate: must be 16000 Hz, got %d", config.SampleRate)
	}

	if config.Channels != geminiChannels {
		return fmt.Errorf("invalid channels: must be mono (1 channel), got %d", config.Channels)
	}

	if config.BitDepth != geminiBitDepth {
		return fmt.Errorf("invalid bit depth: must be 16 bits, got %d", config.BitDepth)
	}

	return nil
}

// EncodePCM encodes raw PCM audio data to base64 for WebSocket transmission
func (e *AudioEncoder) EncodePCM(pcmData []byte) (string, error) {
	if len(pcmData) == 0 {
		return "", ErrEmptyAudioData
	}

	// Validate data size is aligned to sample size
	if len(pcmData)%bytesPerSample != 0 {
		return "", fmt.Errorf("PCM data size %d not aligned to sample size %d", len(pcmData), bytesPerSample)
	}

	// Encode to base64 for JSON transport
	encoded := base64.StdEncoding.EncodeToString(pcmData)
	return encoded, nil
}

// DecodePCM decodes base64-encoded audio data back to raw PCM
func (e *AudioEncoder) DecodePCM(base64Data string) ([]byte, error) {
	if base64Data == "" {
		return nil, ErrEmptyAudioData
	}

	pcmData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 audio: %w", err)
	}

	// Validate decoded data size
	if len(pcmData)%bytesPerSample != 0 {
		return nil, fmt.Errorf("decoded PCM data size %d not aligned to sample size %d", len(pcmData), bytesPerSample)
	}

	return pcmData, nil
}

// CreateChunks splits PCM audio data into appropriately sized chunks
func (e *AudioEncoder) CreateChunks(ctx context.Context, pcmData []byte) ([]*types.MediaChunk, error) {
	if len(pcmData) == 0 {
		return nil, ErrEmptyAudioData
	}

	// Calculate number of chunks
	numChunks := (len(pcmData) + e.chunkSize - 1) / e.chunkSize
	chunks := make([]*types.MediaChunk, 0, numChunks)

	for i := 0; i < len(pcmData); i += e.chunkSize {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := i + e.chunkSize
		if end > len(pcmData) {
			end = len(pcmData)
		}

		chunk := &types.MediaChunk{
			Data:        pcmData[i:end],
			SequenceNum: int64(len(chunks)),
			IsLast:      end == len(pcmData),
			Metadata: map[string]string{
				"format":      "pcm_linear16",
				"sample_rate": fmt.Sprintf("%d", e.sampleRate),
				"channels":    fmt.Sprintf("%d", e.channels),
				"bit_depth":   fmt.Sprintf("%d", e.bitDepth),
			},
		}

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// ReadChunks reads audio from an io.Reader and creates chunks on-the-fly
func (e *AudioEncoder) ReadChunks(ctx context.Context, reader io.Reader) (<-chan *types.MediaChunk, <-chan error) {
	chunkCh := make(chan *types.MediaChunk)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		buffer := make([]byte, e.chunkSize)
		sequenceNum := int64(0)
		var previousChunk *types.MediaChunk

		for {
			if e.checkContextDone(ctx, errCh) {
				return
			}

			n, err := io.ReadFull(reader, buffer)

			// Send previous chunk if we have one
			if previousChunk != nil {
				e.markLastChunkIfEOF(previousChunk, err, n)
				if !e.sendChunk(ctx, chunkCh, errCh, previousChunk) {
					return
				}
				sequenceNum++
				previousChunk = nil
			}

			// Handle read error
			if err != nil {
				e.handleReadError(ctx, chunkCh, errCh, err, n, buffer, sequenceNum)
				return
			}

			// Store chunk for next iteration (to determine if it's the last)
			previousChunk = e.createChunk(buffer, n, sequenceNum, false)
		}
	}()

	return chunkCh, errCh
}

// checkContextDone checks if context is done and sends error if so
func (e *AudioEncoder) checkContextDone(ctx context.Context, errCh chan<- error) bool {
	select {
	case <-ctx.Done():
		errCh <- ctx.Err()
		return true
	default:
		return false
	}
}

// markLastChunkIfEOF marks a chunk as last if we hit EOF
func (e *AudioEncoder) markLastChunkIfEOF(chunk *types.MediaChunk, err error, n int) {
	if err == io.EOF || (err == io.ErrUnexpectedEOF && n == 0) {
		chunk.IsLast = true
	}
}

// sendChunk sends a chunk to the channel, returning false if context is done
func (e *AudioEncoder) sendChunk(ctx context.Context, chunkCh chan<- *types.MediaChunk, errCh chan<- error, chunk *types.MediaChunk) bool {
	select {
	case chunkCh <- chunk:
		return true
	case <-ctx.Done():
		errCh <- ctx.Err()
		return false
	}
}

// handleReadError handles errors from reading
func (e *AudioEncoder) handleReadError(ctx context.Context, chunkCh chan<- *types.MediaChunk, errCh chan<- error, err error, n int, buffer []byte, sequenceNum int64) {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		// Send final chunk if we have partial data
		if n > 0 {
			chunk := e.createChunk(buffer, n, sequenceNum, true)
			e.sendChunk(ctx, chunkCh, errCh, chunk)
		}
		return
	}
	errCh <- fmt.Errorf("failed to read audio data: %w", err)
}

// createChunk creates a MediaChunk with the given data
func (e *AudioEncoder) createChunk(buffer []byte, n int, sequenceNum int64, isLast bool) *types.MediaChunk {
	chunk := &types.MediaChunk{
		Data:        make([]byte, n),
		SequenceNum: sequenceNum,
		IsLast:      isLast,
		Metadata: map[string]string{
			"format":      "pcm_linear16",
			"sample_rate": fmt.Sprintf("%d", e.sampleRate),
			"channels":    fmt.Sprintf("%d", e.channels),
			"bit_depth":   fmt.Sprintf("%d", e.bitDepth),
		},
	}
	copy(chunk.Data, buffer[:n])
	return chunk
} // AssembleChunks reassembles MediaChunks back into continuous PCM data
func (e *AudioEncoder) AssembleChunks(chunks []*types.MediaChunk) ([]byte, error) {
	if len(chunks) == 0 {
		return nil, ErrEmptyAudioData
	}

	// Calculate total size
	totalSize := 0
	for _, chunk := range chunks {
		totalSize += len(chunk.Data)
	}

	// Assemble data
	result := make([]byte, 0, totalSize)
	for _, chunk := range chunks {
		result = append(result, chunk.Data...)
	}

	return result, nil
}

// ConvertInt16ToPCM converts []int16 samples to PCM bytes (little-endian)
func (e *AudioEncoder) ConvertInt16ToPCM(samples []int16) []byte {
	pcmData := make([]byte, len(samples)*bytesPerSample)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(pcmData[i*bytesPerSample:], uint16(sample))
	}
	return pcmData
}

// ConvertPCMToInt16 converts PCM bytes to []int16 samples (little-endian)
func (e *AudioEncoder) ConvertPCMToInt16(pcmData []byte) ([]int16, error) {
	if len(pcmData)%bytesPerSample != 0 {
		return nil, fmt.Errorf("PCM data size %d not aligned to sample size %d", len(pcmData), bytesPerSample)
	}

	samples := make([]int16, len(pcmData)/bytesPerSample)
	for i := 0; i < len(samples); i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(pcmData[i*bytesPerSample:]))
	}
	return samples, nil
}

// GenerateSineWave generates PCM audio for a sine wave (useful for testing)
func (e *AudioEncoder) GenerateSineWave(frequency float64, durationMs int, amplitude float64) []byte {
	if amplitude > 1.0 {
		amplitude = 1.0
	}

	numSamples := (e.sampleRate * durationMs) / 1000
	samples := make([]int16, numSamples)

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(e.sampleRate)
		value := amplitude * math.Sin(2*math.Pi*frequency*t)
		samples[i] = int16(value * math.MaxInt16)
	}

	return e.ConvertInt16ToPCM(samples)
}

// GetChunkDurationMs calculates the duration of a chunk in milliseconds
func (e *AudioEncoder) GetChunkDurationMs(chunkSize int) float64 {
	samplesPerChunk := chunkSize / bytesPerSample
	return float64(samplesPerChunk) / float64(e.sampleRate) * 1000.0
}

// GetChunkSize returns the configured chunk size in bytes
func (e *AudioEncoder) GetChunkSize() int {
	return e.chunkSize
}

// GetSampleRate returns the configured sample rate
func (e *AudioEncoder) GetSampleRate() int {
	return e.sampleRate
}

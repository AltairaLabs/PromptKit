package types

import (
	"context"
	"fmt"
	"io"
	"time"
)

// MediaChunk represents a chunk of streaming media data.
// Used for bidirectional streaming where media is sent or received in chunks.
//
// Example usage:
//
//	chunk := &MediaChunk{
//	    Data:        audioData,
//	    SequenceNum: 1,
//	    Timestamp:   time.Now(),
//	    IsLast:      false,
//	    Metadata:    map[string]string{"mime_type": "audio/pcm"},
//	}
type MediaChunk struct {
	// Data contains the raw media bytes for this chunk
	Data []byte `json:"data"`

	// SequenceNum is the sequence number for ordering chunks (starts at 0)
	SequenceNum int64 `json:"sequence_num"`

	// Timestamp indicates when this chunk was created
	Timestamp time.Time `json:"timestamp"`

	// IsLast indicates if this is the final chunk in the stream
	IsLast bool `json:"is_last"`

	// Metadata contains chunk-specific metadata (MIME type, encoding, etc.)
	Metadata map[string]string `json:"metadata,omitempty"`
}

// StreamingMediaConfig configures streaming media input parameters.
// Used to configure audio/video streaming sessions with providers.
//
// Example usage for audio streaming:
//
//	config := &StreamingMediaConfig{
//	    Type:       ContentTypeAudio,
//	    ChunkSize:  8192,    // 8KB chunks
//	    SampleRate: 16000,   // 16kHz audio
//	    Encoding:   "pcm",   // Raw PCM audio
//	    Channels:   1,       // Mono
//	    BufferSize: 10,      // Buffer 10 chunks
//	}
type StreamingMediaConfig struct {
	// Type specifies the media type being streamed
	// Values: ContentTypeAudio, ContentTypeVideo
	Type string `json:"type"`

	// ChunkSize is the target size in bytes for each chunk
	// Typical values: 4096-8192 for audio, 32768-65536 for video
	ChunkSize int `json:"chunk_size"`

	// --- Audio-specific configuration ---

	// SampleRate is the audio sample rate in Hz
	// Common values: 8000 (phone quality), 16000 (wideband), 44100 (CD quality), 48000 (pro audio)
	SampleRate int `json:"sample_rate,omitempty"`

	// Encoding specifies the audio encoding format
	// Values: "pcm" (raw), "opus", "mp3", "aac"
	Encoding string `json:"encoding,omitempty"`

	// Channels is the number of audio channels
	// Values: 1 (mono), 2 (stereo)
	Channels int `json:"channels,omitempty"`

	// BitDepth is the audio bit depth in bits
	// Common values: 16, 24, 32
	BitDepth int `json:"bit_depth,omitempty"`

	// --- Video-specific configuration ---

	// Width is the video width in pixels
	Width int `json:"width,omitempty"`

	// Height is the video height in pixels
	Height int `json:"height,omitempty"`

	// FrameRate is the video frame rate (FPS)
	// Common values: 24, 30, 60
	FrameRate int `json:"frame_rate,omitempty"`

	// --- Streaming behavior configuration ---

	// BufferSize is the maximum number of chunks to buffer
	// Larger values increase latency but provide more stability
	// Typical values: 5-20
	BufferSize int `json:"buffer_size,omitempty"`

	// FlushInterval is how often to flush buffered data (if applicable)
	FlushInterval time.Duration `json:"flush_interval,omitempty"`

	// Metadata contains additional provider-specific configuration
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Validate checks if the StreamingMediaConfig is valid
func (c *StreamingMediaConfig) Validate() error {
	// Type must be set
	if c.Type == "" {
		return fmt.Errorf("streaming media type is required")
	}

	// Type must be audio or video
	if c.Type != ContentTypeAudio && c.Type != ContentTypeVideo {
		return fmt.Errorf("streaming media type must be audio or video, got: %s", c.Type)
	}

	// ChunkSize must be positive
	if c.ChunkSize <= 0 {
		return fmt.Errorf("chunk size must be positive, got: %d", c.ChunkSize)
	}

	// Audio-specific validation
	if c.Type == ContentTypeAudio {
		if c.SampleRate <= 0 {
			return fmt.Errorf("sample rate must be positive for audio, got: %d", c.SampleRate)
		}
		if c.Channels <= 0 {
			return fmt.Errorf("channels must be positive for audio, got: %d", c.Channels)
		}
		if c.Encoding == "" {
			return fmt.Errorf("encoding is required for audio")
		}
	}

	// Video-specific validation
	if c.Type == ContentTypeVideo {
		if c.Width <= 0 {
			return fmt.Errorf("width must be positive for video, got: %d", c.Width)
		}
		if c.Height <= 0 {
			return fmt.Errorf("height must be positive for video, got: %d", c.Height)
		}
		if c.FrameRate <= 0 {
			return fmt.Errorf("frame rate must be positive for video, got: %d", c.FrameRate)
		}
	}

	return nil
}

// ChunkReader reads from an io.Reader and produces MediaChunks.
// Useful for converting continuous streams (e.g., microphone input) into chunks.
//
// Example usage:
//
//	reader := NewChunkReader(micInput, config)
//	for {
//	    chunk, err := reader.NextChunk(ctx)
//	    if err == io.EOF {
//	        break
//	    }
//	    if err != nil {
//	        return err
//	    }
//	    session.SendChunk(ctx, chunk)
//	}
type ChunkReader struct {
	reader io.Reader
	config StreamingMediaConfig
	seqNum int64
	buffer []byte
}

// NewChunkReader creates a new ChunkReader that reads from the given reader
// and produces MediaChunks according to the config.
func NewChunkReader(r io.Reader, config StreamingMediaConfig) *ChunkReader {
	return &ChunkReader{
		reader: r,
		config: config,
		seqNum: 0,
		buffer: make([]byte, config.ChunkSize),
	}
}

// NextChunk reads the next chunk from the reader.
// Returns io.EOF when the stream is complete.
// The returned chunk's IsLast field will be true on the final chunk.
func (cr *ChunkReader) NextChunk(ctx context.Context) (*MediaChunk, error) {
	// Check context first
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Read up to chunk size
	n, err := cr.reader.Read(cr.buffer)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("error reading chunk: %w", err)
	}

	// If we read nothing and got EOF, we're done
	if n == 0 && err == io.EOF {
		return nil, io.EOF
	}

	// Determine if this is the last chunk
	// We got EOF, so this is the last chunk
	isLast := err == io.EOF

	// Create chunk with the data we read
	chunk := &MediaChunk{
		Data:        make([]byte, n),
		SequenceNum: cr.seqNum,
		Timestamp:   time.Now(),
		IsLast:      isLast,
		Metadata: map[string]string{
			"type":     cr.config.Type,
			"encoding": cr.config.Encoding,
		},
	}
	copy(chunk.Data, cr.buffer[:n])

	// Increment sequence number
	cr.seqNum++

	return chunk, nil
}

// ChunkWriter writes MediaChunks to an io.Writer.
// Useful for converting chunks back into continuous streams (e.g., speaker output).
//
// Example usage:
//
//	writer := NewChunkWriter(speakerOutput)
//	for chunk := range session.Response() {
//	    if chunk.MediaDelta != nil {
//	        err := writer.WriteChunk(chunk.MediaDelta)
//	        if err != nil {
//	            return err
//	        }
//	    }
//	}
type ChunkWriter struct {
	writer io.Writer
	buffer []byte
}

// NewChunkWriter creates a new ChunkWriter that writes to the given writer.
func NewChunkWriter(w io.Writer) *ChunkWriter {
	return &ChunkWriter{
		writer: w,
		buffer: make([]byte, 0, 65536), // 64KB buffer
	}
}

// WriteChunk writes a MediaChunk to the underlying writer.
// Returns the number of bytes written and any error encountered.
func (cw *ChunkWriter) WriteChunk(chunk *MediaChunk) (int, error) {
	if chunk == nil || len(chunk.Data) == 0 {
		return 0, nil
	}

	n, err := cw.writer.Write(chunk.Data)
	if err != nil {
		return n, fmt.Errorf("error writing chunk: %w", err)
	}

	return n, nil
}

// Flush flushes any buffered data to the underlying writer (if it supports flushing).
func (cw *ChunkWriter) Flush() error {
	if flusher, ok := cw.writer.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	return nil
}

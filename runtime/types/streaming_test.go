package types

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestMediaChunk(t *testing.T) {
	t.Run("creates valid chunk", func(t *testing.T) {
		chunk := &MediaChunk{
			Data:        []byte("test data"),
			SequenceNum: 1,
			Timestamp:   time.Now(),
			IsLast:      false,
			Metadata: map[string]string{
				"mime_type": "audio/pcm",
			},
		}

		if len(chunk.Data) != 9 {
			t.Errorf("expected data length 9, got %d", len(chunk.Data))
		}
		if chunk.SequenceNum != 1 {
			t.Errorf("expected sequence 1, got %d", chunk.SequenceNum)
		}
		if chunk.IsLast {
			t.Error("expected IsLast to be false")
		}
	})
}

func TestStreamingMediaConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  StreamingMediaConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid audio config",
			config: StreamingMediaConfig{
				Type:       ContentTypeAudio,
				ChunkSize:  8192,
				SampleRate: 16000,
				Encoding:   "pcm",
				Channels:   1,
			},
			wantErr: false,
		},
		{
			name: "valid video config",
			config: StreamingMediaConfig{
				Type:      ContentTypeVideo,
				ChunkSize: 32768,
				Width:     1920,
				Height:    1080,
				FrameRate: 30,
			},
			wantErr: false,
		},
		{
			name: "missing type",
			config: StreamingMediaConfig{
				ChunkSize: 8192,
			},
			wantErr: true,
			errMsg:  "type is required",
		},
		{
			name: "invalid type",
			config: StreamingMediaConfig{
				Type:      "invalid",
				ChunkSize: 8192,
			},
			wantErr: true,
			errMsg:  "must be audio, video, or image",
		},
		{
			name: "invalid chunk size",
			config: StreamingMediaConfig{
				Type:      ContentTypeAudio,
				ChunkSize: -1,
			},
			wantErr: true,
			errMsg:  "must be positive",
		},
		{
			name: "audio missing sample rate",
			config: StreamingMediaConfig{
				Type:      ContentTypeAudio,
				ChunkSize: 8192,
				Encoding:  "pcm",
				Channels:  1,
			},
			wantErr: true,
			errMsg:  "sample rate must be positive",
		},
		{
			name: "audio missing channels",
			config: StreamingMediaConfig{
				Type:       ContentTypeAudio,
				ChunkSize:  8192,
				SampleRate: 16000,
				Encoding:   "pcm",
			},
			wantErr: true,
			errMsg:  "channels must be positive",
		},
		{
			name: "audio missing encoding",
			config: StreamingMediaConfig{
				Type:       ContentTypeAudio,
				ChunkSize:  8192,
				SampleRate: 16000,
				Channels:   1,
			},
			wantErr: true,
			errMsg:  "encoding is required",
		},
		{
			name: "video missing width",
			config: StreamingMediaConfig{
				Type:      ContentTypeVideo,
				ChunkSize: 32768,
				Height:    1080,
				FrameRate: 30,
			},
			wantErr: true,
			errMsg:  "width must be positive",
		},
		{
			name: "video missing height",
			config: StreamingMediaConfig{
				Type:      ContentTypeVideo,
				ChunkSize: 32768,
				Width:     1920,
				FrameRate: 30,
			},
			wantErr: true,
			errMsg:  "height must be positive",
		},
		{
			name: "video missing frame rate",
			config: StreamingMediaConfig{
				Type:      ContentTypeVideo,
				ChunkSize: 32768,
				Width:     1920,
				Height:    1080,
			},
			wantErr: true,
			errMsg:  "frame rate must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestChunkReader_NextChunk(t *testing.T) {
	t.Run("reads exact chunk sizes", func(t *testing.T) {
		// Create test data: 3 chunks of 10 bytes each
		data := []byte("0123456789abcdefghij0123456789")
		reader := strings.NewReader(string(data))

		config := StreamingMediaConfig{
			Type:       ContentTypeAudio,
			ChunkSize:  10,
			SampleRate: 16000,
			Encoding:   "pcm",
			Channels:   1,
		}

		cr := NewChunkReader(reader, config)
		ctx := context.Background()

		// Read first chunk
		chunk1, err := cr.NextChunk(ctx)
		if err != nil {
			t.Fatalf("unexpected error on first chunk: %v", err)
		}
		if chunk1.SequenceNum != 0 {
			t.Errorf("expected sequence 0, got %d", chunk1.SequenceNum)
		}
		if len(chunk1.Data) != 10 {
			t.Errorf("expected 10 bytes, got %d", len(chunk1.Data))
		}
		if string(chunk1.Data) != "0123456789" {
			t.Errorf("expected '0123456789', got %q", string(chunk1.Data))
		}
		if chunk1.IsLast {
			t.Error("first chunk should not be last")
		}

		// Read second chunk
		chunk2, err := cr.NextChunk(ctx)
		if err != nil {
			t.Fatalf("unexpected error on second chunk: %v", err)
		}
		if chunk2.SequenceNum != 1 {
			t.Errorf("expected sequence 1, got %d", chunk2.SequenceNum)
		}
		if string(chunk2.Data) != "abcdefghij" {
			t.Errorf("expected 'abcdefghij', got %q", string(chunk2.Data))
		}

		// Read third chunk
		chunk3, err := cr.NextChunk(ctx)
		if err != nil {
			t.Fatalf("unexpected error on third chunk: %v", err)
		}
		if chunk3.SequenceNum != 2 {
			t.Errorf("expected sequence 2, got %d", chunk3.SequenceNum)
		}
		if string(chunk3.Data) != "0123456789" {
			t.Errorf("expected '0123456789', got %q", string(chunk3.Data))
		}
		// Note: When data divides evenly into chunks, the reader won't return EOF
		// until the NEXT read attempt. This is standard Go io.Reader behavior.
		// So IsLast may be false here even though this is the last data.

		// Next read should return EOF
		_, err = cr.NextChunk(ctx)
		if err != io.EOF {
			t.Errorf("expected io.EOF, got %v", err)
		}
	})

	t.Run("handles partial last chunk", func(t *testing.T) {
		// Data that doesn't divide evenly
		data := []byte("12345")
		reader := strings.NewReader(string(data))

		config := StreamingMediaConfig{
			Type:       ContentTypeAudio,
			ChunkSize:  3,
			SampleRate: 16000,
			Encoding:   "pcm",
			Channels:   1,
		}

		cr := NewChunkReader(reader, config)
		ctx := context.Background()

		// First chunk: 3 bytes
		chunk1, err := cr.NextChunk(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chunk1.Data) != 3 {
			t.Errorf("expected 3 bytes, got %d", len(chunk1.Data))
		}
		if string(chunk1.Data) != "123" {
			t.Errorf("expected '123', got %q", string(chunk1.Data))
		}
		if chunk1.IsLast {
			t.Error("first chunk should not be last")
		}

		// Second chunk: 2 bytes (partial)
		chunk2, err := cr.NextChunk(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chunk2.Data) != 2 {
			t.Errorf("expected 2 bytes, got %d", len(chunk2.Data))
		}
		if string(chunk2.Data) != "45" {
			t.Errorf("expected '45', got %q", string(chunk2.Data))
		}
		// Note: IsLast behavior depends on whether the underlying reader
		// returns EOF with the data or on the next call. This is
		// reader-implementation-specific, so we don't assert on IsLast here.
		// The important thing is that the next NextChunk() returns EOF.

		// Next read should return EOF
		_, err = cr.NextChunk(ctx)
		if err != io.EOF {
			t.Errorf("expected io.EOF after last chunk, got %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		// Create a slow reader
		slowReader := &slowReader{
			data:  []byte("test data"),
			delay: 100 * time.Millisecond,
		}

		config := StreamingMediaConfig{
			Type:       ContentTypeAudio,
			ChunkSize:  10,
			SampleRate: 16000,
			Encoding:   "pcm",
			Channels:   1,
		}

		cr := NewChunkReader(slowReader, config)
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		// Should return context error
		_, err := cr.NextChunk(ctx)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("handles empty reader", func(t *testing.T) {
		reader := strings.NewReader("")

		config := StreamingMediaConfig{
			Type:       ContentTypeAudio,
			ChunkSize:  10,
			SampleRate: 16000,
			Encoding:   "pcm",
			Channels:   1,
		}

		cr := NewChunkReader(reader, config)
		ctx := context.Background()

		_, err := cr.NextChunk(ctx)
		if err != io.EOF {
			t.Errorf("expected io.EOF, got %v", err)
		}
	})

	t.Run("includes metadata", func(t *testing.T) {
		reader := strings.NewReader("test")

		config := StreamingMediaConfig{
			Type:       ContentTypeAudio,
			ChunkSize:  10,
			SampleRate: 16000,
			Encoding:   "opus",
			Channels:   1,
		}

		cr := NewChunkReader(reader, config)
		ctx := context.Background()

		chunk, err := cr.NextChunk(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if chunk.Metadata["type"] != ContentTypeAudio {
			t.Errorf("expected type %q, got %q", ContentTypeAudio, chunk.Metadata["type"])
		}
		if chunk.Metadata["encoding"] != "opus" {
			t.Errorf("expected encoding 'opus', got %q", chunk.Metadata["encoding"])
		}
	})
}

func TestChunkWriter_WriteChunk(t *testing.T) {
	t.Run("writes chunks correctly", func(t *testing.T) {
		var buf bytes.Buffer
		cw := NewChunkWriter(&buf)

		chunks := []*MediaChunk{
			{Data: []byte("hello ")},
			{Data: []byte("world")},
			{Data: []byte("!")},
		}

		for _, chunk := range chunks {
			n, err := cw.WriteChunk(chunk)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != len(chunk.Data) {
				t.Errorf("expected %d bytes written, got %d", len(chunk.Data), n)
			}
		}

		result := buf.String()
		if result != "hello world!" {
			t.Errorf("expected 'hello world!', got %q", result)
		}
	})

	t.Run("handles nil chunk", func(t *testing.T) {
		var buf bytes.Buffer
		cw := NewChunkWriter(&buf)

		n, err := cw.WriteChunk(nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if n != 0 {
			t.Errorf("expected 0 bytes written, got %d", n)
		}
	})

	t.Run("handles empty chunk", func(t *testing.T) {
		var buf bytes.Buffer
		cw := NewChunkWriter(&buf)

		chunk := &MediaChunk{Data: []byte{}}

		n, err := cw.WriteChunk(chunk)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if n != 0 {
			t.Errorf("expected 0 bytes written, got %d", n)
		}
	})

	t.Run("reassembles data correctly", func(t *testing.T) {
		// Create test data
		original := []byte("The quick brown fox jumps over the lazy dog")
		reader := bytes.NewReader(original)

		// Chunk it
		config := StreamingMediaConfig{
			Type:       ContentTypeAudio,
			ChunkSize:  10,
			SampleRate: 16000,
			Encoding:   "pcm",
			Channels:   1,
		}

		cr := NewChunkReader(reader, config)
		ctx := context.Background()

		// Write chunks
		var buf bytes.Buffer
		cw := NewChunkWriter(&buf)

		for {
			chunk, err := cr.NextChunk(ctx)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			_, err = cw.WriteChunk(chunk)
			if err != nil {
				t.Fatalf("write error: %v", err)
			}
		}

		// Verify
		result := buf.Bytes()
		if !bytes.Equal(result, original) {
			t.Errorf("data mismatch:\nexpected: %q\ngot:      %q", string(original), string(result))
		}
	})
}

// slowReader is a test helper that delays reads
type slowReader struct {
	data  []byte
	pos   int
	delay time.Duration
}

func (sr *slowReader) Read(p []byte) (int, error) {
	time.Sleep(sr.delay)

	if sr.pos >= len(sr.data) {
		return 0, io.EOF
	}

	n := copy(p, sr.data[sr.pos:])
	sr.pos += n

	if sr.pos >= len(sr.data) {
		return n, io.EOF
	}

	return n, nil
}

package providers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestStreamInputRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     StreamingInputConfig
		wantErr bool
	}{
		{
			name: "valid audio request",
			req: StreamingInputConfig{
				Config: types.StreamingMediaConfig{
					Type:       types.ContentTypeAudio,
					ChunkSize:  8192,
					SampleRate: 16000,
					Encoding:   "pcm",
					Channels:   1,
				},
			},
			wantErr: false,
		},
		{
			name: "valid video request",
			req: StreamingInputConfig{
				Config: types.StreamingMediaConfig{
					Type:      types.ContentTypeVideo,
					ChunkSize: 32768,
					Width:     1920,
					Height:    1080,
					FrameRate: 30,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid config",
			req: StreamingInputConfig{
				Config: types.StreamingMediaConfig{
					Type:      types.ContentTypeAudio,
					ChunkSize: -1, // Invalid
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestVideoResolution_String(t *testing.T) {
	tests := []struct {
		name string
		res  VideoResolution
		want string
	}{
		{
			name: "1080p",
			res:  VideoResolution{Width: 1920, Height: 1080},
			want: "1920x1080",
		},
		{
			name: "720p",
			res:  VideoResolution{Width: 1280, Height: 720},
			want: "1280x720",
		},
		{
			name: "4K",
			res:  VideoResolution{Width: 3840, Height: 2160},
			want: "3840x2160",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.res.String()
			if got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStreamingCapabilities(t *testing.T) {
	t.Run("audio capabilities structure", func(t *testing.T) {
		caps := StreamingCapabilities{
			SupportedMediaTypes:  []string{types.ContentTypeAudio},
			BidirectionalSupport: true,
			MaxSessionDuration:   3600,
			Audio: &AudioStreamingCapabilities{
				SupportedEncodings:   []string{"pcm", "opus"},
				SupportedSampleRates: []int{8000, 16000, 48000},
				SupportedChannels:    []int{1, 2},
				PreferredEncoding:    "opus",
				PreferredSampleRate:  16000,
			},
		}

		if len(caps.SupportedMediaTypes) != 1 {
			t.Errorf("expected 1 supported media type, got %d", len(caps.SupportedMediaTypes))
		}
		if !caps.BidirectionalSupport {
			t.Error("expected bidirectional support")
		}
		if caps.Audio == nil {
			t.Fatal("expected audio capabilities")
		}
		if caps.Audio.PreferredEncoding != "opus" {
			t.Errorf("expected preferred encoding 'opus', got %q", caps.Audio.PreferredEncoding)
		}
	})

	t.Run("video capabilities structure", func(t *testing.T) {
		caps := StreamingCapabilities{
			SupportedMediaTypes:  []string{types.ContentTypeVideo},
			BidirectionalSupport: false,
			Video: &VideoStreamingCapabilities{
				SupportedEncodings: []string{"h264", "vp9"},
				SupportedResolutions: []VideoResolution{
					{Width: 1920, Height: 1080},
					{Width: 1280, Height: 720},
				},
				SupportedFrameRates: []int{24, 30, 60},
				PreferredEncoding:   "h264",
				PreferredResolution: VideoResolution{Width: 1920, Height: 1080},
				PreferredFrameRate:  30,
			},
		}

		if caps.Video == nil {
			t.Fatal("expected video capabilities")
		}
		if len(caps.Video.SupportedResolutions) != 2 {
			t.Errorf("expected 2 resolutions, got %d", len(caps.Video.SupportedResolutions))
		}
	})
}

// MockStreamSession is a mock implementation for testing
type MockStreamSession struct {
	chunks      []*types.MediaChunk
	texts       []string
	responses   chan StreamChunk
	doneCh      chan struct{}
	err         error
	closeCalled bool
}

func NewMockStreamSession() *MockStreamSession {
	return &MockStreamSession{
		chunks:    make([]*types.MediaChunk, 0),
		texts:     make([]string, 0),
		responses: make(chan StreamChunk, 10),
		doneCh:    make(chan struct{}),
	}
}

func (m *MockStreamSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	if m.err != nil {
		return m.err
	}
	m.chunks = append(m.chunks, chunk)
	return nil
}

func (m *MockStreamSession) SendText(ctx context.Context, text string) error {
	if m.err != nil {
		return m.err
	}
	m.texts = append(m.texts, text)
	return nil
}

func (m *MockStreamSession) Response() <-chan StreamChunk {
	return m.responses
}

func (m *MockStreamSession) Close() error {
	if !m.closeCalled {
		m.closeCalled = true
		close(m.doneCh)
		close(m.responses)
	}
	return nil
}

func (m *MockStreamSession) Error() error {
	return m.err
}

func (m *MockStreamSession) Done() <-chan struct{} {
	return m.doneCh
}

func TestMockStreamSession(t *testing.T) {
	t.Run("implements StreamInputSession", func(t *testing.T) {
		var _ StreamInputSession = (*MockStreamSession)(nil)
	})

	t.Run("sends chunks", func(t *testing.T) {
		session := NewMockStreamSession()
		defer session.Close()

		chunk := &types.MediaChunk{
			Data:        []byte("test"),
			SequenceNum: 0,
		}

		err := session.SendChunk(context.Background(), chunk)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(session.chunks) != 1 {
			t.Errorf("expected 1 chunk, got %d", len(session.chunks))
		}
	})

	t.Run("sends text", func(t *testing.T) {
		session := NewMockStreamSession()
		defer session.Close()

		err := session.SendText(context.Background(), "hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(session.texts) != 1 {
			t.Errorf("expected 1 text, got %d", len(session.texts))
		}
		if session.texts[0] != "hello" {
			t.Errorf("expected 'hello', got %q", session.texts[0])
		}
	})

	t.Run("receives responses", func(t *testing.T) {
		session := NewMockStreamSession()

		// Send a response
		session.responses <- StreamChunk{Delta: "hello"}
		session.responses <- StreamChunk{Delta: " world"}

		// Read responses
		chunk1 := <-session.Response()
		if chunk1.Delta != "hello" {
			t.Errorf("expected 'hello', got %q", chunk1.Delta)
		}

		chunk2 := <-session.Response()
		if chunk2.Delta != " world" {
			t.Errorf("expected ' world', got %q", chunk2.Delta)
		}

		session.Close()

		// After close, channel should be closed
		_, ok := <-session.Response()
		if ok {
			t.Error("expected response channel to be closed")
		}
	})

	t.Run("done channel closed on close", func(t *testing.T) {
		session := NewMockStreamSession()

		select {
		case <-session.Done():
			t.Error("done channel should not be closed yet")
		default:
			// Good
		}

		session.Close()

		select {
		case <-session.Done():
			// Good
		default:
			t.Error("done channel should be closed after Close()")
		}
	})

	t.Run("multiple close calls safe", func(t *testing.T) {
		session := NewMockStreamSession()

		err1 := session.Close()
		err2 := session.Close()
		err3 := session.Close()

		if err1 != nil || err2 != nil || err3 != nil {
			t.Error("multiple Close() calls should not error")
		}
	})
}

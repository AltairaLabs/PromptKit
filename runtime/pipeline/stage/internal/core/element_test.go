package core

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
)

// mockMediaStorageService is a mock implementation for testing externalization.
type mockMediaStorageService struct {
	storeFunc    func(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error)
	retrieveFunc func(ctx context.Context, reference storage.Reference) (*types.MediaContent, error)
	deleteFunc   func(ctx context.Context, reference storage.Reference) error
	getURLFunc   func(ctx context.Context, reference storage.Reference, expiry time.Duration) (string, error)
}

func (m *mockMediaStorageService) StoreMedia(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
	if m.storeFunc != nil {
		return m.storeFunc(ctx, content, metadata)
	}
	return "", nil
}

func (m *mockMediaStorageService) RetrieveMedia(ctx context.Context, reference storage.Reference) (*types.MediaContent, error) {
	if m.retrieveFunc != nil {
		return m.retrieveFunc(ctx, reference)
	}
	return nil, nil
}

func (m *mockMediaStorageService) DeleteMedia(ctx context.Context, reference storage.Reference) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, reference)
	}
	return nil
}

func (m *mockMediaStorageService) GetURL(ctx context.Context, reference storage.Reference, expiry time.Duration) (string, error) {
	if m.getURLFunc != nil {
		return m.getURLFunc(ctx, reference, expiry)
	}
	return "", nil
}

func TestVideoData_IsExternalized(t *testing.T) {
	tests := []struct {
		name     string
		video    VideoData
		expected bool
	}{
		{
			name:     "not externalized - has data",
			video:    VideoData{Data: []byte{1, 2, 3}},
			expected: false,
		},
		{
			name:     "not externalized - no ref",
			video:    VideoData{},
			expected: false,
		},
		{
			name:     "externalized - has ref, no data",
			video:    VideoData{StorageRef: "ref-123"},
			expected: true,
		},
		{
			name:     "not externalized - has both",
			video:    VideoData{Data: []byte{1, 2, 3}, StorageRef: "ref-123"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.video.IsExternalized()
			if got != tt.expected {
				t.Errorf("IsExternalized() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestVideoData_Load(t *testing.T) {
	ctx := context.Background()
	testData := []byte{1, 2, 3, 4, 5}
	encodedData := base64.StdEncoding.EncodeToString(testData)

	t.Run("load externalized video", func(t *testing.T) {
		mock := &mockMediaStorageService{
			retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
				if ref != "video-ref-123" {
					t.Errorf("unexpected reference: %v", ref)
				}
				return &types.MediaContent{
					Data:     &encodedData,
					MIMEType: "video/mp4",
				}, nil
			},
		}

		video := &VideoData{StorageRef: "video-ref-123", MIMEType: "video/mp4"}
		err := video.Load(ctx, mock)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if len(video.Data) != len(testData) {
			t.Errorf("Expected data length %d, got %d", len(testData), len(video.Data))
		}
	})

	t.Run("skip if not externalized", func(t *testing.T) {
		video := &VideoData{Data: []byte{1, 2, 3}}
		err := video.Load(ctx, nil) // nil store should not be called
		if err != nil {
			t.Fatalf("Load should skip when not externalized: %v", err)
		}
	})

	t.Run("error on retrieve failure", func(t *testing.T) {
		mock := &mockMediaStorageService{
			retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
				return nil, errors.New("storage error")
			},
		}

		video := &VideoData{StorageRef: "video-ref-123"}
		err := video.Load(ctx, mock)
		if err == nil {
			t.Error("Expected error on retrieve failure")
		}
	})
}

func TestVideoData_Externalize(t *testing.T) {
	ctx := context.Background()
	testData := []byte{1, 2, 3, 4, 5}

	t.Run("externalize video data", func(t *testing.T) {
		var storedContent *types.MediaContent
		mock := &mockMediaStorageService{
			storeFunc: func(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
				storedContent = content
				return "new-video-ref", nil
			},
		}

		video := &VideoData{Data: testData, MIMEType: "video/mp4"}
		err := video.Externalize(ctx, mock, nil)
		if err != nil {
			t.Fatalf("Externalize failed: %v", err)
		}
		if video.StorageRef != "new-video-ref" {
			t.Errorf("Expected ref 'new-video-ref', got %v", video.StorageRef)
		}
		if video.Data != nil {
			t.Error("Data should be cleared after externalize")
		}
		if storedContent == nil || storedContent.MIMEType != "video/mp4" {
			t.Error("Content should have correct MIME type")
		}
	})

	t.Run("skip if no data", func(t *testing.T) {
		video := &VideoData{}
		err := video.Externalize(ctx, nil, nil) // nil store should not be called
		if err != nil {
			t.Fatalf("Externalize should skip when no data: %v", err)
		}
	})

	t.Run("error on store failure", func(t *testing.T) {
		mock := &mockMediaStorageService{
			storeFunc: func(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
				return "", errors.New("storage error")
			},
		}

		video := &VideoData{Data: testData, MIMEType: "video/mp4"}
		err := video.Externalize(ctx, mock, nil)
		if err == nil {
			t.Error("Expected error on store failure")
		}
	})
}

func TestVideoData_EnsureLoaded(t *testing.T) {
	ctx := context.Background()
	testData := []byte{1, 2, 3, 4, 5}
	encodedData := base64.StdEncoding.EncodeToString(testData)

	t.Run("ensure loaded returns data", func(t *testing.T) {
		mock := &mockMediaStorageService{
			retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
				return &types.MediaContent{Data: &encodedData}, nil
			},
		}

		video := &VideoData{StorageRef: "ref-123"}
		data, err := video.EnsureLoaded(ctx, mock)
		if err != nil {
			t.Fatalf("EnsureLoaded failed: %v", err)
		}
		if len(data) != len(testData) {
			t.Errorf("Expected data length %d, got %d", len(testData), len(data))
		}
	})
}

func TestImageData_IsExternalized(t *testing.T) {
	tests := []struct {
		name     string
		image    ImageData
		expected bool
	}{
		{
			name:     "not externalized - has data",
			image:    ImageData{Data: []byte{1, 2, 3}},
			expected: false,
		},
		{
			name:     "not externalized - no ref",
			image:    ImageData{},
			expected: false,
		},
		{
			name:     "externalized - has ref, no data",
			image:    ImageData{StorageRef: "ref-123"},
			expected: true,
		},
		{
			name:     "not externalized - has both",
			image:    ImageData{Data: []byte{1, 2, 3}, StorageRef: "ref-123"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.image.IsExternalized()
			if got != tt.expected {
				t.Errorf("IsExternalized() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestImageData_Load(t *testing.T) {
	ctx := context.Background()
	testData := []byte{1, 2, 3, 4, 5}
	encodedData := base64.StdEncoding.EncodeToString(testData)

	t.Run("load externalized image", func(t *testing.T) {
		mock := &mockMediaStorageService{
			retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
				if ref != "image-ref-123" {
					t.Errorf("unexpected reference: %v", ref)
				}
				return &types.MediaContent{
					Data:     &encodedData,
					MIMEType: "image/png",
				}, nil
			},
		}

		image := &ImageData{StorageRef: "image-ref-123", MIMEType: "image/png"}
		err := image.Load(ctx, mock)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if len(image.Data) != len(testData) {
			t.Errorf("Expected data length %d, got %d", len(testData), len(image.Data))
		}
	})

	t.Run("skip if not externalized", func(t *testing.T) {
		image := &ImageData{Data: []byte{1, 2, 3}}
		err := image.Load(ctx, nil) // nil store should not be called
		if err != nil {
			t.Fatalf("Load should skip when not externalized: %v", err)
		}
	})

	t.Run("error on retrieve failure", func(t *testing.T) {
		mock := &mockMediaStorageService{
			retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
				return nil, errors.New("storage error")
			},
		}

		image := &ImageData{StorageRef: "image-ref-123"}
		err := image.Load(ctx, mock)
		if err == nil {
			t.Error("Expected error on retrieve failure")
		}
	})
}

func TestImageData_Externalize(t *testing.T) {
	ctx := context.Background()
	testData := []byte{1, 2, 3, 4, 5}

	t.Run("externalize image data", func(t *testing.T) {
		var storedContent *types.MediaContent
		mock := &mockMediaStorageService{
			storeFunc: func(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
				storedContent = content
				return "new-image-ref", nil
			},
		}

		image := &ImageData{Data: testData, MIMEType: "image/png"}
		err := image.Externalize(ctx, mock, nil)
		if err != nil {
			t.Fatalf("Externalize failed: %v", err)
		}
		if image.StorageRef != "new-image-ref" {
			t.Errorf("Expected ref 'new-image-ref', got %v", image.StorageRef)
		}
		if image.Data != nil {
			t.Error("Data should be cleared after externalize")
		}
		if storedContent == nil || storedContent.MIMEType != "image/png" {
			t.Error("Content should have correct MIME type")
		}
	})

	t.Run("skip if no data", func(t *testing.T) {
		image := &ImageData{}
		err := image.Externalize(ctx, nil, nil) // nil store should not be called
		if err != nil {
			t.Fatalf("Externalize should skip when no data: %v", err)
		}
	})

	t.Run("error on store failure", func(t *testing.T) {
		mock := &mockMediaStorageService{
			storeFunc: func(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
				return "", errors.New("storage error")
			},
		}

		image := &ImageData{Data: testData, MIMEType: "image/png"}
		err := image.Externalize(ctx, mock, nil)
		if err == nil {
			t.Error("Expected error on store failure")
		}
	})
}

func TestImageData_EnsureLoaded(t *testing.T) {
	ctx := context.Background()
	testData := []byte{1, 2, 3, 4, 5}
	encodedData := base64.StdEncoding.EncodeToString(testData)

	t.Run("ensure loaded returns data", func(t *testing.T) {
		mock := &mockMediaStorageService{
			retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
				return &types.MediaContent{Data: &encodedData}, nil
			},
		}

		image := &ImageData{StorageRef: "ref-123"}
		data, err := image.EnsureLoaded(ctx, mock)
		if err != nil {
			t.Fatalf("EnsureLoaded failed: %v", err)
		}
		if len(data) != len(testData) {
			t.Errorf("Expected data length %d, got %d", len(testData), len(data))
		}
	})
}

func TestVideoData_Load_ReadDataError(t *testing.T) {
	ctx := context.Background()
	mock := &mockMediaStorageService{
		retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
			// Return content with nil Data to trigger ReadData error
			return &types.MediaContent{MIMEType: "video/mp4"}, nil
		},
	}

	video := &VideoData{StorageRef: "video-ref-123"}
	err := video.Load(ctx, mock)
	if err == nil {
		t.Error("Expected error when ReadData fails")
	}
}

func TestImageData_Load_ReadDataError(t *testing.T) {
	ctx := context.Background()
	mock := &mockMediaStorageService{
		retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
			// Return content with nil Data to trigger ReadData error
			return &types.MediaContent{MIMEType: "image/png"}, nil
		},
	}

	image := &ImageData{StorageRef: "image-ref-123"}
	err := image.Load(ctx, mock)
	if err == nil {
		t.Error("Expected error when ReadData fails")
	}
}

func TestVideoData_EnsureLoaded_Error(t *testing.T) {
	ctx := context.Background()
	mock := &mockMediaStorageService{
		retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
			return nil, errors.New("storage error")
		},
	}

	video := &VideoData{StorageRef: "video-ref-123"}
	_, err := video.EnsureLoaded(ctx, mock)
	if err == nil {
		t.Error("Expected error to propagate from EnsureLoaded")
	}
}

func TestNewEndOfTurnElement(t *testing.T) {
	e := NewEndOfTurnElement()
	if !e.EndOfTurn {
		t.Fatal("expected EndOfTurn true")
	}
	if e.EndOfStream {
		t.Fatal("EndOfTurn element must not set EndOfStream")
	}
	if e.Priority != PriorityCritical {
		t.Fatalf("expected PriorityCritical, got %v", e.Priority)
	}
	if !e.IsControl() {
		t.Fatal("EndOfTurn element must be a control signal")
	}
	if e.IsEmpty() {
		t.Fatal("EndOfTurn element must not be empty")
	}
}

func TestNewInterruptElement(t *testing.T) {
	e := NewInterruptElement()
	if !e.Interrupt {
		t.Fatal("expected Interrupt true")
	}
	if e.Priority != PriorityCritical {
		t.Fatalf("expected PriorityCritical, got %v", e.Priority)
	}
	if !e.IsControl() {
		t.Fatal("Interrupt element must be a control signal")
	}
	if e.IsEmpty() {
		t.Fatal("Interrupt element must not be empty")
	}
}

func TestControlSignals_DefaultFalse(t *testing.T) {
	e := NewTextElement("hi")
	if e.EndOfTurn || e.Interrupt {
		t.Fatal("new content element must default EndOfTurn/Interrupt to false")
	}
}

func TestImageData_EnsureLoaded_Error(t *testing.T) {
	ctx := context.Background()
	mock := &mockMediaStorageService{
		retrieveFunc: func(ctx context.Context, ref storage.Reference) (*types.MediaContent, error) {
			return nil, errors.New("storage error")
		},
	}

	image := &ImageData{StorageRef: "image-ref-123"}
	_, err := image.EnsureLoaded(ctx, mock)
	if err == nil {
		t.Error("Expected error to propagate from EnsureLoaded")
	}
}

// Note: Tests for NewTextElement, NewMessageElement, NewAudioElement, NewVideoElement,
// NewImageElement, NewErrorElement, NewEndOfStreamElement, StreamElement.IsEmpty,
// StreamElement.HasContent, StreamElement.IsControl, and AudioFormat.String
// are defined in stages_utilities_test.go

func TestStreamElement_ReasoningIsNonContent(t *testing.T) {
	d := "thinking..."
	e := StreamElement{Reasoning: &ReasoningDelta{Text: d}}
	if e.Message != nil || e.Text != nil || e.Part != nil || e.Audio != nil {
		t.Fatal("a reasoning element must carry no content fields")
	}
	if e.Reasoning == nil || e.Reasoning.Text != "thinking..." {
		t.Fatal("reasoning delta not set")
	}
}

func TestAudioFormat_String(t *testing.T) {
	tests := []struct {
		format   AudioFormat
		expected string
	}{
		{AudioFormatPCM16, "pcm16"},
		{AudioFormatFloat32, "float32"},
		{AudioFormatOpus, "opus"},
		{AudioFormatMP3, "mp3"},
		{AudioFormatAAC, "aac"},
		{AudioFormat(99), "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.format.String())
	}
}

func TestNewAudioElement(t *testing.T) {
	audio := &AudioData{
		Samples:    []byte{1, 2, 3, 4},
		SampleRate: 16000,
		Channels:   1,
	}

	elem := NewAudioElement(audio)

	assert.NotNil(t, elem.Audio)
	assert.Equal(t, PriorityHigh, elem.Priority)
}

func TestNewVideoElement(t *testing.T) {
	video := &VideoData{
		Data:     []byte{1, 2, 3, 4},
		MIMEType: "video/mp4",
		Width:    640,
		Height:   480,
	}

	elem := NewVideoElement(video)

	assert.NotNil(t, elem.Video)
	assert.Equal(t, PriorityHigh, elem.Priority)
}

func TestNewImageElement(t *testing.T) {
	image := &ImageData{
		Data:     []byte{1, 2, 3, 4},
		MIMEType: "image/png",
		Width:    100,
		Height:   100,
	}

	elem := NewImageElement(image)

	assert.NotNil(t, elem.Image)
	assert.Equal(t, PriorityNormal, elem.Priority)
}

func TestNewEndOfStreamElement(t *testing.T) {
	elem := NewEndOfStreamElement()

	assert.True(t, elem.EndOfStream)
	assert.Equal(t, PriorityCritical, elem.Priority)
}

func TestStreamElement_IsEmpty(t *testing.T) {
	t.Run("empty element", func(t *testing.T) {
		elem := StreamElement{}
		assert.True(t, elem.IsEmpty())
	})

	t.Run("element with text", func(t *testing.T) {
		elem := NewTextElement("hello")
		assert.False(t, elem.IsEmpty())
	})

	t.Run("element with error", func(t *testing.T) {
		elem := NewErrorElement(assert.AnError)
		assert.False(t, elem.IsEmpty())
	})
}

func TestStreamElement_HasContent(t *testing.T) {
	t.Run("empty element", func(t *testing.T) {
		elem := StreamElement{}
		assert.False(t, elem.HasContent())
	})

	t.Run("element with text", func(t *testing.T) {
		elem := NewTextElement("hello")
		assert.True(t, elem.HasContent())
	})

	t.Run("element with message", func(t *testing.T) {
		msg := &types.Message{Role: "user", Content: "hi"}
		elem := NewMessageElement(msg)
		assert.True(t, elem.HasContent())
	})
}

func TestStreamElement_IsControl(t *testing.T) {
	t.Run("non-control element", func(t *testing.T) {
		elem := NewTextElement("hello")
		assert.False(t, elem.IsControl())
	})

	t.Run("error element", func(t *testing.T) {
		elem := NewErrorElement(assert.AnError)
		assert.True(t, elem.IsControl())
	})

	t.Run("end of stream element", func(t *testing.T) {
		elem := NewEndOfStreamElement()
		assert.True(t, elem.IsControl())
	})
}

func TestStreamElement_WithSource(t *testing.T) {
	elem := NewTextElement("test")
	result := elem.WithSource("my-stage")

	assert.Equal(t, "my-stage", result.Source)
}

func TestStreamElement_WithPriority(t *testing.T) {
	elem := NewTextElement("test")
	result := elem.WithPriority(PriorityCritical)

	assert.Equal(t, PriorityCritical, result.Priority)
}

func TestStreamElement_WithSequence(t *testing.T) {
	elem := NewTextElement("test")
	result := elem.WithSequence(42)

	assert.Equal(t, int64(42), result.Sequence)
}

func TestAudioFormat_BytesPerSample(t *testing.T) {
	tests := []struct {
		format AudioFormat
		want   int
	}{
		{AudioFormatPCM16, 2},
		{AudioFormatFloat32, 4},
		{AudioFormatOpus, 0},
		{AudioFormatMP3, 0},
		{AudioFormatAAC, 0},
		{AudioFormat(99), 0},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.format.BytesPerSample(), tt.format.String())
	}
}

func TestTimeNow_ReadsTheWallClock(t *testing.T) {
	before := time.Now()
	got := TimeNow()
	assert.False(t, got.Before(before))
	assert.False(t, got.After(time.Now()))
}

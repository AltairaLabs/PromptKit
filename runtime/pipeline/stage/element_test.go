package stage

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

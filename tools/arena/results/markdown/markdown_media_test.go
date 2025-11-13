package markdown

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

func TestMarkdownRepository_MediaHelpers(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())

	t.Run("mediaHasData with data", func(t *testing.T) {
		media := &types.MediaContent{
			Data:     ptrString("base64data"),
			MIMEType: "image/jpeg",
		}
		if !repo.mediaHasData(media) {
			t.Error("should return true for media with data")
		}
	})

	t.Run("mediaHasData with filepath", func(t *testing.T) {
		media := &types.MediaContent{
			FilePath: ptrString("/path/to/file.jpg"),
			MIMEType: "image/jpeg",
		}
		if !repo.mediaHasData(media) {
			t.Error("should return true for media with filepath")
		}
	})

	t.Run("mediaHasData with URL", func(t *testing.T) {
		media := &types.MediaContent{
			URL:      ptrString("https://example.com/image.jpg"),
			MIMEType: "image/jpeg",
		}
		if !repo.mediaHasData(media) {
			t.Error("should return true for media with URL")
		}
	})

	t.Run("mediaHasData empty", func(t *testing.T) {
		media := &types.MediaContent{
			MIMEType: "image/jpeg",
		}
		if repo.mediaHasData(media) {
			t.Error("should return false for media without data")
		}
	})

	t.Run("calculateMediaSize with data", func(t *testing.T) {
		media := &types.MediaContent{
			Data:     ptrString("base64encodeddata"),
			MIMEType: "image/jpeg",
		}
		size := repo.calculateMediaSize(media)
		if size != 17 {
			t.Errorf("expected size 17, got %d", size)
		}
	})

	t.Run("calculateMediaSize without data", func(t *testing.T) {
		media := &types.MediaContent{
			FilePath: ptrString("/path"),
			MIMEType: "image/jpeg",
		}
		size := repo.calculateMediaSize(media)
		if size != 0 {
			t.Errorf("expected size 0, got %d", size)
		}
	})
}

func TestMarkdownRepository_CountMediaByType(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())

	tests := []struct {
		name           string
		contentType    string
		expectedImages int
		expectedAudio  int
		expectedVideo  int
	}{
		{"image", "image", 1, 0, 0},
		{"audio", "audio", 0, 1, 0},
		{"video", "video", 0, 0, 1},
		{"text", "text", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := &testSummary{}
			repo.countMediaByType(summary, tt.contentType)

			if summary.TotalImages != tt.expectedImages {
				t.Errorf("TotalImages = %d, want %d", summary.TotalImages, tt.expectedImages)
			}
			if summary.TotalAudio != tt.expectedAudio {
				t.Errorf("TotalAudio = %d, want %d", summary.TotalAudio, tt.expectedAudio)
			}
			if summary.TotalVideo != tt.expectedVideo {
				t.Errorf("TotalVideo = %d, want %d", summary.TotalVideo, tt.expectedVideo)
			}
		})
	}
}

func TestMarkdownRepository_ProcessMediaPart(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())

	tests := []struct {
		name           string
		part           types.ContentPart
		expectedImages int
		expectedAudio  int
		expectedVideo  int
		expectedLoaded int
		expectedErrors int
		expectedSize   int64
	}{
		{
			name: "image with data",
			part: types.ContentPart{
				Type: "image",
				Media: &types.MediaContent{
					Data:     ptrString("imgdata"),
					MIMEType: "image/png",
				},
			},
			expectedImages: 1,
			expectedLoaded: 1,
			expectedSize:   7,
		},
		{
			name: "audio with filepath",
			part: types.ContentPart{
				Type: "audio",
				Media: &types.MediaContent{
					FilePath: ptrString("/path/audio.mp3"),
					MIMEType: "audio/mpeg",
				},
			},
			expectedAudio:  1,
			expectedLoaded: 1,
			expectedSize:   0,
		},
		{
			name: "video with URL",
			part: types.ContentPart{
				Type: "video",
				Media: &types.MediaContent{
					URL:      ptrString("https://example.com/video.mp4"),
					MIMEType: "video/mp4",
				},
			},
			expectedVideo:  1,
			expectedLoaded: 1,
			expectedSize:   0,
		},
		{
			name: "image without data (error)",
			part: types.ContentPart{
				Type: "image",
				Media: &types.MediaContent{
					MIMEType: "image/jpeg",
				},
			},
			expectedImages: 1,
			expectedErrors: 1,
			expectedSize:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := &testSummary{}
			repo.processMediaPart(summary, &tt.part)

			if summary.TotalImages != tt.expectedImages {
				t.Errorf("TotalImages = %d, want %d", summary.TotalImages, tt.expectedImages)
			}
			if summary.TotalAudio != tt.expectedAudio {
				t.Errorf("TotalAudio = %d, want %d", summary.TotalAudio, tt.expectedAudio)
			}
			if summary.TotalVideo != tt.expectedVideo {
				t.Errorf("TotalVideo = %d, want %d", summary.TotalVideo, tt.expectedVideo)
			}
			if summary.MediaLoadSuccess != tt.expectedLoaded {
				t.Errorf("MediaLoadSuccess = %d, want %d", summary.MediaLoadSuccess, tt.expectedLoaded)
			}
			if summary.MediaLoadErrors != tt.expectedErrors {
				t.Errorf("MediaLoadErrors = %d, want %d", summary.MediaLoadErrors, tt.expectedErrors)
			}
			if summary.TotalMediaSize != tt.expectedSize {
				t.Errorf("TotalMediaSize = %d, want %d", summary.TotalMediaSize, tt.expectedSize)
			}
		})
	}
}

func TestMarkdownRepository_AddMediaStats(t *testing.T) {
	repo := NewMarkdownResultRepository(t.TempDir())

	tests := []struct {
		name           string
		result         engine.RunResult
		expectedImages int
		expectedAudio  int
		expectedVideo  int
		expectedLoaded int
		expectedErrors int
		expectedSize   int64
	}{
		{
			name: "no media",
			result: engine.RunResult{
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
				},
			},
		},
		{
			name: "single image with data",
			result: engine.RunResult{
				Messages: []types.Message{
					{
						Role: "user",
						Parts: []types.ContentPart{
							{
								Type: "image",
								Media: &types.MediaContent{
									Data:     ptrString("imagedata"),
									MIMEType: "image/jpeg",
								},
							},
						},
					},
				},
			},
			expectedImages: 1,
			expectedLoaded: 1,
			expectedSize:   9,
		},
		{
			name: "multiple media types",
			result: engine.RunResult{
				Messages: []types.Message{
					{
						Role: "user",
						Parts: []types.ContentPart{
							{
								Type: "image",
								Media: &types.MediaContent{
									Data:     ptrString("img"),
									MIMEType: "image/png",
								},
							},
							{
								Type: "audio",
								Media: &types.MediaContent{
									FilePath: ptrString("/audio.mp3"),
									MIMEType: "audio/mpeg",
								},
							},
						},
					},
				},
			},
			expectedImages: 1,
			expectedAudio:  1,
			expectedLoaded: 2,
			expectedSize:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := &testSummary{}
			repo.addMediaStats(summary, &tt.result)

			if summary.TotalImages != tt.expectedImages {
				t.Errorf("TotalImages = %d, want %d", summary.TotalImages, tt.expectedImages)
			}
			if summary.TotalAudio != tt.expectedAudio {
				t.Errorf("TotalAudio = %d, want %d", summary.TotalAudio, tt.expectedAudio)
			}
			if summary.TotalVideo != tt.expectedVideo {
				t.Errorf("TotalVideo = %d, want %d", summary.TotalVideo, tt.expectedVideo)
			}
			if summary.MediaLoadSuccess != tt.expectedLoaded {
				t.Errorf("MediaLoadSuccess = %d, want %d", summary.MediaLoadSuccess, tt.expectedLoaded)
			}
			if summary.MediaLoadErrors != tt.expectedErrors {
				t.Errorf("MediaLoadErrors = %d, want %d", summary.MediaLoadErrors, tt.expectedErrors)
			}
			if summary.TotalMediaSize != tt.expectedSize {
				t.Errorf("TotalMediaSize = %d, want %d", summary.TotalMediaSize, tt.expectedSize)
			}
		})
	}
}

func ptrString(s string) *string {
	return &s
}

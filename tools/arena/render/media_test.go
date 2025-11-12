package render

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

func TestRenderMediaSummaryBadge(t *testing.T) {
	tests := []struct {
		name    string
		summary *types.MediaSummary
		want    []string // Strings that should appear in output
		wantNot []string // Strings that should NOT appear
	}{
		{
			name: "mixed media",
			summary: &types.MediaSummary{
				TotalParts: 3,
				TextParts:  1,
				ImageParts: 1,
				AudioParts: 1,
			},
			want:    []string{"media-badge image", "🖼️ 1", "media-badge audio", "🎵 1"},
			wantNot: []string{"video"},
		},
		{
			name: "text only",
			summary: &types.MediaSummary{
				TotalParts: 1,
				TextParts:  1,
			},
			want:    []string{},
			wantNot: []string{"media-badge", "🖼️", "🎵", "🎬"},
		},
		{
			name:    "nil summary",
			summary: nil,
			want:    []string{},
			wantNot: []string{"media-badge"},
		},
		{
			name: "multiple images",
			summary: &types.MediaSummary{
				TotalParts: 4,
				TextParts:  1,
				ImageParts: 3,
			},
			want:    []string{"🖼️ 3", "media-badge image"},
			wantNot: []string{"audio", "video"},
		},
		{
			name: "all media types",
			summary: &types.MediaSummary{
				TotalParts: 4,
				ImageParts: 1,
				AudioParts: 1,
				VideoParts: 1,
				TextParts:  1,
			},
			want: []string{"🖼️ 1", "🎵 1", "🎬 1", "media-badge image", "media-badge audio", "media-badge video"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderMediaSummaryBadge(tt.summary)

			// Check for expected strings
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("renderMediaSummaryBadge() missing expected string %q\nGot: %s", want, got)
				}
			}

			// Check for unexpected strings
			for _, wantNot := range tt.wantNot {
				if strings.Contains(got, wantNot) {
					t.Errorf("renderMediaSummaryBadge() contains unexpected string %q\nGot: %s", wantNot, got)
				}
			}
		})
	}
}

func TestRenderMediaItem(t *testing.T) {
	tests := []struct {
		name    string
		item    types.MediaItemSummary
		want    []string
		wantNot []string
	}{
		{
			name: "loaded image",
			item: types.MediaItemSummary{
				Type:      types.ContentTypeImage,
				Source:    "test-image.jpg",
				MIMEType:  "image/jpeg",
				SizeBytes: 45000,
				Loaded:    true,
				Error:     "",
			},
			want:    []string{"loaded", "✅", "test-image.jpg", "image/jpeg", "43.9 KB", "🖼️"},
			wantNot: []string{"error", "not-loaded", "❌", "⚠️"},
		},
		{
			name: "failed audio load",
			item: types.MediaItemSummary{
				Type:      types.ContentTypeAudio,
				Source:    "missing-audio.wav",
				MIMEType:  "audio/wav",
				SizeBytes: 0,
				Loaded:    false,
				Error:     "file not found",
			},
			want:    []string{"error", "❌", "missing-audio.wav", "file not found", "🎵"},
			wantNot: []string{"loaded", "✅"},
		},
		{
			name: "not loaded video",
			item: types.MediaItemSummary{
				Type:      types.ContentTypeVideo,
				Source:    "video.mp4",
				MIMEType:  "video/mp4",
				SizeBytes: 1024000,
				Loaded:    false,
				Error:     "",
			},
			want:    []string{"not-loaded", "⚠️", "video.mp4", "1000.0 KB", "🎬"},
			wantNot: []string{"error", "❌"},
		},
		{
			name: "long source path",
			item: types.MediaItemSummary{
				Type:      types.ContentTypeImage,
				Source:    "/very/long/path/to/some/deeply/nested/directory/structure/image.png",
				MIMEType:  "image/png",
				SizeBytes: 100,
				Loaded:    true,
			},
			want:    []string{"loaded", ".../image.png"}, // Should be truncated
			wantNot: []string{"/very/long/path/to/some/deeply/nested"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderMediaItem(tt.item)

			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("renderMediaItem() missing expected string %q\nGot: %s", want, got)
				}
			}

			for _, wantNot := range tt.wantNot {
				if strings.Contains(got, wantNot) {
					t.Errorf("renderMediaItem() contains unexpected string %q\nGot: %s", wantNot, got)
				}
			}
		})
	}
}

func TestGetMediaTypeIcon(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		want      string
	}{
		{"image", types.ContentTypeImage, "🖼️"},
		{"audio", types.ContentTypeAudio, "🎵"},
		{"video", types.ContentTypeVideo, "🎬"},
		{"unknown", "unknown-type", "📎"},
		{"empty", "", "📎"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMediaTypeIcon(tt.mediaType)
			if got != tt.want {
				t.Errorf("getMediaTypeIcon(%q) = %q, want %q", tt.mediaType, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes int
		want  string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 512, "512 B"},
		{"1 KB", 1024, "1.0 KB"},
		{"1.5 KB", 1536, "1.5 KB"},
		{"1 MB", 1048576, "1.0 MB"},
		{"1.5 MB", 1572864, "1.5 MB"},
		{"1 GB", 1073741824, "1.0 GB"},
		{"mixed", 45678, "44.6 KB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestTruncateSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		maxLen int
		want   string
	}{
		{
			name:   "short path",
			source: "test.jpg",
			maxLen: 40,
			want:   "test.jpg",
		},
		{
			name:   "long path with filename",
			source: "/very/long/path/to/some/deeply/nested/directory/structure/image.png",
			maxLen: 40,
			want:   ".../image.png",
		},
		{
			name:   "URL truncation",
			source: "https://example.com/very/long/path/to/resource/image.jpg",
			maxLen: 40,
			want:   ".../image.jpg",
		},
		{
			name:   "exact length",
			source: "1234567890123456789012345678901234567890",
			maxLen: 40,
			want:   "1234567890123456789012345678901234567890",
		},
		{
			name:   "one over length",
			source: "12345678901234567890123456789012345678901",
			maxLen: 40,
			want:   "1234567890123456789012345678901234567...",
		},
		{
			name:   "long filename only",
			source: "/path/to/very-long-filename-that-exceeds-max-length-limits.jpg",
			maxLen: 30,
			want:   ".../very-long-filename-that...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateSource(tt.source, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateSource(%q, %d) = %q, want %q", tt.source, tt.maxLen, got, tt.want)
			}
			if len(got) > tt.maxLen {
				t.Errorf("truncateSource(%q, %d) returned string longer than max: %d > %d",
					tt.source, tt.maxLen, len(got), tt.maxLen)
			}
		})
	}
}

func TestRenderMessageWithMedia(t *testing.T) {
	tests := []struct {
		name    string
		msg     types.Message
		want    []string
		wantNot []string
	}{
		{
			name: "text only message",
			msg: types.Message{
				Role:    "user",
				Content: "Hello world",
				Parts:   nil,
			},
			want:    []string{"message user", "Hello world", "message-text"},
			wantNot: []string{"media-badge", "media-items"},
		},
		{
			name: "multimodal message with image",
			msg: types.Message{
				Role:    "user",
				Content: "Analyze this image",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeText,
						Text: stringPtr("Analyze this image"),
					},
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: "image/jpeg",
							FilePath: stringPtr("test.jpg"),
							Data:     stringPtr("base64data"),
						},
					},
				},
			},
			want: []string{"message user", "Analyze this image", "media-badge image", "🖼️", "media-items", "test.jpg"},
		},
		{
			name: "message with multiple media types",
			msg: types.Message{
				Role: "assistant",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: "image/png",
							FilePath: stringPtr("chart.png"),
							Data:     stringPtr("data"),
						},
					},
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: "audio/wav",
							FilePath: stringPtr("voice.wav"),
							Data:     stringPtr("data"),
						},
					},
				},
			},
			want: []string{"message assistant", "🖼️", "🎵", "chart.png", "voice.wav", "media-items"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderMessageWithMedia(tt.msg)

			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("renderMessageWithMedia() missing expected string %q\nGot: %s", want, got)
				}
			}

			for _, wantNot := range tt.wantNot {
				if strings.Contains(got, wantNot) {
					t.Errorf("renderMessageWithMedia() contains unexpected string %q\nGot: %s", wantNot, got)
				}
			}
		})
	}
}

func TestGetMediaSummaryFromParts(t *testing.T) {
	tests := []struct {
		name  string
		parts []types.ContentPart
		want  *types.MediaSummary
	}{
		{
			name:  "nil parts",
			parts: nil,
			want:  nil,
		},
		{
			name:  "empty parts",
			parts: []types.ContentPart{},
			want:  nil,
		},
		{
			name: "text only",
			parts: []types.ContentPart{
				{Type: types.ContentTypeText, Text: stringPtr("hello")},
			},
			want: &types.MediaSummary{
				TotalParts: 1,
				TextParts:  1,
				MediaItems: []types.MediaItemSummary{},
			},
		},
		{
			name: "mixed content",
			parts: []types.ContentPart{
				{Type: types.ContentTypeText, Text: stringPtr("text")},
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						MIMEType: "image/jpeg",
						FilePath: stringPtr("test.jpg"),
					},
				},
				{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						MIMEType: "audio/wav",
						URL:      stringPtr("http://example.com/audio.wav"),
					},
				},
			},
			want: &types.MediaSummary{
				TotalParts: 3,
				TextParts:  1,
				ImageParts: 1,
				AudioParts: 1,
				MediaItems: []types.MediaItemSummary{
					{Type: types.ContentTypeImage, Source: "test.jpg", MIMEType: "image/jpeg"},
					{Type: types.ContentTypeAudio, Source: "http://example.com/audio.wav", MIMEType: "audio/wav"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMediaSummaryFromParts(tt.parts)

			if tt.want == nil {
				if got != nil {
					t.Errorf("getMediaSummaryFromParts() = %+v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatalf("getMediaSummaryFromParts() = nil, want %+v", tt.want)
			}

			if got.TotalParts != tt.want.TotalParts {
				t.Errorf("TotalParts = %d, want %d", got.TotalParts, tt.want.TotalParts)
			}
			if got.TextParts != tt.want.TextParts {
				t.Errorf("TextParts = %d, want %d", got.TextParts, tt.want.TextParts)
			}
			if got.ImageParts != tt.want.ImageParts {
				t.Errorf("ImageParts = %d, want %d", got.ImageParts, tt.want.ImageParts)
			}
			if got.AudioParts != tt.want.AudioParts {
				t.Errorf("AudioParts = %d, want %d", got.AudioParts, tt.want.AudioParts)
			}
			if got.VideoParts != tt.want.VideoParts {
				t.Errorf("VideoParts = %d, want %d", got.VideoParts, tt.want.VideoParts)
			}
			if len(got.MediaItems) != len(tt.want.MediaItems) {
				t.Errorf("len(MediaItems) = %d, want %d", len(got.MediaItems), len(tt.want.MediaItems))
			}
		})
	}
}

func TestGetMediaItemSummaryFromPart(t *testing.T) {
	tests := []struct {
		name string
		part types.ContentPart
		want types.MediaItemSummary
	}{
		{
			name: "no media content",
			part: types.ContentPart{
				Type:  types.ContentTypeImage,
				Media: nil,
			},
			want: types.MediaItemSummary{
				Type:   types.ContentTypeImage,
				Loaded: false,
				Error:  "no media content",
			},
		},
		{
			name: "file path source",
			part: types.ContentPart{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					MIMEType: "image/png",
					FilePath: stringPtr("/path/to/image.png"),
				},
			},
			want: types.MediaItemSummary{
				Type:     types.ContentTypeImage,
				Source:   "/path/to/image.png",
				MIMEType: "image/png",
				Loaded:   false,
			},
		},
		{
			name: "URL source",
			part: types.ContentPart{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					MIMEType: "audio/mp3",
					URL:      stringPtr("https://example.com/audio.mp3"),
				},
			},
			want: types.MediaItemSummary{
				Type:     types.ContentTypeAudio,
				Source:   "https://example.com/audio.mp3",
				MIMEType: "audio/mp3",
				Loaded:   false,
			},
		},
		{
			name: "inline data",
			part: types.ContentPart{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					MIMEType: "image/jpeg",
					Data:     stringPtr("aGVsbG8gd29ybGQ="), // 16 chars base64
				},
			},
			want: types.MediaItemSummary{
				Type:      types.ContentTypeImage,
				Source:    "inline data",
				MIMEType:  "image/jpeg",
				Loaded:    true,
				SizeBytes: 12, // (16 * 3) / 4
			},
		},
		{
			name: "with size metadata",
			part: types.ContentPart{
				Type: types.ContentTypeVideo,
				Media: &types.MediaContent{
					MIMEType: "video/mp4",
					FilePath: stringPtr("video.mp4"),
					SizeKB:   int64Ptr(1024),
				},
			},
			want: types.MediaItemSummary{
				Type:      types.ContentTypeVideo,
				Source:    "video.mp4",
				MIMEType:  "video/mp4",
				Loaded:    false,
				SizeBytes: 1048576, // 1024 * 1024
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMediaItemSummaryFromPart(tt.part)

			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.Source != tt.want.Source {
				t.Errorf("Source = %q, want %q", got.Source, tt.want.Source)
			}
			if got.MIMEType != tt.want.MIMEType {
				t.Errorf("MIMEType = %q, want %q", got.MIMEType, tt.want.MIMEType)
			}
			if got.Loaded != tt.want.Loaded {
				t.Errorf("Loaded = %v, want %v", got.Loaded, tt.want.Loaded)
			}
			if got.Error != tt.want.Error {
				t.Errorf("Error = %q, want %q", got.Error, tt.want.Error)
			}
			if got.SizeBytes != tt.want.SizeBytes {
				t.Errorf("SizeBytes = %d, want %d", got.SizeBytes, tt.want.SizeBytes)
			}
		})
	}
}

// Helper functions for tests
func stringPtr(s string) *string {
	return &s
}

func int64Ptr(i int64) *int64 {
	return &i
}

func TestCalculateMediaStats(t *testing.T) {
	tests := []struct {
		name    string
		results []engine.RunResult
		want    MediaStats
	}{
		{
			name:    "empty results",
			results: []engine.RunResult{},
			want:    MediaStats{},
		},
		{
			name: "text only messages",
			results: []engine.RunResult{
				{
					Messages: []types.Message{
						{Role: "user", Content: "Hello"},
						{Role: "assistant", Content: "Hi"},
					},
				},
			},
			want: MediaStats{},
		},
		{
			name: "single image loaded",
			results: []engine.RunResult{
				{
					Messages: []types.Message{
						{
							Role: "user",
							Parts: []types.ContentPart{
								{
									Type: types.ContentTypeImage,
									Media: &types.MediaContent{
										MIMEType: "image/jpeg",
										FilePath: stringPtr("test.jpg"),
										Data:     stringPtr("data"),
										SizeKB:   int64Ptr(50),
									},
								},
							},
						},
					},
				},
			},
			want: MediaStats{
				TotalImages:      1,
				MediaLoadSuccess: 1,
				TotalMediaSize:   51200, // 50 * 1024
			},
		},
		{
			name: "mixed media with errors",
			results: []engine.RunResult{
				{
					Messages: []types.Message{
						{
							Role: "user",
							Parts: []types.ContentPart{
								{
									Type: types.ContentTypeImage,
									Media: &types.MediaContent{
										MIMEType: "image/png",
										FilePath: stringPtr("loaded.png"),
										Data:     stringPtr("data"),
										SizeKB:   int64Ptr(100),
									},
								},
								{
									Type: types.ContentTypeImage,
									Media: &types.MediaContent{
										MIMEType: "image/jpeg",
										FilePath: stringPtr("missing.jpg"),
										// No Data = not loaded, triggers error path
									},
								},
								{
									Type: types.ContentTypeAudio,
									Media: &types.MediaContent{
										MIMEType: "audio/wav",
										FilePath: stringPtr("audio.wav"),
										Data:     stringPtr("audiodata"),
										SizeKB:   int64Ptr(256),
									},
								},
							},
						},
					},
				},
			},
			want: MediaStats{
				TotalImages:      2,
				TotalAudio:       1,
				MediaLoadSuccess: 2,
				TotalMediaSize:   364544, // (100 + 256) * 1024
			},
		},
		{
			name: "multiple results",
			results: []engine.RunResult{
				{
					Messages: []types.Message{
						{
							Role: "user",
							Parts: []types.ContentPart{
								{
									Type: types.ContentTypeImage,
									Media: &types.MediaContent{
										MIMEType: "image/jpeg",
										FilePath: stringPtr("img1.jpg"),
										Data:     stringPtr("data1"),
										SizeKB:   int64Ptr(50),
									},
								},
							},
						},
					},
				},
				{
					Messages: []types.Message{
						{
							Role: "user",
							Parts: []types.ContentPart{
								{
									Type: types.ContentTypeVideo,
									Media: &types.MediaContent{
										MIMEType: "video/mp4",
										FilePath: stringPtr("video.mp4"),
										Data:     stringPtr("videodata"),
										SizeKB:   int64Ptr(1024),
									},
								},
							},
						},
					},
				},
			},
			want: MediaStats{
				TotalImages:      1,
				TotalVideo:       1,
				MediaLoadSuccess: 2,
				TotalMediaSize:   1099776, // (50 + 1024) * 1024
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateMediaStats(tt.results)

			if got.TotalImages != tt.want.TotalImages {
				t.Errorf("TotalImages = %d, want %d", got.TotalImages, tt.want.TotalImages)
			}
			if got.TotalAudio != tt.want.TotalAudio {
				t.Errorf("TotalAudio = %d, want %d", got.TotalAudio, tt.want.TotalAudio)
			}
			if got.TotalVideo != tt.want.TotalVideo {
				t.Errorf("TotalVideo = %d, want %d", got.TotalVideo, tt.want.TotalVideo)
			}
			if got.MediaLoadSuccess != tt.want.MediaLoadSuccess {
				t.Errorf("MediaLoadSuccess = %d, want %d", got.MediaLoadSuccess, tt.want.MediaLoadSuccess)
			}
			if got.MediaLoadErrors != tt.want.MediaLoadErrors {
				t.Errorf("MediaLoadErrors = %d, want %d", got.MediaLoadErrors, tt.want.MediaLoadErrors)
			}
			if got.TotalMediaSize != tt.want.TotalMediaSize {
				t.Errorf("TotalMediaSize = %d, want %d", got.TotalMediaSize, tt.want.TotalMediaSize)
			}
		})
	}
}

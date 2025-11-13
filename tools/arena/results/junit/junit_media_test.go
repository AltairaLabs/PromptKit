package junit

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

func TestCalculateMediaStats(t *testing.T) {
	tests := []struct {
		name     string
		results  []engine.RunResult
		expected MediaStats
	}{
		{
			name:    "empty results",
			results: []engine.RunResult{},
			expected: MediaStats{
				TotalImages:      0,
				TotalAudio:       0,
				TotalVideo:       0,
				MediaLoadSuccess: 0,
				MediaLoadErrors:  0,
				TotalMediaSize:   0,
			},
		},
		{
			name: "single image with data",
			results: []engine.RunResult{
				{
					Messages: []types.Message{
						{
							Role: "user",
							Parts: []types.ContentPart{
								{
									Type: types.ContentTypeImage,
									Media: &types.MediaContent{
										Data:     ptrString("base64data"),
										MIMEType: "image/jpeg",
									},
								},
							},
						},
					},
				},
			},
			expected: MediaStats{
				TotalImages:      1,
				TotalAudio:       0,
				TotalVideo:       0,
				MediaLoadSuccess: 1,
				MediaLoadErrors:  0,
				TotalMediaSize:   10, // length of "base64data"
			},
		},
		{
			name: "multiple media types",
			results: []engine.RunResult{
				{
					Messages: []types.Message{
						{
							Role: "user",
							Parts: []types.ContentPart{
								{
									Type: types.ContentTypeImage,
									Media: &types.MediaContent{
										Data:     ptrString("img123"),
										MIMEType: "image/png",
									},
								},
								{
									Type: types.ContentTypeAudio,
									Media: &types.MediaContent{
										FilePath: ptrString("/path/to/audio.mp3"),
										MIMEType: "audio/mpeg",
									},
								},
								{
									Type: types.ContentTypeVideo,
									Media: &types.MediaContent{
										URL:      ptrString("https://example.com/video.mp4"),
										MIMEType: "video/mp4",
									},
								},
							},
						},
					},
				},
			},
			expected: MediaStats{
				TotalImages:      1,
				TotalAudio:       1,
				TotalVideo:       1,
				MediaLoadSuccess: 3,
				MediaLoadErrors:  0,
				TotalMediaSize:   6, // length of "img123"
			},
		},
		{
			name: "media without data sources (errors)",
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
										// No data, file path, or URL
									},
								},
								{
									Type: types.ContentTypeAudio,
									Media: &types.MediaContent{
										Data:     ptrString(""), // Empty data
										MIMEType: "audio/mp3",
									},
								},
							},
						},
					},
				},
			},
			expected: MediaStats{
				TotalImages:      1,
				TotalAudio:       1,
				TotalVideo:       0,
				MediaLoadSuccess: 0,
				MediaLoadErrors:  2,
				TotalMediaSize:   0,
			},
		},
		{
			name: "messages without parts",
			results: []engine.RunResult{
				{
					Messages: []types.Message{
						{
							Role:    "user",
							Content: "Just text, no parts",
						},
					},
				},
			},
			expected: MediaStats{
				TotalImages:      0,
				TotalAudio:       0,
				TotalVideo:       0,
				MediaLoadSuccess: 0,
				MediaLoadErrors:  0,
				TotalMediaSize:   0,
			},
		},
		{
			name: "parts without media",
			results: []engine.RunResult{
				{
					Messages: []types.Message{
						{
							Role: "user",
							Parts: []types.ContentPart{
								{
									Type: types.ContentTypeText,
									Text: ptrString("Hello world"),
								},
								{
									Type: types.ContentTypeImage,
									// No media field
								},
							},
						},
					},
				},
			},
			expected: MediaStats{
				TotalImages:      0,
				TotalAudio:       0,
				TotalVideo:       0,
				MediaLoadSuccess: 0,
				MediaLoadErrors:  0,
				TotalMediaSize:   0,
			},
		},
		{
			name: "multiple results with mixed media",
			results: []engine.RunResult{
				{
					Messages: []types.Message{
						{
							Role: "user",
							Parts: []types.ContentPart{
								{
									Type: types.ContentTypeImage,
									Media: &types.MediaContent{
										Data:     ptrString("abc"),
										MIMEType: "image/png",
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
									Type: types.ContentTypeImage,
									Media: &types.MediaContent{
										Data:     ptrString("def"),
										MIMEType: "image/jpeg",
									},
								},
								{
									Type: types.ContentTypeAudio,
									Media: &types.MediaContent{
										Data:     ptrString("audio123"),
										MIMEType: "audio/wav",
									},
								},
							},
						},
					},
				},
			},
			expected: MediaStats{
				TotalImages:      2,
				TotalAudio:       1,
				TotalVideo:       0,
				MediaLoadSuccess: 3,
				MediaLoadErrors:  0,
				TotalMediaSize:   14, // 3 + 3 + 8
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateMediaStats(tt.results)

			if result.TotalImages != tt.expected.TotalImages {
				t.Errorf("TotalImages = %d, want %d", result.TotalImages, tt.expected.TotalImages)
			}
			if result.TotalAudio != tt.expected.TotalAudio {
				t.Errorf("TotalAudio = %d, want %d", result.TotalAudio, tt.expected.TotalAudio)
			}
			if result.TotalVideo != tt.expected.TotalVideo {
				t.Errorf("TotalVideo = %d, want %d", result.TotalVideo, tt.expected.TotalVideo)
			}
			if result.MediaLoadSuccess != tt.expected.MediaLoadSuccess {
				t.Errorf("MediaLoadSuccess = %d, want %d", result.MediaLoadSuccess, tt.expected.MediaLoadSuccess)
			}
			if result.MediaLoadErrors != tt.expected.MediaLoadErrors {
				t.Errorf("MediaLoadErrors = %d, want %d", result.MediaLoadErrors, tt.expected.MediaLoadErrors)
			}
			if result.TotalMediaSize != tt.expected.TotalMediaSize {
				t.Errorf("TotalMediaSize = %d, want %d", result.TotalMediaSize, tt.expected.TotalMediaSize)
			}
		})
	}
}

func TestRenderMediaProperties(t *testing.T) {
	tests := []struct {
		name     string
		stats    MediaStats
		expected map[string]string
	}{
		{
			name: "all zeros - no properties emitted",
			stats: MediaStats{
				TotalImages:      0,
				TotalAudio:       0,
				TotalVideo:       0,
				MediaLoadSuccess: 0,
				MediaLoadErrors:  0,
				TotalMediaSize:   0,
			},
			expected: map[string]string{}, // Empty - no properties emitted for zeros
		},
		{
			name: "with values",
			stats: MediaStats{
				TotalImages:      5,
				TotalAudio:       3,
				TotalVideo:       2,
				MediaLoadSuccess: 8,
				MediaLoadErrors:  2,
				TotalMediaSize:   12345,
			},
			expected: map[string]string{
				"media.images.total":     "5",
				"media.audio.total":      "3",
				"media.video.total":      "2",
				"media.loaded.success":   "8",
				"media.loaded.errors":    "2",
				"media.size.total_bytes": "12345",
			},
		},
		{
			name: "large values without errors",
			stats: MediaStats{
				TotalImages:      100,
				TotalAudio:       50,
				TotalVideo:       25,
				MediaLoadSuccess: 175,
				MediaLoadErrors:  0,
				TotalMediaSize:   999999999,
			},
			expected: map[string]string{
				"media.images.total":     "100",
				"media.audio.total":      "50",
				"media.video.total":      "25",
				"media.loaded.success":   "175",
				"media.size.total_bytes": "999999999",
				// No media.loaded.errors since it's 0
			},
		},
		{
			name: "only images",
			stats: MediaStats{
				TotalImages:      10,
				TotalAudio:       0,
				TotalVideo:       0,
				MediaLoadSuccess: 10,
				MediaLoadErrors:  0,
				TotalMediaSize:   5000,
			},
			expected: map[string]string{
				"media.images.total":     "10",
				"media.loaded.success":   "10",
				"media.size.total_bytes": "5000",
			},
		},
		{
			name: "with errors",
			stats: MediaStats{
				TotalImages:      5,
				TotalAudio:       0,
				TotalVideo:       0,
				MediaLoadSuccess: 3,
				MediaLoadErrors:  2,
				TotalMediaSize:   1500,
			},
			expected: map[string]string{
				"media.images.total":     "5",
				"media.loaded.success":   "3",
				"media.loaded.errors":    "2",
				"media.size.total_bytes": "1500",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderMediaProperties(tt.stats)

			if len(result) != len(tt.expected) {
				t.Errorf("got %d properties, want %d", len(result), len(tt.expected))
			}

			// Convert result to map for easier comparison
			resultMap := make(map[string]string)
			for _, prop := range result {
				resultMap[prop.Name] = prop.Value
			}

			for key, expectedValue := range tt.expected {
				if resultMap[key] != expectedValue {
					t.Errorf("property %s = %s, want %s", key, resultMap[key], expectedValue)
				}
			}
		})
	}
}

func TestMediaStatsIntegration(t *testing.T) {
	// Create a realistic test scenario
	results := []engine.RunResult{
		{
			RunID:      "test-001",
			ProviderID: "openai",
			ScenarioID: "image-analysis",
			StartTime:  time.Now().Add(-5 * time.Second),
			EndTime:    time.Now(),
			Duration:   5 * time.Second,
			Messages: []types.Message{
				{
					Role:    "user",
					Content: "Analyze this image",
					Parts: []types.ContentPart{
						{
							Type: types.ContentTypeText,
							Text: ptrString("Analyze this image"),
						},
						{
							Type: types.ContentTypeImage,
							Media: &types.MediaContent{
								Data:     ptrString("image_base64_data_here"),
								MIMEType: "image/jpeg",
							},
						},
					},
				},
				{
					Role:    "assistant",
					Content: "I can see...",
				},
			},
		},
		{
			RunID:      "test-002",
			ProviderID: "anthropic",
			ScenarioID: "audio-transcription",
			StartTime:  time.Now().Add(-3 * time.Second),
			EndTime:    time.Now(),
			Duration:   3 * time.Second,
			Messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						{
							Type: types.ContentTypeAudio,
							Media: &types.MediaContent{
								FilePath: ptrString("/path/to/audio.mp3"),
								MIMEType: "audio/mpeg",
							},
						},
					},
				},
				{
					Role:    "assistant",
					Content: "Transcription: ...",
				},
			},
		},
	}

	// Calculate stats
	stats := calculateMediaStats(results)

	// Verify stats
	if stats.TotalImages != 1 {
		t.Errorf("TotalImages = %d, want 1", stats.TotalImages)
	}
	if stats.TotalAudio != 1 {
		t.Errorf("TotalAudio = %d, want 1", stats.TotalAudio)
	}
	if stats.MediaLoadSuccess != 2 {
		t.Errorf("MediaLoadSuccess = %d, want 2", stats.MediaLoadSuccess)
	}

	// Render properties
	props := renderMediaProperties(stats)

	if len(props) != 4 { // Only non-zero values are included
		t.Errorf("got %d properties, want 4", len(props))
	}

	// Verify property structure
	for _, prop := range props {
		if prop.Name == "" {
			t.Error("property has empty name")
		}
		if prop.Value == "" {
			t.Error("property has empty value")
		}
	}
}

// Helper function to create string pointers
func ptrString(s string) *string {
	return &s
}

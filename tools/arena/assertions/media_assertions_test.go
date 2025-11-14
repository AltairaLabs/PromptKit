package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

func TestImageFormatValidator(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		message    types.Message
		wantPassed bool
		wantError  string
	}{
		{
			name: "valid jpeg format",
			params: map[string]interface{}{
				"formats": []interface{}{"jpeg", "png"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImageJPEG,
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "valid png format",
			params: map[string]interface{}{
				"formats": []interface{}{"jpeg", "png"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImagePNG,
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "invalid format",
			params: map[string]interface{}{
				"formats": []interface{}{"jpeg", "png"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImageGIF,
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "no images in message",
			params: map[string]interface{}{
				"formats": []interface{}{"jpeg", "png"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeText,
						Text: stringPtr("Hello"),
					},
				},
			},
			wantPassed: false,
			wantError:  "no images found",
		},
		{
			name: "empty formats list",
			params: map[string]interface{}{
				"formats": []interface{}{},
			},
			message: types.Message{
				Role:  "user",
				Parts: []types.ContentPart{},
			},
			wantPassed: false,
			wantError:  "at least one format",
		},
		{
			name: "multiple images mixed formats",
			params: map[string]interface{}{
				"formats": []interface{}{"jpeg"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImageJPEG,
						},
					},
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImagePNG,
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "case insensitive format matching",
			params: map[string]interface{}{
				"formats": []interface{}{"JPEG", "PNG"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImageJPEG,
						},
					},
				},
			},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewImageFormatValidator(tt.params)
			validationParams := map[string]interface{}{
				"_assistant_message": tt.message,
			}
			result := validator.Validate("", validationParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() passed = %v, want %v. Details: %+v", result.Passed, tt.wantPassed, result.Details)
			}

			if tt.wantError != "" {
				if details, ok := result.Details.(map[string]interface{}); ok {
					if errorMsg, ok := details["error"].(string); ok {
						if errorMsg == "" {
							t.Errorf("Expected error containing %q, got empty error", tt.wantError)
						}
					}
				}
			}
		})
	}
}

func TestImageDimensionsValidator(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		message    types.Message
		wantPassed bool
		wantError  string
	}{
		{
			name: "exact dimensions match",
			params: map[string]interface{}{
				"width":  1920,
				"height": 1080,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImageJPEG,
							Width:    intPtr(1920),
							Height:   intPtr(1080),
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "within min max range",
			params: map[string]interface{}{
				"min_width":  800,
				"max_width":  2000,
				"min_height": 600,
				"max_height": 1500,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImageJPEG,
							Width:    intPtr(1920),
							Height:   intPtr(1080),
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "width too small",
			params: map[string]interface{}{
				"min_width": 1000,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImageJPEG,
							Width:    intPtr(800),
							Height:   intPtr(600),
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "height too large",
			params: map[string]interface{}{
				"max_height": 1000,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImageJPEG,
							Width:    intPtr(1920),
							Height:   intPtr(1200),
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "missing dimensions metadata",
			params: map[string]interface{}{
				"min_width": 800,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeImageJPEG,
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "no images in message",
			params: map[string]interface{}{
				"min_width": 800,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeText,
						Text: stringPtr("Hello"),
					},
				},
			},
			wantPassed: false,
			wantError:  "no images found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewImageDimensionsValidator(tt.params)
			validationParams := map[string]interface{}{
				"_assistant_message": tt.message,
			}
			result := validator.Validate("", validationParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() passed = %v, want %v. Details: %+v", result.Passed, tt.wantPassed, result.Details)
			}

			if tt.wantError != "" {
				if details, ok := result.Details.(map[string]interface{}); ok {
					if errorMsg, ok := details["error"].(string); ok {
						if errorMsg == "" {
							t.Errorf("Expected error containing %q, got empty error", tt.wantError)
						}
					}
				}
			}
		})
	}
}

func TestAudioDurationValidator(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		message    types.Message
		wantPassed bool
		wantError  string
	}{
		{
			name: "duration within range",
			params: map[string]interface{}{
				"min_seconds": 1.0,
				"max_seconds": 300.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeAudioMP3,
							Duration: intPtr(30),
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "duration too short",
			params: map[string]interface{}{
				"min_seconds": 10.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeAudioMP3,
							Duration: intPtr(5),
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "duration too long",
			params: map[string]interface{}{
				"max_seconds": 60.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeAudioMP3,
							Duration: intPtr(120),
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "missing duration metadata",
			params: map[string]interface{}{
				"min_seconds": 1.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeAudioMP3,
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "no audio in message",
			params: map[string]interface{}{
				"min_seconds": 1.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeText,
						Text: stringPtr("Hello"),
					},
				},
			},
			wantPassed: false,
			wantError:  "no audio found",
		},
		{
			name: "integer params",
			params: map[string]interface{}{
				"min_seconds": 1,
				"max_seconds": 300,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeAudioMP3,
							Duration: intPtr(30),
						},
					},
				},
			},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewAudioDurationValidator(tt.params)
			validationParams := map[string]interface{}{
				"_assistant_message": tt.message,
			}
			result := validator.Validate("", validationParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() passed = %v, want %v. Details: %+v", result.Passed, tt.wantPassed, result.Details)
			}

			if tt.wantError != "" {
				if details, ok := result.Details.(map[string]interface{}); ok {
					if errorMsg, ok := details["error"].(string); ok {
						if errorMsg == "" {
							t.Errorf("Expected error containing %q, got empty error", tt.wantError)
						}
					}
				}
			}
		})
	}
}

func TestAudioFormatValidator(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		message    types.Message
		wantPassed bool
		wantError  string
	}{
		{
			name: "valid mp3 format",
			params: map[string]interface{}{
				"formats": []interface{}{"mp3", "wav"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeAudioMP3,
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "valid wav format",
			params: map[string]interface{}{
				"formats": []interface{}{"wav"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeAudioWAV,
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "invalid format",
			params: map[string]interface{}{
				"formats": []interface{}{"mp3"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeAudioWAV,
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "no audio in message",
			params: map[string]interface{}{
				"formats": []interface{}{"mp3"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeText,
						Text: stringPtr("Hello"),
					},
				},
			},
			wantPassed: false,
			wantError:  "no audio found",
		},
		{
			name: "empty formats list",
			params: map[string]interface{}{
				"formats": []interface{}{},
			},
			message: types.Message{
				Role:  "user",
				Parts: []types.ContentPart{},
			},
			wantPassed: false,
			wantError:  "at least one format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewAudioFormatValidator(tt.params)
			validationParams := map[string]interface{}{
				"_assistant_message": tt.message,
			}
			result := validator.Validate("", validationParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() passed = %v, want %v. Details: %+v", result.Passed, tt.wantPassed, result.Details)
			}

			if tt.wantError != "" {
				if details, ok := result.Details.(map[string]interface{}); ok {
					if errorMsg, ok := details["error"].(string); ok {
						if errorMsg == "" {
							t.Errorf("Expected error containing %q, got empty error", tt.wantError)
						}
					}
				}
			}
		})
	}
}

func TestVideoDurationValidator(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		message    types.Message
		wantPassed bool
		wantError  string
	}{
		{
			name: "duration within range",
			params: map[string]interface{}{
				"min_seconds": 1.0,
				"max_seconds": 600.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
							Duration: intPtr(120),
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "duration too short",
			params: map[string]interface{}{
				"min_seconds": 60.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
							Duration: intPtr(30),
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "duration too long",
			params: map[string]interface{}{
				"max_seconds": 120.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
							Duration: intPtr(180),
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "missing duration metadata",
			params: map[string]interface{}{
				"min_seconds": 1.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "no video in message",
			params: map[string]interface{}{
				"min_seconds": 1.0,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeText,
						Text: stringPtr("Hello"),
					},
				},
			},
			wantPassed: false,
			wantError:  "no video found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewVideoDurationValidator(tt.params)
			validationParams := map[string]interface{}{
				"_assistant_message": tt.message,
			}
			result := validator.Validate("", validationParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() passed = %v, want %v. Details: %+v", result.Passed, tt.wantPassed, result.Details)
			}

			if tt.wantError != "" {
				if details, ok := result.Details.(map[string]interface{}); ok {
					if errorMsg, ok := details["error"].(string); ok {
						if errorMsg == "" {
							t.Errorf("Expected error containing %q, got empty error", tt.wantError)
						}
					}
				}
			}
		})
	}
}

func TestVideoResolutionValidator(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		message    types.Message
		wantPassed bool
		wantError  string
	}{
		{
			name: "1080p preset match",
			params: map[string]interface{}{
				"presets": []interface{}{"1080p"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
							Width:    intPtr(1920),
							Height:   intPtr(1080),
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "720p preset match",
			params: map[string]interface{}{
				"presets": []interface{}{"720p", "1080p"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
							Width:    intPtr(1280),
							Height:   intPtr(720),
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "4k preset match",
			params: map[string]interface{}{
				"presets": []interface{}{"4k"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
							Width:    intPtr(3840),
							Height:   intPtr(2160),
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "preset mismatch",
			params: map[string]interface{}{
				"presets": []interface{}{"1080p"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
							Width:    intPtr(1280),
							Height:   intPtr(720),
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "within min max range",
			params: map[string]interface{}{
				"min_width":  1280,
				"max_width":  2000,
				"min_height": 720,
				"max_height": 1200,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
							Width:    intPtr(1920),
							Height:   intPtr(1080),
						},
					},
				},
			},
			wantPassed: true,
		},
		{
			name: "width too small",
			params: map[string]interface{}{
				"min_width": 1920,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
							Width:    intPtr(1280),
							Height:   intPtr(720),
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "missing dimensions metadata",
			params: map[string]interface{}{
				"min_width": 1280,
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeVideo,
						Media: &types.MediaContent{
							MIMEType: types.MIMETypeVideoMP4,
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "no video in message",
			params: map[string]interface{}{
				"presets": []interface{}{"1080p"},
			},
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeText,
						Text: stringPtr("Hello"),
					},
				},
			},
			wantPassed: false,
			wantError:  "no video found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewVideoResolutionValidator(tt.params)
			validationParams := map[string]interface{}{
				"_assistant_message": tt.message,
			}
			result := validator.Validate("", validationParams)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() passed = %v, want %v. Details: %+v", result.Passed, tt.wantPassed, result.Details)
			}

			if tt.wantError != "" {
				if details, ok := result.Details.(map[string]interface{}); ok {
					if errorMsg, ok := details["error"].(string); ok {
						if errorMsg == "" {
							t.Errorf("Expected error containing %q, got empty error", tt.wantError)
						}
					}
				}
			}
		})
	}
}

func TestExtractFormatFromMIMEType(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		want     string
	}{
		{"jpeg image", types.MIMETypeImageJPEG, "jpeg"},
		{"png image", types.MIMETypeImagePNG, "png"},
		{"gif image", types.MIMETypeImageGIF, "gif"},
		{"webp image", types.MIMETypeImageWebP, "webp"},
		{"mp3 audio", types.MIMETypeAudioMP3, "mp3"},
		{"wav audio", types.MIMETypeAudioWAV, "wav"},
		{"mp4 video", types.MIMETypeVideoMP4, "mp4"},
		{"invalid mime type", "invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFormatFromMIMEType(tt.mimeType)
			if got != tt.want {
				t.Errorf("extractFormatFromMIMEType(%q) = %q, want %q", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestMediaValidatorsWithoutMessage(t *testing.T) {
	validators := []struct {
		name      string
		validator func(map[string]interface{}) runtimeValidators.Validator
		params    map[string]interface{}
	}{
		{
			name:      "image_format",
			validator: NewImageFormatValidator,
			params:    map[string]interface{}{"formats": []interface{}{"jpeg"}},
		},
		{
			name:      "image_dimensions",
			validator: NewImageDimensionsValidator,
			params:    map[string]interface{}{"min_width": 800},
		},
		{
			name:      "audio_duration",
			validator: NewAudioDurationValidator,
			params:    map[string]interface{}{"min_seconds": 1.0},
		},
		{
			name:      "audio_format",
			validator: NewAudioFormatValidator,
			params:    map[string]interface{}{"formats": []interface{}{"mp3"}},
		},
		{
			name:      "video_duration",
			validator: NewVideoDurationValidator,
			params:    map[string]interface{}{"min_seconds": 1.0},
		},
		{
			name:      "video_resolution",
			validator: NewVideoResolutionValidator,
			params:    map[string]interface{}{"min_width": 1280},
		},
	}

	for _, tt := range validators {
		t.Run(tt.name+"_without_message_param", func(t *testing.T) {
			v := tt.validator(tt.params)
			result := v.Validate("", map[string]interface{}{})

			// Should fail when message parameter is missing
			if result.Passed {
				t.Error("Expected validation to fail without message parameter")
			}

			details, ok := result.Details.(map[string]interface{})
			if !ok {
				t.Fatal("Expected details to be a map")
			}

			if _, hasError := details["error"]; !hasError {
				t.Error("Expected error in details when message parameter is missing")
			}
		})
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

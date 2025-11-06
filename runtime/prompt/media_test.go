package prompt

import (
	"testing"
)

// TestValidateMediaConfig tests the main media configuration validation
func TestValidateMediaConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *MediaConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: false,
		},
		{
			name: "disabled config",
			config: &MediaConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "enabled without supported types",
			config: &MediaConfig{
				Enabled:        true,
				SupportedTypes: []string{},
			},
			wantErr: true,
			errMsg:  "no supported_types specified",
		},
		{
			name: "invalid supported type",
			config: &MediaConfig{
				Enabled:        true,
				SupportedTypes: []string{"invalid"},
			},
			wantErr: true,
			errMsg:  "invalid supported type",
		},
		{
			name: "valid image config",
			config: &MediaConfig{
				Enabled:        true,
				SupportedTypes: []string{"image"},
				Image: &ImageConfig{
					MaxSizeMB:       20,
					AllowedFormats:  []string{"jpeg", "png"},
					DefaultDetail:   "high",
					MaxImagesPerMsg: 5,
				},
			},
			wantErr: false,
		},
		{
			name: "valid audio config",
			config: &MediaConfig{
				Enabled:        true,
				SupportedTypes: []string{"audio"},
				Audio: &AudioConfig{
					MaxSizeMB:       25,
					AllowedFormats:  []string{"mp3", "wav"},
					MaxDurationSec:  600,
					RequireMetadata: false,
				},
			},
			wantErr: false,
		},
		{
			name: "valid video config",
			config: &MediaConfig{
				Enabled:        true,
				SupportedTypes: []string{"video"},
				Video: &VideoConfig{
					MaxSizeMB:       100,
					AllowedFormats:  []string{"mp4", "webm"},
					MaxDurationSec:  300,
					RequireMetadata: false,
				},
			},
			wantErr: false,
		},
		{
			name: "multiple types with configs",
			config: &MediaConfig{
				Enabled:        true,
				SupportedTypes: []string{"image", "audio", "video"},
				Image: &ImageConfig{
					MaxSizeMB:      20,
					AllowedFormats: []string{"jpeg"},
				},
				Audio: &AudioConfig{
					MaxSizeMB:      25,
					AllowedFormats: []string{"mp3"},
				},
				Video: &VideoConfig{
					MaxSizeMB:      100,
					AllowedFormats: []string{"mp4"},
				},
			},
			wantErr: false,
		},
		{
			name: "config with invalid example",
			config: &MediaConfig{
				Enabled:        true,
				SupportedTypes: []string{"image"},
				Image: &ImageConfig{
					MaxSizeMB:      20,
					AllowedFormats: []string{"jpeg"},
				},
				Examples: []MultimodalExample{
					{
						Name: "invalid-example",
						Parts: []ExampleContentPart{
							{
								Text: "", // Empty text
								Media: &ExampleMedia{
									FilePath: "", // Empty path
									MIMEType: "", // Empty MIME type
								},
							},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid example",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMediaConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMediaConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateMediaConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// TestValidateImageConfig tests image configuration validation
func TestValidateImageConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *ImageConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &ImageConfig{
				MaxSizeMB:       20,
				AllowedFormats:  []string{"jpeg", "png", "webp"},
				DefaultDetail:   "high",
				MaxImagesPerMsg: 5,
			},
			wantErr: false,
		},
		{
			name: "negative max size",
			config: &ImageConfig{
				MaxSizeMB: -1,
			},
			wantErr: true,
			errMsg:  "max_size_mb cannot be negative",
		},
		{
			name: "invalid format",
			config: &ImageConfig{
				MaxSizeMB:      20,
				AllowedFormats: []string{"invalid"},
			},
			wantErr: true,
			errMsg:  "invalid image format",
		},
		{
			name: "invalid detail level",
			config: &ImageConfig{
				MaxSizeMB:     20,
				DefaultDetail: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid default_detail",
		},
		{
			name: "negative max images",
			config: &ImageConfig{
				MaxSizeMB:       20,
				MaxImagesPerMsg: -1,
			},
			wantErr: true,
			errMsg:  "max_images_per_msg cannot be negative",
		},
		{
			name: "zero values allowed",
			config: &ImageConfig{
				MaxSizeMB:       0,
				MaxImagesPerMsg: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateImageConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateImageConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateImageConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// TestValidateAudioConfig tests audio configuration validation
func TestValidateAudioConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *AudioConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &AudioConfig{
				MaxSizeMB:       25,
				AllowedFormats:  []string{"mp3", "wav", "ogg"},
				MaxDurationSec:  600,
				RequireMetadata: false,
			},
			wantErr: false,
		},
		{
			name: "negative max size",
			config: &AudioConfig{
				MaxSizeMB: -1,
			},
			wantErr: true,
			errMsg:  "max_size_mb cannot be negative",
		},
		{
			name: "invalid format",
			config: &AudioConfig{
				MaxSizeMB:      25,
				AllowedFormats: []string{"invalid"},
			},
			wantErr: true,
			errMsg:  "invalid audio format",
		},
		{
			name: "negative duration",
			config: &AudioConfig{
				MaxSizeMB:      25,
				MaxDurationSec: -1,
			},
			wantErr: true,
			errMsg:  "max_duration_sec cannot be negative",
		},
		{
			name: "all valid formats",
			config: &AudioConfig{
				AllowedFormats: []string{"mp3", "wav", "ogg", "webm", "m4a", "flac"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAudioConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAudioConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateAudioConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// TestValidateVideoConfig tests video configuration validation
func TestValidateVideoConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *VideoConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &VideoConfig{
				MaxSizeMB:       100,
				AllowedFormats:  []string{"mp4", "webm"},
				MaxDurationSec:  300,
				RequireMetadata: false,
			},
			wantErr: false,
		},
		{
			name: "negative max size",
			config: &VideoConfig{
				MaxSizeMB: -1,
			},
			wantErr: true,
			errMsg:  "max_size_mb cannot be negative",
		},
		{
			name: "invalid format",
			config: &VideoConfig{
				MaxSizeMB:      100,
				AllowedFormats: []string{"invalid"},
			},
			wantErr: true,
			errMsg:  "invalid video format",
		},
		{
			name: "negative duration",
			config: &VideoConfig{
				MaxSizeMB:      100,
				MaxDurationSec: -1,
			},
			wantErr: true,
			errMsg:  "max_duration_sec cannot be negative",
		},
		{
			name: "all valid formats",
			config: &VideoConfig{
				AllowedFormats: []string{"mp4", "webm", "ogg", "mov", "avi"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVideoConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateVideoConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateVideoConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// TestValidateMultimodalExample tests example validation
func TestValidateMultimodalExample(t *testing.T) {
	tests := []struct {
		name    string
		example *MultimodalExample
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid example with text",
			example: &MultimodalExample{
				Name: "test-example",
				Role: "user",
				Parts: []ExampleContentPart{
					{
						Type: "text",
						Text: "Hello world",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			example: &MultimodalExample{
				Role: "user",
				Parts: []ExampleContentPart{
					{Type: "text", Text: "test"},
				},
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "missing role",
			example: &MultimodalExample{
				Name: "test",
				Parts: []ExampleContentPart{
					{Type: "text", Text: "test"},
				},
			},
			wantErr: true,
			errMsg:  "role is required",
		},
		{
			name: "invalid role",
			example: &MultimodalExample{
				Name: "test",
				Role: "invalid",
				Parts: []ExampleContentPart{
					{Type: "text", Text: "test"},
				},
			},
			wantErr: true,
			errMsg:  "invalid role",
		},
		{
			name: "no parts",
			example: &MultimodalExample{
				Name:  "test",
				Role:  "user",
				Parts: []ExampleContentPart{},
			},
			wantErr: true,
			errMsg:  "at least one content part",
		},
		{
			name: "valid image example",
			example: &MultimodalExample{
				Name: "image-test",
				Role: "user",
				Parts: []ExampleContentPart{
					{
						Type: "text",
						Text: "What's this?",
					},
					{
						Type: "image",
						Media: &ExampleMedia{
							FilePath: "./test.jpg",
							MIMEType: "image/jpeg",
							Detail:   "high",
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMultimodalExample(tt.example)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMultimodalExample() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateMultimodalExample() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// TestValidateExampleContentPart tests content part validation
func TestValidateExampleContentPart(t *testing.T) {
	tests := []struct {
		name    string
		part    *ExampleContentPart
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid text part",
			part: &ExampleContentPart{
				Type: "text",
				Text: "Hello",
			},
			wantErr: false,
		},
		{
			name: "text part with empty text",
			part: &ExampleContentPart{
				Type: "text",
				Text: "",
			},
			wantErr: true,
			errMsg:  "non-empty text",
		},
		{
			name: "text part with media",
			part: &ExampleContentPart{
				Type:  "text",
				Text:  "Hello",
				Media: &ExampleMedia{},
			},
			wantErr: true,
			errMsg:  "should not have media",
		},
		{
			name: "image part without media",
			part: &ExampleContentPart{
				Type: "image",
			},
			wantErr: true,
			errMsg:  "must have media",
		},
		{
			name: "image part with text",
			part: &ExampleContentPart{
				Type: "image",
				Text: "invalid",
				Media: &ExampleMedia{
					FilePath: "./test.jpg",
					MIMEType: "image/jpeg",
				},
			},
			wantErr: true,
			errMsg:  "should not have text field",
		},
		{
			name: "valid image part",
			part: &ExampleContentPart{
				Type: "image",
				Media: &ExampleMedia{
					FilePath: "./test.jpg",
					MIMEType: "image/jpeg",
					Detail:   "high",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid content type",
			part: &ExampleContentPart{
				Type: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid content type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExampleContentPart(tt.part)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateExampleContentPart() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateExampleContentPart() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// TestValidateExampleMedia tests media validation
func TestValidateExampleMedia(t *testing.T) {
	tests := []struct {
		name        string
		media       *ExampleMedia
		contentType string
		wantErr     bool
		errMsg      string
	}{
		{
			name: "valid file path",
			media: &ExampleMedia{
				FilePath: "./test.jpg",
				MIMEType: "image/jpeg",
			},
			contentType: "image",
			wantErr:     false,
		},
		{
			name: "valid URL",
			media: &ExampleMedia{
				URL:      "https://example.com/image.jpg",
				MIMEType: "image/jpeg",
			},
			contentType: "image",
			wantErr:     false,
		},
		{
			name: "no source",
			media: &ExampleMedia{
				MIMEType: "image/jpeg",
			},
			contentType: "image",
			wantErr:     true,
			errMsg:      "either file_path or url",
		},
		{
			name: "multiple sources",
			media: &ExampleMedia{
				FilePath: "./test.jpg",
				URL:      "https://example.com/image.jpg",
				MIMEType: "image/jpeg",
			},
			contentType: "image",
			wantErr:     true,
			errMsg:      "exactly one source",
		},
		{
			name: "missing MIME type",
			media: &ExampleMedia{
				FilePath: "./test.jpg",
			},
			contentType: "image",
			wantErr:     true,
			errMsg:      "must have mime_type",
		},
		{
			name: "invalid detail level",
			media: &ExampleMedia{
				FilePath: "./test.jpg",
				MIMEType: "image/jpeg",
				Detail:   "invalid",
			},
			contentType: "image",
			wantErr:     true,
			errMsg:      "invalid detail level",
		},
		{
			name: "valid detail levels",
			media: &ExampleMedia{
				FilePath: "./test.jpg",
				MIMEType: "image/jpeg",
				Detail:   "high",
			},
			contentType: "image",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExampleMedia(tt.media, tt.contentType)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateExampleMedia() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateExampleMedia() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// TestSupportsMediaType tests media type support checking
func TestSupportsMediaType(t *testing.T) {
	config := &MediaConfig{
		Enabled:        true,
		SupportedTypes: []string{"image", "audio"},
	}

	tests := []struct {
		name      string
		config    *MediaConfig
		mediaType string
		want      bool
	}{
		{"supported image", config, "image", true},
		{"supported audio", config, "audio", true},
		{"unsupported video", config, "video", false},
		{"nil config", nil, "image", false},
		{"disabled config", &MediaConfig{Enabled: false, SupportedTypes: []string{"image"}}, "image", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SupportsMediaType(tt.config, tt.mediaType); got != tt.want {
				t.Errorf("SupportsMediaType() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGetMediaConfigs tests the getter functions
func TestGetMediaConfigs(t *testing.T) {
	imageConfig := &ImageConfig{MaxSizeMB: 20}
	audioConfig := &AudioConfig{MaxSizeMB: 25}
	videoConfig := &VideoConfig{MaxSizeMB: 100}

	config := &MediaConfig{
		Enabled:        true,
		SupportedTypes: []string{"image", "audio", "video"},
		Image:          imageConfig,
		Audio:          audioConfig,
		Video:          videoConfig,
	}

	t.Run("GetImageConfig", func(t *testing.T) {
		got := GetImageConfig(config)
		if got != imageConfig {
			t.Errorf("GetImageConfig() = %v, want %v", got, imageConfig)
		}
	})

	t.Run("GetAudioConfig", func(t *testing.T) {
		got := GetAudioConfig(config)
		if got != audioConfig {
			t.Errorf("GetAudioConfig() = %v, want %v", got, audioConfig)
		}
	})

	t.Run("GetVideoConfig", func(t *testing.T) {
		got := GetVideoConfig(config)
		if got != videoConfig {
			t.Errorf("GetVideoConfig() = %v, want %v", got, videoConfig)
		}
	})

	// Test with unsupported type
	configNoImage := &MediaConfig{
		Enabled:        true,
		SupportedTypes: []string{"audio"},
	}

	t.Run("GetImageConfig unsupported", func(t *testing.T) {
		got := GetImageConfig(configNoImage)
		if got != nil {
			t.Errorf("GetImageConfig() = %v, want nil", got)
		}
	})

	t.Run("GetAudioConfig unsupported", func(t *testing.T) {
		configNoAudio := &MediaConfig{
			Enabled:        true,
			SupportedTypes: []string{"image"},
		}
		got := GetAudioConfig(configNoAudio)
		if got != nil {
			t.Errorf("GetAudioConfig() = %v, want nil", got)
		}
	})

	t.Run("GetVideoConfig unsupported", func(t *testing.T) {
		configNoVideo := &MediaConfig{
			Enabled:        true,
			SupportedTypes: []string{"image"},
		}
		got := GetVideoConfig(configNoVideo)
		if got != nil {
			t.Errorf("GetVideoConfig() = %v, want nil", got)
		}
	})

	t.Run("GetAudioConfig nil config", func(t *testing.T) {
		got := GetAudioConfig(nil)
		if got != nil {
			t.Errorf("GetAudioConfig(nil) = %v, want nil", got)
		}
	})

	t.Run("GetVideoConfig nil config", func(t *testing.T) {
		got := GetVideoConfig(nil)
		if got != nil {
			t.Errorf("GetVideoConfig(nil) = %v, want nil", got)
		}
	})
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

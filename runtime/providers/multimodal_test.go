package providers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// mockProvider is a test implementation of Provider interface
type mockProvider struct {
	id string
}

func (m *mockProvider) ID() string { return m.id }
func (m *mockProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, nil
}
func (m *mockProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	return nil, nil
}
func (m *mockProvider) SupportsStreaming() bool      { return false }
func (m *mockProvider) ShouldIncludeRawOutput() bool { return false }
func (m *mockProvider) Close() error                 { return nil }
func (m *mockProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}

// mockMultimodalProvider implements MultimodalSupport
type mockMultimodalProvider struct {
	mockProvider
	capabilities MultimodalCapabilities
}

func (m *mockMultimodalProvider) GetMultimodalCapabilities() MultimodalCapabilities {
	return m.capabilities
}

func (m *mockMultimodalProvider) ChatMultimodal(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, nil
}

func (m *mockMultimodalProvider) ChatMultimodalStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	return nil, nil
}

func TestSupportsMultimodal(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     bool
	}{
		{
			name:     "text-only provider",
			provider: &mockProvider{id: "text-only"},
			want:     false,
		},
		{
			name: "multimodal provider",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "multimodal"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SupportsMultimodal(tt.provider)
			if got != tt.want {
				t.Errorf("SupportsMultimodal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMultimodalProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		wantNil  bool
	}{
		{
			name:     "text-only provider returns nil",
			provider: &mockProvider{id: "text-only"},
			wantNil:  true,
		},
		{
			name: "multimodal provider returns instance",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "multimodal"},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMultimodalProvider(tt.provider)
			if (got == nil) != tt.wantNil {
				t.Errorf("GetMultimodalProvider() nil = %v, want nil = %v", got == nil, tt.wantNil)
			}
		})
	}
}

func TestHasImageSupport(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     bool
	}{
		{
			name:     "text-only provider",
			provider: &mockProvider{id: "text-only"},
			want:     false,
		},
		{
			name: "multimodal provider with images",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "with-images"},
				capabilities: MultimodalCapabilities{
					SupportsImages: true,
				},
			},
			want: true,
		},
		{
			name: "multimodal provider without images",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "no-images"},
				capabilities: MultimodalCapabilities{
					SupportsImages: false,
					SupportsAudio:  true,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasImageSupport(tt.provider)
			if got != tt.want {
				t.Errorf("HasImageSupport() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasAudioSupport(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     bool
	}{
		{
			name:     "text-only provider",
			provider: &mockProvider{id: "text-only"},
			want:     false,
		},
		{
			name: "multimodal provider with audio",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "with-audio"},
				capabilities: MultimodalCapabilities{
					SupportsAudio: true,
				},
			},
			want: true,
		},
		{
			name: "multimodal provider without audio",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "no-audio"},
				capabilities: MultimodalCapabilities{
					SupportsImages: true,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasAudioSupport(tt.provider)
			if got != tt.want {
				t.Errorf("HasAudioSupport() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasVideoSupport(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     bool
	}{
		{
			name:     "text-only provider",
			provider: &mockProvider{id: "text-only"},
			want:     false,
		},
		{
			name: "multimodal provider with video",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "with-video"},
				capabilities: MultimodalCapabilities{
					SupportsVideo: true,
				},
			},
			want: true,
		},
		{
			name: "multimodal provider without video",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "no-video"},
				capabilities: MultimodalCapabilities{
					SupportsImages: true,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasVideoSupport(tt.provider)
			if got != tt.want {
				t.Errorf("HasVideoSupport() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsFormatSupported(t *testing.T) {
	tests := []struct {
		name        string
		provider    Provider
		contentType string
		mimeType    string
		want        bool
	}{
		{
			name:        "text-only provider doesn't support images",
			provider:    &mockProvider{id: "text-only"},
			contentType: types.ContentTypeImage,
			mimeType:    types.MIMETypeImageJPEG,
			want:        false,
		},
		{
			name: "provider with no format restrictions supports all",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "all-formats"},
				capabilities: MultimodalCapabilities{
					SupportsImages: true,
					ImageFormats:   []string{}, // Empty = supports all
				},
			},
			contentType: types.ContentTypeImage,
			mimeType:    types.MIMETypeImageJPEG,
			want:        true,
		},
		{
			name: "provider supports specific format",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "jpeg-only"},
				capabilities: MultimodalCapabilities{
					SupportsImages: true,
					ImageFormats:   []string{types.MIMETypeImageJPEG, types.MIMETypeImagePNG},
				},
			},
			contentType: types.ContentTypeImage,
			mimeType:    types.MIMETypeImageJPEG,
			want:        true,
		},
		{
			name: "provider doesn't support specific format",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "jpeg-only"},
				capabilities: MultimodalCapabilities{
					SupportsImages: true,
					ImageFormats:   []string{types.MIMETypeImageJPEG},
				},
			},
			contentType: types.ContentTypeImage,
			mimeType:    types.MIMETypeImagePNG,
			want:        false,
		},
		{
			name: "audio format check",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "audio-provider"},
				capabilities: MultimodalCapabilities{
					SupportsAudio: true,
					AudioFormats:  []string{types.MIMETypeAudioMP3},
				},
			},
			contentType: types.ContentTypeAudio,
			mimeType:    types.MIMETypeAudioMP3,
			want:        true,
		},
		{
			name: "video format check",
			provider: &mockMultimodalProvider{
				mockProvider: mockProvider{id: "video-provider"},
				capabilities: MultimodalCapabilities{
					SupportsVideo: true,
					VideoFormats:  []string{types.MIMETypeVideoMP4},
				},
			},
			contentType: types.ContentTypeVideo,
			mimeType:    types.MIMETypeVideoMP4,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsFormatSupported(tt.provider, tt.contentType, tt.mimeType)
			if got != tt.want {
				t.Errorf("IsFormatSupported() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateMultimodalMessage(t *testing.T) {
	textOnlyProvider := &mockProvider{id: "text-only"}
	imageProvider := &mockMultimodalProvider{
		mockProvider: mockProvider{id: "image-provider"},
		capabilities: MultimodalCapabilities{
			SupportsImages: true,
			ImageFormats:   []string{types.MIMETypeImageJPEG, types.MIMETypeImagePNG},
		},
	}
	audioProvider := &mockMultimodalProvider{
		mockProvider: mockProvider{id: "audio-provider"},
		capabilities: MultimodalCapabilities{
			SupportsAudio: true,
			AudioFormats:  []string{types.MIMETypeAudioMP3},
		},
	}

	tests := []struct {
		name     string
		provider Provider
		message  types.Message
		wantErr  bool
		errType  string
	}{
		{
			name:     "text-only message is always valid",
			provider: textOnlyProvider,
			message: types.Message{
				Role:    "user",
				Content: "Hello",
			},
			wantErr: false,
		},
		{
			name:     "multimodal message on text-only provider fails",
			provider: textOnlyProvider,
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewTextPart("Hello"),
					types.NewImagePartFromURL("https://example.com/image.jpg", nil),
				},
			},
			wantErr: true,
			errType: "multimodal",
		},
		{
			name:     "supported image format passes",
			provider: imageProvider,
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewTextPart("What's this?"),
					types.NewImagePartFromURL("https://example.com/image.jpg", nil),
				},
			},
			wantErr: false,
		},
		{
			name:     "unsupported image format fails",
			provider: imageProvider,
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewImagePartFromData("base64data", types.MIMETypeImageGIF, nil),
				},
			},
			wantErr: true,
			errType: "image",
		},
		{
			name:     "audio on image-only provider fails",
			provider: imageProvider,
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewAudioPartFromData("base64audio", types.MIMETypeAudioMP3),
				},
			},
			wantErr: true,
			errType: "audio",
		},
		{
			name:     "supported audio format passes",
			provider: audioProvider,
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					types.NewTextPart("Transcribe this:"),
					types.NewAudioPartFromData("base64audio", types.MIMETypeAudioMP3),
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMultimodalMessage(tt.provider, tt.message)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMultimodalMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if contentErr, ok := err.(*UnsupportedContentError); ok {
					if tt.errType != "" && contentErr.ContentType != tt.errType {
						t.Errorf("Expected error type %q, got %q", tt.errType, contentErr.ContentType)
					}
				} else {
					t.Errorf("Expected UnsupportedContentError, got %T", err)
				}
			}
		})
	}
}

func TestUnsupportedContentError(t *testing.T) {
	tests := []struct {
		name    string
		err     *UnsupportedContentError
		wantMsg string
	}{
		{
			name: "basic error",
			err: &UnsupportedContentError{
				Provider:    "test-provider",
				ContentType: "image",
				Message:     "not supported",
				PartIndex:   -1,
			},
			wantMsg: "test-provider: not supported",
		},
		{
			name: "error with part index",
			err: &UnsupportedContentError{
				Provider:    "test-provider",
				ContentType: "audio",
				Message:     "unsupported format",
				PartIndex:   2,
			},
			wantMsg: "test-provider: unsupported format (part \x02)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestMultimodalCapabilitiesStructure(t *testing.T) {
	// Test that we can create and use capabilities struct
	caps := MultimodalCapabilities{
		SupportsImages: true,
		SupportsAudio:  true,
		SupportsVideo:  false,
		ImageFormats:   []string{types.MIMETypeImageJPEG, types.MIMETypeImagePNG},
		AudioFormats:   []string{types.MIMETypeAudioMP3, types.MIMETypeAudioWAV},
		VideoFormats:   []string{},
		MaxImageSizeMB: 10,
		MaxAudioSizeMB: 25,
		MaxVideoSizeMB: 0,
	}

	if !caps.SupportsImages {
		t.Error("Expected SupportsImages to be true")
	}
	if !caps.SupportsAudio {
		t.Error("Expected SupportsAudio to be true")
	}
	if caps.SupportsVideo {
		t.Error("Expected SupportsVideo to be false")
	}
	if len(caps.ImageFormats) != 2 {
		t.Errorf("Expected 2 image formats, got %d", len(caps.ImageFormats))
	}
	if caps.MaxImageSizeMB != 10 {
		t.Errorf("Expected MaxImageSizeMB to be 10, got %d", caps.MaxImageSizeMB)
	}
}

func TestImageDetailConstants(t *testing.T) {
	// Test that image detail constants are properly defined
	if ImageDetailLow != "low" {
		t.Errorf("ImageDetailLow = %q, want %q", ImageDetailLow, "low")
	}
	if ImageDetailHigh != "high" {
		t.Errorf("ImageDetailHigh = %q, want %q", ImageDetailHigh, "high")
	}
	if ImageDetailAuto != "auto" {
		t.Errorf("ImageDetailAuto = %q, want %q", ImageDetailAuto, "auto")
	}
}

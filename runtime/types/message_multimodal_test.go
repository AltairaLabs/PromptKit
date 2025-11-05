package types

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMessageGetContent(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    string
	}{
		{
			name: "legacy text content",
			message: Message{
				Role:    "user",
				Content: "Hello, world!",
			},
			want: "Hello, world!",
		},
		{
			name: "multimodal with single text part",
			message: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Hello from parts"),
				},
			},
			want: "Hello from parts",
		},
		{
			name: "multimodal with multiple text parts",
			message: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("First part. "),
					NewTextPart("Second part."),
				},
			},
			want: "First part. Second part.",
		},
		{
			name: "multimodal with text and image",
			message: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Here's an image: "),
					NewImagePartFromURL("https://example.com/image.jpg", nil),
					NewTextPart(" What do you see?"),
				},
			},
			want: "Here's an image:  What do you see?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.message.GetContent()
			if got != tt.want {
				t.Errorf("GetContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMessageIsMultimodal(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    bool
	}{
		{
			name: "legacy text only",
			message: Message{
				Role:    "user",
				Content: "Hello",
			},
			want: false,
		},
		{
			name: "with parts",
			message: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Hello"),
				},
			},
			want: true,
		},
		{
			name: "empty message",
			message: Message{
				Role: "user",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.message.IsMultimodal()
			if got != tt.want {
				t.Errorf("IsMultimodal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessageHasMediaContent(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    bool
	}{
		{
			name: "text only",
			message: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Hello"),
				},
			},
			want: false,
		},
		{
			name: "with image",
			message: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Check this out:"),
					NewImagePartFromURL("https://example.com/image.jpg", nil),
				},
			},
			want: true,
		},
		{
			name: "with audio",
			message: Message{
				Role: "user",
				Parts: []ContentPart{
					NewAudioPartFromData("base64audio", MIMETypeAudioMP3),
				},
			},
			want: true,
		},
		{
			name: "with video",
			message: Message{
				Role: "user",
				Parts: []ContentPart{
					NewVideoPartFromData("base64video", MIMETypeVideoMP4),
				},
			},
			want: true,
		},
		{
			name: "legacy content",
			message: Message{
				Role:    "user",
				Content: "Hello",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.message.HasMediaContent()
			if got != tt.want {
				t.Errorf("HasMediaContent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessageSetTextContent(t *testing.T) {
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			NewTextPart("Old text"),
			NewImagePartFromURL("https://example.com/image.jpg", nil),
		},
	}

	msg.SetTextContent("New text content")

	if msg.Content != "New text content" {
		t.Errorf("Content = %q, want %q", msg.Content, "New text content")
	}
	if len(msg.Parts) != 0 {
		t.Errorf("Parts should be cleared, got %d parts", len(msg.Parts))
	}
}

func TestMessageSetMultimodalContent(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Old content",
	}

	parts := []ContentPart{
		NewTextPart("New text"),
		NewImagePartFromURL("https://example.com/image.jpg", nil),
	}

	msg.SetMultimodalContent(parts)

	if msg.Content != "" {
		t.Errorf("Content should be cleared, got %q", msg.Content)
	}
	if len(msg.Parts) != 2 {
		t.Errorf("Parts length = %d, want 2", len(msg.Parts))
	}
}

func TestMessageAddPart(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Initial content",
	}

	// First part should clear Content
	msg.AddPart(NewTextPart("First part"))
	if msg.Content != "" {
		t.Errorf("Content should be cleared after first AddPart, got %q", msg.Content)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts length = %d, want 1", len(msg.Parts))
	}

	// Second part should just append
	msg.AddPart(NewTextPart("Second part"))
	if len(msg.Parts) != 2 {
		t.Errorf("Parts length = %d, want 2", len(msg.Parts))
	}
}

func TestMessageAddTextPart(t *testing.T) {
	msg := Message{Role: "user"}
	msg.AddTextPart("Hello, world!")

	if len(msg.Parts) != 1 {
		t.Fatalf("Parts length = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Type != ContentTypeText {
		t.Errorf("Part type = %s, want %s", msg.Parts[0].Type, ContentTypeText)
	}
	if msg.Parts[0].Text == nil || *msg.Parts[0].Text != "Hello, world!" {
		t.Errorf("Part text = %v, want %q", msg.Parts[0].Text, "Hello, world!")
	}
}

func TestMessageAddImagePartFromURL(t *testing.T) {
	msg := Message{Role: "user"}
	detail := "high"
	msg.AddImagePartFromURL("https://example.com/image.jpg", &detail)

	if len(msg.Parts) != 1 {
		t.Fatalf("Parts length = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Type != ContentTypeImage {
		t.Errorf("Part type = %s, want %s", msg.Parts[0].Type, ContentTypeImage)
	}
	if msg.Parts[0].Media == nil {
		t.Fatal("Part media is nil")
	}
	if msg.Parts[0].Media.URL == nil || *msg.Parts[0].Media.URL != "https://example.com/image.jpg" {
		t.Errorf("Media URL = %v, want %q", msg.Parts[0].Media.URL, "https://example.com/image.jpg")
	}
	if msg.Parts[0].Media.Detail == nil || *msg.Parts[0].Media.Detail != detail {
		t.Errorf("Media detail = %v, want %q", msg.Parts[0].Media.Detail, detail)
	}
}

func TestMessageAddImagePart(t *testing.T) {
	// Create a temporary image file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.jpg")
	if err := os.WriteFile(tmpFile, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	msg := Message{Role: "user"}
	detail := "low"
	err := msg.AddImagePart(tmpFile, &detail)
	if err != nil {
		t.Fatalf("AddImagePart() error = %v", err)
	}

	if len(msg.Parts) != 1 {
		t.Fatalf("Parts length = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Type != ContentTypeImage {
		t.Errorf("Part type = %s, want %s", msg.Parts[0].Type, ContentTypeImage)
	}
	if msg.Parts[0].Media == nil {
		t.Fatal("Part media is nil")
	}
	if msg.Parts[0].Media.FilePath == nil || *msg.Parts[0].Media.FilePath != tmpFile {
		t.Errorf("Media FilePath = %v, want %q", msg.Parts[0].Media.FilePath, tmpFile)
	}
}

func TestMessageAddAudioPart(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	msg := Message{Role: "user"}
	err := msg.AddAudioPart(tmpFile)
	if err != nil {
		t.Fatalf("AddAudioPart() error = %v", err)
	}

	if len(msg.Parts) != 1 {
		t.Fatalf("Parts length = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Type != ContentTypeAudio {
		t.Errorf("Part type = %s, want %s", msg.Parts[0].Type, ContentTypeAudio)
	}
	if msg.Parts[0].Media == nil {
		t.Fatal("Part media is nil")
	}
	if msg.Parts[0].Media.MIMEType != MIMETypeAudioMP3 {
		t.Errorf("Media MIMEType = %s, want %s", msg.Parts[0].Media.MIMEType, MIMETypeAudioMP3)
	}
}

func TestMessageAddVideoPart(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp4")
	if err := os.WriteFile(tmpFile, []byte("fake video data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	msg := Message{Role: "user"}
	err := msg.AddVideoPart(tmpFile)
	if err != nil {
		t.Fatalf("AddVideoPart() error = %v", err)
	}

	if len(msg.Parts) != 1 {
		t.Fatalf("Parts length = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Type != ContentTypeVideo {
		t.Errorf("Part type = %s, want %s", msg.Parts[0].Type, ContentTypeVideo)
	}
	if msg.Parts[0].Media == nil {
		t.Fatal("Part media is nil")
	}
	if msg.Parts[0].Media.MIMEType != MIMETypeVideoMP4 {
		t.Errorf("Media MIMEType = %s, want %s", msg.Parts[0].Media.MIMEType, MIMETypeVideoMP4)
	}
}

func TestMessageBackwardCompatibility(t *testing.T) {
	// Test that legacy Content field still works
	msg := Message{
		Role:    "user",
		Content: "Legacy text content",
	}

	// GetContent should return the Content field
	if msg.GetContent() != "Legacy text content" {
		t.Errorf("GetContent() for legacy message = %q, want %q", msg.GetContent(), "Legacy text content")
	}

	// IsMultimodal should return false
	if msg.IsMultimodal() {
		t.Error("IsMultimodal() for legacy message = true, want false")
	}

	// HasMediaContent should return false
	if msg.HasMediaContent() {
		t.Error("HasMediaContent() for legacy message = true, want false")
	}
}

func TestMessageMultimodalWorkflow(t *testing.T) {
	// Simulate a typical multimodal message construction
	msg := Message{
		Role: "user",
	}

	// Add text
	msg.AddTextPart("Please analyze this image and audio:")

	// Add image
	tmpDir := t.TempDir()
	imgFile := filepath.Join(tmpDir, "chart.png")
	if err := os.WriteFile(imgFile, []byte("fake image"), 0644); err != nil {
		t.Fatalf("failed to create image file: %v", err)
	}
	if err := msg.AddImagePart(imgFile, nil); err != nil {
		t.Fatalf("AddImagePart() error = %v", err)
	}

	// Add more text
	msg.AddTextPart(" And here's the audio:")

	// Add audio
	audioFile := filepath.Join(tmpDir, "recording.mp3")
	if err := os.WriteFile(audioFile, []byte("fake audio"), 0644); err != nil {
		t.Fatalf("failed to create audio file: %v", err)
	}
	if err := msg.AddAudioPart(audioFile); err != nil {
		t.Fatalf("AddAudioPart() error = %v", err)
	}

	// Verify the message structure
	if !msg.IsMultimodal() {
		t.Error("Message should be multimodal")
	}
	if !msg.HasMediaContent() {
		t.Error("Message should have media content")
	}
	if len(msg.Parts) != 4 {
		t.Errorf("Message should have 4 parts, got %d", len(msg.Parts))
	}

	// Verify content extraction includes only text
	content := msg.GetContent()
	if content != "Please analyze this image and audio: And here's the audio:" {
		t.Errorf("GetContent() = %q, unexpected value", content)
	}
}

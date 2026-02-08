package a2a

import (
	"encoding/base64"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestInferContentType(t *testing.T) {
	tests := []struct {
		mediaType string
		want      string
	}{
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"audio/wav", "audio"},
		{"audio/mpeg", "audio"},
		{"video/mp4", "video"},
		{"video/webm", "video"},
		{"application/pdf", "document"},
		{"text/plain", "document"},
		{"text/html", "document"},
		{"application/octet-stream", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			got := InferContentType(tt.mediaType)
			if got != tt.want {
				t.Errorf("InferContentType(%q) = %q, want %q", tt.mediaType, got, tt.want)
			}
		})
	}
}

func TestPartToContentPart(t *testing.T) {
	rawData := []byte("hello image")
	b64 := base64.StdEncoding.EncodeToString(rawData)

	tests := []struct {
		name    string
		part    Part
		want    types.ContentPart
		wantErr bool
	}{
		{
			name: "text part",
			part: Part{Text: ptr("hello")},
			want: types.ContentPart{Type: "text", Text: ptr("hello")},
		},
		{
			name: "raw+image",
			part: Part{Raw: rawData, MediaType: "image/png"},
			want: types.ContentPart{
				Type: "image",
				Media: &types.MediaContent{
					Data:     &b64,
					MIMEType: "image/png",
				},
			},
		},
		{
			name: "raw+audio",
			part: Part{Raw: rawData, MediaType: "audio/wav"},
			want: types.ContentPart{
				Type: "audio",
				Media: &types.MediaContent{
					Data:     &b64,
					MIMEType: "audio/wav",
				},
			},
		},
		{
			name: "raw+video",
			part: Part{Raw: rawData, MediaType: "video/mp4"},
			want: types.ContentPart{
				Type: "video",
				Media: &types.MediaContent{
					Data:     &b64,
					MIMEType: "video/mp4",
				},
			},
		},
		{
			name: "url+image",
			part: Part{URL: ptr("https://example.com/img.png"), MediaType: "image/png"},
			want: types.ContentPart{
				Type: "image",
				Media: &types.MediaContent{
					URL:      ptr("https://example.com/img.png"),
					MIMEType: "image/png",
				},
			},
		},
		{
			name:    "data part returns error",
			part:    Part{Data: map[string]any{"key": "val"}},
			wantErr: true,
		},
		{
			name:    "empty part returns error",
			part:    Part{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PartToContentPart(&tt.part)
			if (err != nil) != tt.wantErr {
				t.Fatalf("PartToContentPart() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if tt.want.Text != nil {
				if got.Text == nil || *got.Text != *tt.want.Text {
					t.Errorf("Text = %v, want %v", got.Text, *tt.want.Text)
				}
			}
			if tt.want.Media != nil {
				if got.Media == nil {
					t.Fatal("Media is nil, want non-nil")
				}
				if got.Media.MIMEType != tt.want.Media.MIMEType {
					t.Errorf("MIMEType = %q, want %q", got.Media.MIMEType, tt.want.Media.MIMEType)
				}
				if tt.want.Media.Data != nil {
					if got.Media.Data == nil || *got.Media.Data != *tt.want.Media.Data {
						t.Errorf("Data = %v, want %v", got.Media.Data, *tt.want.Media.Data)
					}
				}
				if tt.want.Media.URL != nil {
					if got.Media.URL == nil || *got.Media.URL != *tt.want.Media.URL {
						t.Errorf("URL = %v, want %v", got.Media.URL, *tt.want.Media.URL)
					}
				}
			}
		})
	}
}

func TestContentPartToA2APart(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("image bytes"))

	tests := []struct {
		name    string
		part    types.ContentPart
		check   func(t *testing.T, got Part)
		wantErr bool
	}{
		{
			name: "text",
			part: types.ContentPart{Type: "text", Text: ptr("hello")},
			check: func(t *testing.T, got Part) {
				if got.Text == nil || *got.Text != "hello" {
					t.Errorf("Text = %v, want 'hello'", got.Text)
				}
			},
		},
		{
			name: "media with data",
			part: types.ContentPart{
				Type: "image",
				Media: &types.MediaContent{
					Data:     &b64,
					MIMEType: "image/png",
				},
			},
			check: func(t *testing.T, got Part) {
				if got.Raw == nil {
					t.Fatal("Raw is nil")
				}
				if string(got.Raw) != "image bytes" {
					t.Errorf("Raw = %q, want 'image bytes'", string(got.Raw))
				}
				if got.MediaType != "image/png" {
					t.Errorf("MediaType = %q, want 'image/png'", got.MediaType)
				}
			},
		},
		{
			name: "media with url",
			part: types.ContentPart{
				Type: "image",
				Media: &types.MediaContent{
					URL:      ptr("https://example.com/img.png"),
					MIMEType: "image/png",
				},
			},
			check: func(t *testing.T, got Part) {
				if got.URL == nil || *got.URL != "https://example.com/img.png" {
					t.Errorf("URL = %v, want 'https://example.com/img.png'", got.URL)
				}
				if got.MediaType != "image/png" {
					t.Errorf("MediaType = %q, want 'image/png'", got.MediaType)
				}
			},
		},
		{
			name:    "empty part returns error",
			part:    types.ContentPart{Type: "text"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ContentPartToA2APart(tt.part)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ContentPartToA2APart() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			tt.check(t, got)
		})
	}
}

func TestMessageToMessage(t *testing.T) {
	msg := Message{
		Role: RoleAgent,
		Parts: []Part{
			{Text: ptr("Hello ")},
			{Text: ptr("world")},
			{Raw: []byte("img"), MediaType: "image/png"},
		},
		Metadata: map[string]any{"key": "value"},
	}

	got, err := MessageToMessage(&msg)
	if err != nil {
		t.Fatalf("MessageToMessage() error = %v", err)
	}

	if got.Role != "assistant" {
		t.Errorf("Role = %q, want 'assistant'", got.Role)
	}
	if len(got.Parts) != 3 {
		t.Fatalf("len(Parts) = %d, want 3", len(got.Parts))
	}
	if got.Parts[0].Type != "text" || *got.Parts[0].Text != "Hello " {
		t.Errorf("Parts[0] = %+v, want text 'Hello '", got.Parts[0])
	}
	if got.Parts[1].Type != "text" || *got.Parts[1].Text != "world" {
		t.Errorf("Parts[1] = %+v, want text 'world'", got.Parts[1])
	}
	if got.Parts[2].Type != "image" {
		t.Errorf("Parts[2].Type = %q, want 'image'", got.Parts[2].Type)
	}
	if got.Content != "Hello world" {
		t.Errorf("Content = %q, want 'Hello world'", got.Content)
	}
	if got.Meta["key"] != "value" {
		t.Errorf("Meta[key] = %v, want 'value'", got.Meta["key"])
	}
}

func TestMessageToMessage_UserRole(t *testing.T) {
	msg := Message{
		Role:  RoleUser,
		Parts: []Part{{Text: ptr("hi")}},
	}
	got, err := MessageToMessage(&msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Role != "user" {
		t.Errorf("Role = %q, want 'user'", got.Role)
	}
}

func TestContentPartsToArtifacts(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("img"))

	parts := []types.ContentPart{
		{Type: "text", Text: ptr("result text")},
		{
			Type: "image",
			Media: &types.MediaContent{
				Data:     &b64,
				MIMEType: "image/png",
			},
		},
	}

	artifacts, err := ContentPartsToArtifacts(parts)
	if err != nil {
		t.Fatalf("ContentPartsToArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1", len(artifacts))
	}
	if artifacts[0].ArtifactID != "artifact-1" {
		t.Errorf("ArtifactID = %q, want 'artifact-1'", artifacts[0].ArtifactID)
	}
	if len(artifacts[0].Parts) != 2 {
		t.Fatalf("len(Parts) = %d, want 2", len(artifacts[0].Parts))
	}
	if artifacts[0].Parts[0].Text == nil || *artifacts[0].Parts[0].Text != "result text" {
		t.Errorf("Parts[0] text = %v, want 'result text'", artifacts[0].Parts[0].Text)
	}
	if artifacts[0].Parts[1].MediaType != "image/png" {
		t.Errorf("Parts[1].MediaType = %q, want 'image/png'", artifacts[0].Parts[1].MediaType)
	}
}

func TestContentPartsToArtifacts_Empty(t *testing.T) {
	artifacts, err := ContentPartsToArtifacts(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if artifacts != nil {
		t.Errorf("expected nil, got %v", artifacts)
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		orig types.ContentPart
	}{
		{
			name: "text",
			orig: types.ContentPart{Type: "text", Text: ptr("round trip text")},
		},
		{
			name: "image data",
			orig: types.ContentPart{
				Type: "image",
				Media: &types.MediaContent{
					Data:     ptr(base64.StdEncoding.EncodeToString([]byte("pixel data"))),
					MIMEType: "image/png",
				},
			},
		},
		{
			name: "image url",
			orig: types.ContentPart{
				Type: "image",
				Media: &types.MediaContent{
					URL:      ptr("https://example.com/photo.jpg"),
					MIMEType: "image/jpeg",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a2aPart, err := ContentPartToA2APart(tt.orig)
			if err != nil {
				t.Fatalf("ContentPartToA2APart() error = %v", err)
			}

			back, err := PartToContentPart(&a2aPart)
			if err != nil {
				t.Fatalf("PartToContentPart() error = %v", err)
			}

			if back.Type != tt.orig.Type {
				t.Errorf("Type = %q, want %q", back.Type, tt.orig.Type)
			}

			if tt.orig.Text != nil {
				if back.Text == nil || *back.Text != *tt.orig.Text {
					t.Errorf("Text = %v, want %q", back.Text, *tt.orig.Text)
				}
			}

			if tt.orig.Media != nil {
				if back.Media == nil {
					t.Fatal("Media is nil after round-trip")
				}
				if back.Media.MIMEType != tt.orig.Media.MIMEType {
					t.Errorf("MIMEType = %q, want %q", back.Media.MIMEType, tt.orig.Media.MIMEType)
				}
				if tt.orig.Media.Data != nil {
					if back.Media.Data == nil || *back.Media.Data != *tt.orig.Media.Data {
						t.Errorf("Data mismatch after round-trip")
					}
				}
				if tt.orig.Media.URL != nil {
					if back.Media.URL == nil || *back.Media.URL != *tt.orig.Media.URL {
						t.Errorf("URL mismatch after round-trip")
					}
				}
			}
		})
	}
}

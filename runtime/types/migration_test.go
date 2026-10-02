package types

import (
	"reflect"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/testutil"
)

func TestMigrateToMultimodal(t *testing.T) {
	tests := []struct {
		name     string
		input    Message
		wantText string
		wantLen  int
	}{
		{
			name: "legacy text message",
			input: Message{
				Role:    "user",
				Content: "Hello, world!",
			},
			wantText: "Hello, world!",
			wantLen:  1,
		},
		{
			name: "empty legacy message",
			input: Message{
				Role:    "user",
				Content: "",
			},
			wantText: "",
			wantLen:  0,
		},
		{
			name: "already multimodal",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Already multimodal"),
				},
			},
			wantText: "Already multimodal",
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.input
			MigrateToMultimodal(&msg)

			if !msg.IsMultimodal() && tt.wantLen > 0 {
				t.Error("Message should be multimodal after migration")
			}
			if msg.Content != "" && len(msg.Parts) > 0 {
				t.Error("Content should be empty after migration to Parts")
			}
			if len(msg.Parts) != tt.wantLen {
				t.Errorf("Parts length = %d, want %d", len(msg.Parts), tt.wantLen)
			}
			if msg.GetContent() != tt.wantText {
				t.Errorf("GetContent() = %q, want %q", msg.GetContent(), tt.wantText)
			}
		})
	}
}

func TestMigrateToLegacy(t *testing.T) {
	tests := []struct {
		name    string
		input   Message
		wantErr bool
		want    string
	}{
		{
			name: "text-only multimodal",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Hello from Parts"),
				},
			},
			wantErr: false,
			want:    "Hello from Parts",
		},
		{
			name: "multiple text parts",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Part 1. "),
					NewTextPart("Part 2."),
				},
			},
			wantErr: false,
			want:    "Part 1. Part 2.",
		},
		{
			name: "already legacy",
			input: Message{
				Role:    "user",
				Content: "Legacy content",
			},
			wantErr: false,
			want:    "Legacy content",
		},
		{
			name: "with image - should fail",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Text"),
					NewImagePartFromURL("https://example.com/image.jpg", nil),
				},
			},
			wantErr: true,
		},
		{
			name: "with audio - should fail",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					NewAudioPartFromData("base64", MIMETypeAudioMP3),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.input
			err := MigrateToLegacy(&msg)

			if (err != nil) != tt.wantErr {
				t.Errorf("MigrateToLegacy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if msg.IsMultimodal() {
					t.Error("Message should not be multimodal after legacy migration")
				}
				if msg.Content != tt.want {
					t.Errorf("Content = %q, want %q", msg.Content, tt.want)
				}
				if len(msg.Parts) != 0 {
					t.Errorf("Parts should be empty after legacy migration, got %d", len(msg.Parts))
				}
			}
		})
	}
}

func TestMigrateMessagesToMultimodal(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "First message"},
		{Role: "assistant", Content: "Second message"},
		{Role: "user", Content: "Third message"},
	}

	MigrateMessagesToMultimodal(messages)

	for i, msg := range messages {
		if !msg.IsMultimodal() {
			t.Errorf("Message %d should be multimodal", i)
		}
		if msg.Content != "" {
			t.Errorf("Message %d Content should be empty", i)
		}
		if len(msg.Parts) != 1 {
			t.Errorf("Message %d should have 1 part, got %d", i, len(msg.Parts))
		}
	}
}

func TestMigrateMessagesToLegacy(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		wantErr  bool
	}{
		{
			name: "text-only messages",
			messages: []Message{
				{Role: "user", Parts: []ContentPart{NewTextPart("First")}},
				{Role: "assistant", Parts: []ContentPart{NewTextPart("Second")}},
			},
			wantErr: false,
		},
		{
			name: "message with image fails",
			messages: []Message{
				{Role: "user", Parts: []ContentPart{NewTextPart("First")}},
				{Role: "user", Parts: []ContentPart{
					NewTextPart("With image"),
					NewImagePartFromURL("https://example.com/img.jpg", nil),
				}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MigrateMessagesToLegacy(tt.messages)
			if (err != nil) != tt.wantErr {
				t.Errorf("MigrateMessagesToLegacy() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				for i, msg := range tt.messages {
					if msg.IsMultimodal() {
						t.Errorf("Message %d should not be multimodal", i)
					}
				}
			}
		})
	}
}

func TestCloneMessage(t *testing.T) {
	original := Message{
		Role:    "user",
		Content: "Original content",
		Parts: []ContentPart{
			NewTextPart("Text part"),
			NewImagePartFromURL("https://example.com/image.jpg", nil),
		},
		Meta: map[string]interface{}{
			"key": "value",
		},
	}

	clone := CloneMessage(original)

	// Verify deep copy
	if clone.Role != original.Role {
		t.Error("Role not cloned correctly")
	}
	if clone.Content != original.Content {
		t.Error("Content not cloned correctly")
	}
	if len(clone.Parts) != len(original.Parts) {
		t.Error("Parts not cloned correctly")
	}

	// Modify clone and verify original unchanged
	clone.Content = "Modified"
	clone.Parts[0].Text = testutil.Ptr("Modified text")
	clone.Meta["key"] = "modified"

	if original.Content == "Modified" {
		t.Error("Original Content was modified")
	}
	if *original.Parts[0].Text == "Modified text" {
		t.Error("Original Parts were modified")
	}
	if original.Meta["key"] == "modified" {
		t.Error("Original Meta was modified")
	}
}

func TestCloneMessage_MediaOptionalFieldsAreDeepCopied(t *testing.T) {
	media := &MediaContent{
		MIMEType: MIMETypeVideoMP4,
		Data:     testutil.Ptr("ZGF0YQ=="),
		FilePath: testutil.Ptr("/tmp/clip.mp4"),
		URL:      testutil.Ptr("https://example.com/clip.mp4"),
		Format:   testutil.Ptr("mp4"),
		SizeKB:   testutil.Ptr(int64(512)),
		Detail:   testutil.Ptr("high"),
		Caption:  testutil.Ptr("a clip"),
		Duration: testutil.Ptr(12),
		BitRate:  testutil.Ptr(128),
		Channels: testutil.Ptr(2),
		Width:    testutil.Ptr(640),
		Height:   testutil.Ptr(480),
		FPS:      testutil.Ptr(30),
	}
	original := Message{Role: "user", Parts: []ContentPart{{Type: ContentTypeVideo, Media: media}}}

	got := CloneMessage(original).Parts[0].Media

	if got == media {
		t.Fatal("Media pointer was shared, want a copy")
	}
	if got.MIMEType != media.MIMEType || *got.Data != *media.Data || *got.FilePath != *media.FilePath ||
		*got.URL != *media.URL || *got.Format != *media.Format || *got.SizeKB != *media.SizeKB ||
		*got.Detail != *media.Detail || *got.Caption != *media.Caption || *got.Duration != *media.Duration ||
		*got.BitRate != *media.BitRate || *got.Channels != *media.Channels || *got.Width != *media.Width ||
		*got.Height != *media.Height || *got.FPS != *media.FPS {
		t.Fatalf("clone differs from original: got %+v, want %+v", *got, *media)
	}

	*got.URL, *got.SizeKB, *got.FPS = "https://example.com/other.mp4", 1, 60
	if *media.URL != "https://example.com/clip.mp4" || *media.SizeKB != 512 || *media.FPS != 30 {
		t.Error("modifying the clone changed the original media")
	}
}

// TestCloneMessage_EveryMediaFieldIsCopied populates every MediaContent field
// by reflection, so a field added to the struct later fails this test until
// cloneMediaContent copies it.
func TestCloneMessage_EveryMediaFieldIsCopied(t *testing.T) {
	media := &MediaContent{}
	v := reflect.ValueOf(media).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		switch {
		case f.Kind() == reflect.String:
			f.SetString("x-" + v.Type().Field(i).Name)
		case f.Kind() == reflect.Ptr && f.Type().Elem().Kind() == reflect.String:
			s := "x-" + v.Type().Field(i).Name
			f.Set(reflect.ValueOf(&s))
		case f.Kind() == reflect.Ptr && f.Type().Elem().Kind() == reflect.Int:
			n := i + 1
			f.Set(reflect.ValueOf(&n))
		case f.Kind() == reflect.Ptr && f.Type().Elem().Kind() == reflect.Int64:
			n := int64(i + 1)
			f.Set(reflect.ValueOf(&n))
		default:
			t.Fatalf("MediaContent.%s has kind %s: extend this test to populate it", v.Type().Field(i).Name, f.Type())
		}
	}
	original := Message{Role: "user", Parts: []ContentPart{{Type: ContentTypeImage, Media: media}}}

	got := CloneMessage(original).Parts[0].Media

	if !reflect.DeepEqual(got, media) {
		t.Fatalf("clone differs from original:\n got  %+v\n want %+v", *got, *media)
	}
	gv := reflect.ValueOf(got).Elem()
	for i := 0; i < gv.NumField(); i++ {
		if gv.Field(i).Kind() == reflect.Ptr && gv.Field(i).Pointer() == v.Field(i).Pointer() {
			t.Errorf("MediaContent.%s shares its pointer with the original", gv.Type().Field(i).Name)
		}
	}
}

func TestCloneMessageWithToolCalls(t *testing.T) {
	original := Message{
		Role: "assistant",
		ToolCalls: []MessageToolCall{
			{ID: "call1", Name: "tool1"},
		},
		CostInfo: &CostInfo{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	clone := CloneMessage(original)

	// Verify tool calls cloned
	if len(clone.ToolCalls) != 1 {
		t.Error("ToolCalls not cloned")
	}

	// Verify cost info cloned
	if clone.CostInfo == nil || clone.CostInfo.InputTokens != 100 {
		t.Error("CostInfo not cloned correctly")
	}

	// Modify and verify independence
	clone.ToolCalls[0].Name = "modified"
	clone.CostInfo.InputTokens = 999

	if original.ToolCalls[0].Name == "modified" {
		t.Error("Original ToolCalls modified")
	}
	if original.CostInfo.InputTokens == 999 {
		t.Error("Original CostInfo modified")
	}
}

func TestConvertTextToMultimodal(t *testing.T) {
	msg := ConvertTextToMultimodal("user", "Hello, world!")

	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if !msg.IsMultimodal() {
		t.Error("Message should be multimodal")
	}
	if len(msg.Parts) != 1 {
		t.Errorf("Parts length = %d, want 1", len(msg.Parts))
	}
	if msg.GetContent() != "Hello, world!" {
		t.Errorf("GetContent() = %q, want %q", msg.GetContent(), "Hello, world!")
	}
}

func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want string
	}{
		{
			name: "legacy message",
			msg:  Message{Role: "user", Content: "Legacy text"},
			want: "Legacy text",
		},
		{
			name: "multimodal text only",
			msg: Message{
				Role:  "user",
				Parts: []ContentPart{NewTextPart("Multimodal text")},
			},
			want: "Multimodal text",
		},
		{
			name: "multimodal with image",
			msg: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Text before "),
					NewImagePartFromURL("https://example.com/img.jpg", nil),
					NewTextPart(" text after"),
				},
			},
			want: "Text before  text after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTextContent(tt.msg)
			if got != tt.want {
				t.Errorf("ExtractTextContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasOnlyTextContent(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "legacy text",
			msg:  Message{Role: "user", Content: "Text"},
			want: true,
		},
		{
			name: "multimodal text only",
			msg: Message{
				Role:  "user",
				Parts: []ContentPart{NewTextPart("Text")},
			},
			want: true,
		},
		{
			name: "multimodal with image",
			msg: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Text"),
					NewImagePartFromURL("https://example.com/img.jpg", nil),
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasOnlyTextContent(tt.msg)
			if got != tt.want {
				t.Errorf("HasOnlyTextContent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitMultimodalMessage(t *testing.T) {
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			NewTextPart("Here's an image: "),
			NewImagePartFromURL("https://example.com/img.jpg", nil),
			NewTextPart(" and some audio: "),
			NewAudioPartFromData("base64", MIMETypeAudioMP3),
		},
	}

	text, mediaParts := SplitMultimodalMessage(msg)

	if text != "Here's an image:  and some audio: " {
		t.Errorf("text = %q, unexpected value", text)
	}
	if len(mediaParts) != 2 {
		t.Errorf("mediaParts length = %d, want 2", len(mediaParts))
	}
	if mediaParts[0].Type != ContentTypeImage {
		t.Error("First media part should be image")
	}
	if mediaParts[1].Type != ContentTypeAudio {
		t.Error("Second media part should be audio")
	}
}

func TestSplitMultimodalMessageLegacy(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Legacy text",
	}

	text, mediaParts := SplitMultimodalMessage(msg)

	if text != "Legacy text" {
		t.Errorf("text = %q, want %q", text, "Legacy text")
	}
	if len(mediaParts) != 0 {
		t.Errorf("mediaParts should be empty for legacy message, got %d", len(mediaParts))
	}
}

func TestCombineTextAndMedia(t *testing.T) {
	mediaParts := []ContentPart{
		NewImagePartFromURL("https://example.com/img.jpg", nil),
		NewAudioPartFromData("base64", MIMETypeAudioMP3),
	}

	msg := CombineTextAndMedia("user", "Check these out:", mediaParts)

	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if len(msg.Parts) != 3 {
		t.Errorf("Parts length = %d, want 3", len(msg.Parts))
	}
	if msg.Parts[0].Type != ContentTypeText {
		t.Error("First part should be text")
	}
	if msg.Parts[1].Type != ContentTypeImage {
		t.Error("Second part should be image")
	}
	if msg.Parts[2].Type != ContentTypeAudio {
		t.Error("Third part should be audio")
	}
}

func TestCombineTextAndMediaEmptyText(t *testing.T) {
	mediaParts := []ContentPart{
		NewImagePartFromURL("https://example.com/img.jpg", nil),
	}

	msg := CombineTextAndMedia("user", "", mediaParts)

	if len(msg.Parts) != 1 {
		t.Errorf("Parts length = %d, want 1 (text should be skipped)", len(msg.Parts))
	}
	if msg.Parts[0].Type != ContentTypeImage {
		t.Error("First part should be image")
	}
}

func TestCountMediaParts(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want int
	}{
		{
			name: "legacy message",
			msg:  Message{Role: "user", Content: "Text"},
			want: 0,
		},
		{
			name: "text-only multimodal",
			msg: Message{
				Role:  "user",
				Parts: []ContentPart{NewTextPart("Text")},
			},
			want: 0,
		},
		{
			name: "one image",
			msg: Message{
				Role: "user",
				Parts: []ContentPart{
					NewTextPart("Text"),
					NewImagePartFromURL("https://example.com/img.jpg", nil),
				},
			},
			want: 1,
		},
		{
			name: "multiple media types",
			msg: Message{
				Role: "user",
				Parts: []ContentPart{
					NewImagePartFromURL("https://example.com/img.jpg", nil),
					NewAudioPartFromData("base64", MIMETypeAudioMP3),
					NewVideoPartFromData("base64", MIMETypeVideoMP4),
				},
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountMediaParts(tt.msg)
			if got != tt.want {
				t.Errorf("CountMediaParts() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCountPartsByType(t *testing.T) {
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			NewTextPart("First text"),
			NewImagePartFromURL("https://example.com/img1.jpg", nil),
			NewTextPart("Second text"),
			NewImagePartFromURL("https://example.com/img2.jpg", nil),
			NewAudioPartFromData("base64", MIMETypeAudioMP3),
		},
	}

	tests := []struct {
		contentType string
		want        int
	}{
		{ContentTypeText, 2},
		{ContentTypeImage, 2},
		{ContentTypeAudio, 1},
		{ContentTypeVideo, 0},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			got := CountPartsByType(msg, tt.contentType)
			if got != tt.want {
				t.Errorf("CountPartsByType(%q) = %d, want %d", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestCountPartsByTypeLegacy(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Legacy text",
	}

	if got := CountPartsByType(msg, ContentTypeText); got != 1 {
		t.Errorf("Legacy text message should count as 1 text part, got %d", got)
	}
	if got := CountPartsByType(msg, ContentTypeImage); got != 0 {
		t.Errorf("Legacy text message should have 0 images, got %d", got)
	}
}

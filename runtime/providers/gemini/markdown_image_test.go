package gemini

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestExtractMarkdownImages(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []types.ContentPart
	}{
		{
			name: "single markdown image with base64",
			text: "Here is an image:\n\n![Alt text](data:image/png;base64,iVBORw0KGgo=)\n\nDone.",
			expected: []types.ContentPart{
				types.NewTextPart("Here is an image:\n\n"),
				types.NewImagePartFromData("iVBORw0KGgo=", "image/png", nil),
				types.NewTextPart("\n\nDone."),
			},
		},
		{
			name: "multiple markdown images",
			text: "![img1](data:image/png;base64,ABC=) and ![img2](data:image/jpeg;base64,XYZ=)",
			expected: []types.ContentPart{
				types.NewImagePartFromData("ABC=", "image/png", nil),
				types.NewTextPart(" and "),
				types.NewImagePartFromData("XYZ=", "image/jpeg", nil),
			},
		},
		{
			name:     "no markdown images",
			text:     "Just plain text",
			expected: []types.ContentPart{types.NewTextPart("Just plain text")},
		},
		{
			name:     "empty text",
			text:     "",
			expected: []types.ContentPart{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMarkdownImages(tt.text)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d parts, got %d", len(tt.expected), len(result))
				return
			}

			for i := range result {
				if result[i].Type != tt.expected[i].Type {
					t.Errorf("part %d: expected type %s, got %s", i, tt.expected[i].Type, result[i].Type)
				}

				if result[i].Type == types.ContentTypeText {
					if *result[i].Text != *tt.expected[i].Text {
						t.Errorf("part %d: expected text %q, got %q", i, *tt.expected[i].Text, *result[i].Text)
					}
				} else if result[i].Type == types.ContentTypeImage {
					if *result[i].Media.Data != *tt.expected[i].Media.Data {
						t.Errorf("part %d: expected data %q, got %q", i, *tt.expected[i].Media.Data, *result[i].Media.Data)
					}
					if result[i].Media.MIMEType != tt.expected[i].Media.MIMEType {
						t.Errorf("part %d: expected mime %s, got %s", i, tt.expected[i].Media.MIMEType, result[i].Media.MIMEType)
					}
				}
			}
		})
	}
}

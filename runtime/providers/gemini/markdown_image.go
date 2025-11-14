package gemini

import (
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Regex to match markdown images with base64 data URIs
// Format: ![alt text](data:image/TYPE;base64,DATA)
var markdownImageRegex = regexp.MustCompile(`!\[[^\]]*\]\(data:image/([^;]+);base64,([^)]+)\)`)

// extractMarkdownImages parses text for markdown-style images with base64 data
// and returns a slice of ContentPart with text and image parts separated
func extractMarkdownImages(text string) []types.ContentPart {
	if text == "" {
		return []types.ContentPart{}
	}

	matches := markdownImageRegex.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		// No markdown images found, return as single text part
		return []types.ContentPart{types.NewTextPart(text)}
	}

	var parts []types.ContentPart
	lastEnd := 0

	for _, match := range matches {
		// match[0] = start of entire match
		// match[1] = end of entire match
		// match[2] = start of image type (e.g., "png")
		// match[3] = end of image type
		// match[4] = start of base64 data
		// match[5] = end of base64 data

		// Add text before this image (if any)
		if match[0] > lastEnd {
			textBefore := text[lastEnd:match[0]]
			parts = append(parts, types.NewTextPart(textBefore))
		}

		// Extract image type and data
		imageType := text[match[2]:match[3]]
		base64Data := text[match[4]:match[5]]
		mimeType := "image/" + imageType

		// Add image part
		parts = append(parts, types.NewImagePartFromData(base64Data, mimeType, nil))

		lastEnd = match[1]
	}

	// Add remaining text after last image (if any)
	if lastEnd < len(text) {
		textAfter := text[lastEnd:]
		parts = append(parts, types.NewTextPart(textAfter))
	}

	return parts
}

// processTextPartForImages checks if a text part contains markdown images
// and splits it into text + image + text parts as needed
func processTextPartForImages(textPart string) []types.ContentPart {
	// Check if there are any markdown images in this text
	if !strings.Contains(textPart, "data:image/") {
		return []types.ContentPart{types.NewTextPart(textPart)}
	}

	return extractMarkdownImages(textPart)
}

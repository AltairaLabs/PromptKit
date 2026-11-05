package a2a

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// InferContentType maps a MIME type to a PromptKit content type string: image,
// audio or video for the matching "image/", "audio/" or "video/" prefix, and
// document for everything else.
func InferContentType(mediaType string) string {
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return types.ContentTypeImage
	case strings.HasPrefix(mediaType, "audio/"):
		return types.ContentTypeAudio
	case strings.HasPrefix(mediaType, "video/"):
		return types.ContentTypeVideo
	case mediaType == "application/pdf",
		strings.HasPrefix(mediaType, "text/"):
		return types.ContentTypeDocument
	default:
		return types.ContentTypeDocument
	}
}

// PartToContentPart converts an A2A Part to a PromptKit ContentPart. A text part
// becomes a text content part. A Raw or URL part becomes a media content part
// and requires MediaType. A structured data part, or a part with none of text,
// raw, URL or data, returns an error.
func PartToContentPart(part *Part) (types.ContentPart, error) {
	switch {
	case part.Text != nil:
		return types.ContentPart{
			Type: types.ContentTypeText,
			Text: part.Text,
		}, nil

	case len(part.Raw) > 0 && part.MediaType != "":
		b64 := base64.StdEncoding.EncodeToString(part.Raw)
		return types.ContentPart{
			Type: InferContentType(part.MediaType),
			Media: &types.MediaContent{
				Data:     &b64,
				MIMEType: part.MediaType,
			},
		}, nil

	case part.URL != nil && part.MediaType != "":
		return types.ContentPart{
			Type: InferContentType(part.MediaType),
			Media: &types.MediaContent{
				URL:      part.URL,
				MIMEType: part.MediaType,
			},
		}, nil

	case part.dataValue() != nil:
		return types.ContentPart{}, fmt.Errorf("a2a: structured data parts are not supported")

	default:
		return types.ContentPart{}, fmt.Errorf("a2a: empty part (no text, raw, url, or data)")
	}
}

// MessageToMessage converts an A2A Message to a PromptKit Message. The "agent"
// role becomes "assistant"; any other role is copied unchanged. Metadata becomes
// Meta, and Content is set from the text parts. It returns an error naming the
// first part that fails to convert.
func MessageToMessage(msg *Message) (*types.Message, error) {
	role := string(msg.Role)
	if role == "agent" {
		role = "assistant"
	}

	out := &types.Message{
		Role: role,
		Meta: msg.Metadata,
	}

	for i, p := range msg.Parts {
		cp, err := PartToContentPart(&p)
		if err != nil {
			return nil, fmt.Errorf("a2a: converting part %d: %w", i, err)
		}
		out.Parts = append(out.Parts, cp)
	}

	// Set legacy Content field from text parts.
	out.Content = out.GetContent()

	return out, nil
}

// ContentPartToA2APart converts a PromptKit ContentPart to an A2A Part. A text
// part becomes a text part. A media part becomes a Raw part when it carries
// base64 data, otherwise a URL part when it carries a URL. Any other part
// returns an error.
func ContentPartToA2APart(part types.ContentPart) (Part, error) {
	if part.Type == types.ContentTypeText && part.Text != nil {
		return Part{Text: part.Text}, nil
	}

	if part.Media != nil {
		if part.Media.Data != nil && *part.Media.Data != "" {
			raw, err := base64.StdEncoding.DecodeString(*part.Media.Data)
			if err != nil {
				return Part{}, fmt.Errorf("a2a: decoding base64 data: %w", err)
			}
			return Part{
				Raw:       raw,
				MediaType: part.Media.MIMEType,
			}, nil
		}

		if part.Media.URL != nil && *part.Media.URL != "" {
			return Part{
				URL:       part.Media.URL,
				MediaType: part.Media.MIMEType,
			}, nil
		}
	}

	return Part{}, fmt.Errorf("a2a: content part has no text or media data")
}

// ContentPartsToArtifacts converts PromptKit ContentParts into A2A Artifacts.
// It returns a single Artifact, with ID "artifact-1", containing every part. It
// returns nil for an empty parts slice and an error if any part fails to
// convert.
func ContentPartsToArtifacts(parts []types.ContentPart) ([]Artifact, error) {
	if len(parts) == 0 {
		return nil, nil
	}

	var a2aParts []Part
	for i, p := range parts {
		ap, err := ContentPartToA2APart(p)
		if err != nil {
			return nil, fmt.Errorf("a2a: converting content part %d to artifact part: %w", i, err)
		}
		a2aParts = append(a2aParts, ap)
	}

	if len(a2aParts) == 0 {
		return nil, nil
	}

	return []Artifact{
		{
			ArtifactID: "artifact-1",
			Parts:      a2aParts,
		},
	}, nil
}

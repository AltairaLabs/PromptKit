package a2a

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
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

// ErrContentTypeNotSupported is wrapped by the errors PartToContentPart and
// MessageToMessage return for content whose media type cannot be determined.
// A server answers it with ContentTypeNotSupportedError
// (ErrCodeContentTypeNotSupported).
var ErrContentTypeNotSupported = errors.New("a2a: content type not supported")

// errDataPart is PartToContentPart's error for a structured data part, which
// has no media content part to become.
var errDataPart = errors.New("a2a: structured data parts are not supported")

// octetStream is what content sniffing reports for bytes of no known type.
const octetStream = "application/octet-stream"

// PartToContentPart converts an A2A Part to a PromptKit ContentPart. A text part
// becomes a text content part. A Raw or URL part becomes a media content part.
// Its media type is MediaType when set; A2A makes that field optional, so
// otherwise it is taken from Filename's extension, then from a data URL's
// header or the URL path's extension, or from sniffing Raw's bytes. A media
// part whose type none of these name returns an error wrapping
// ErrContentTypeNotSupported. A structured data part, or a part with none of
// text, raw, a non-empty URL or data, returns an error.
func PartToContentPart(part *Part) (types.ContentPart, error) {
	switch {
	case part.Text != nil:
		return types.ContentPart{
			Type: types.ContentTypeText,
			Text: part.Text,
		}, nil

	case len(part.Raw) > 0:
		mediaType := rawMediaType(part)
		if mediaType == "" {
			return types.ContentPart{}, fmt.Errorf(
				"%w: raw part has no mediaType and its content is not a recognized type", ErrContentTypeNotSupported)
		}
		b64 := base64.StdEncoding.EncodeToString(part.Raw)
		return types.ContentPart{
			Type: InferContentType(mediaType),
			Media: &types.MediaContent{
				Data:     &b64,
				MIMEType: mediaType,
			},
		}, nil

	case part.URL != nil && *part.URL != "":
		mediaType := urlMediaType(part)
		if mediaType == "" {
			return types.ContentPart{}, fmt.Errorf(
				"%w: url part has no mediaType and neither its filename nor its URL names a type",
				ErrContentTypeNotSupported)
		}
		return types.ContentPart{
			Type: InferContentType(mediaType),
			Media: &types.MediaContent{
				URL:      part.URL,
				MIMEType: mediaType,
			},
		}, nil

	case part.dataValue() != nil:
		return types.ContentPart{}, errDataPart

	default:
		return types.ContentPart{}, fmt.Errorf("a2a: empty part (no text, raw, url, or data)")
	}
}

// rawMediaType names a raw part's media type: MediaType, else Filename's
// extension, else the type its bytes sniff as. It is "" when none names one.
func rawMediaType(part *Part) string {
	if part.MediaType != "" {
		return part.MediaType
	}
	if t := extensionMediaType(part.Filename); t != "" {
		return t
	}
	if t := bareMediaType(http.DetectContentType(part.Raw)); t != octetStream {
		return t
	}
	return ""
}

// urlMediaType names a URL part's media type: MediaType, else Filename's
// extension, else a data URL's declared type or the URL path's extension. It
// is "" when none names one.
func urlMediaType(part *Part) string {
	if part.MediaType != "" {
		return part.MediaType
	}
	if t := extensionMediaType(part.Filename); t != "" {
		return t
	}
	if rest, isData := strings.CutPrefix(*part.URL, "data:"); isData {
		header, _, _ := strings.Cut(rest, ",")
		declared, _, _ := strings.Cut(header, ";")
		return declared
	}
	u, err := url.Parse(*part.URL)
	if err != nil {
		return ""
	}
	return extensionMediaType(u.Path)
}

// extensionMediaType is the media type name's extension stands for, without
// parameters, or "" for an unknown or missing extension. The lookup ignores
// case.
func extensionMediaType(name string) string {
	return bareMediaType(mime.TypeByExtension(path.Ext(name)))
}

// bareMediaType drops a media type's parameters ("; charset=utf-8").
func bareMediaType(mediaType string) string {
	bare, _, _ := strings.Cut(mediaType, ";")
	return strings.TrimSpace(bare)
}

// dataPartToContentPart renders a data part as a text content part holding
// the value's JSON, which is how a conversation can read structured input.
func dataPartToContentPart(part *Part) (types.ContentPart, error) {
	encoded, err := json.Marshal(part.dataValue())
	if err != nil {
		return types.ContentPart{}, fmt.Errorf("a2a: encoding data part: %w", err)
	}
	text := string(encoded)
	return types.ContentPart{Type: types.ContentTypeText, Text: &text}, nil
}

// MessageToMessage converts an A2A Message to a PromptKit Message. The "agent"
// role becomes "assistant"; any other role is copied unchanged. Metadata becomes
// Meta. Parts are converted with PartToContentPart, except that a data part
// becomes a text part holding the value's JSON. Content is set from the text
// parts. It returns an error naming the first part that fails to convert;
// content of a type that cannot be determined wraps ErrContentTypeNotSupported.
func MessageToMessage(msg *Message) (*types.Message, error) {
	role := string(msg.Role)
	if role == "agent" {
		role = "assistant"
	}

	out := &types.Message{
		Role: role,
		Meta: msg.Metadata,
	}

	for i := range msg.Parts {
		p := &msg.Parts[i]
		cp, err := PartToContentPart(p)
		if errors.Is(err, errDataPart) {
			cp, err = dataPartToContentPart(p)
		}
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

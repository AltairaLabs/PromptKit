package gemini

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The adapter forwards any MIME type as inlineData, so the declaration must
// admit every type the Gemini API documents, including those PromptKit
// infers from common file extensions (.mp3 -> audio/mpeg, .weba -> audio/webm,
// .docx, .txt, .md, .csv, .json, .xml). Each of these worked before request
// validation existed; rejecting one would break a working request.
func TestGeminiDeclaresEveryDocumentedFormat(t *testing.T) {
	p := NewProvider("test", "gemini-2.5-flash", "https://example.invalid", providers.ProviderDefaults{}, false)
	accepted := map[string][]string{
		types.ContentTypeAudio: {
			types.MIMETypeAudioWAV, "audio/mp3", types.MIMETypeAudioMP3, "audio/aiff", "audio/aac",
			types.MIMETypeAudioOgg, "audio/flac", "audio/m4a", "audio/l16", "audio/opus",
			"audio/alaw", "audio/mulaw", types.MIMETypeAudioWebM,
		},
		types.ContentTypeVideo: {
			types.MIMETypeVideoMP4, "video/mpeg", "video/mov", "video/avi", "video/x-flv", "video/flv",
			"video/mpg", types.MIMETypeVideoWebM, "video/wmv", "video/3gpp",
		},
		types.ContentTypeDocument: {
			types.MIMETypePDF, types.MIMETypeDocx, types.MIMETypeDoc, types.MIMETypePlainText,
			types.MIMETypeMarkdown, types.MIMETypeCSV, "text/html", types.MIMETypeJSON, types.MIMETypeXML,
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		},
	}
	for contentType, mimes := range accepted {
		for _, mime := range mimes {
			if !providers.IsFormatSupported(p, contentType, mime) {
				t.Errorf("%s %q is accepted by the Gemini API but not declared", contentType, mime)
			}
		}
	}
	if providers.IsFormatSupported(p, types.ContentTypeImage, "image/bmp") {
		t.Error("image/bmp is not a documented Gemini image type")
	}
}

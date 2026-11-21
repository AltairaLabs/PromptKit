package a2a

import (
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A2A 1.0 makes mediaType optional: a raw or url part without it takes its
// type from the filename, the URL or the bytes, and is refused with
// ErrContentTypeNotSupported only when none of them names one.
func TestPartToContentPart_InfersMissingMediaType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	tests := []struct {
		name     string
		part     Part
		wantType string
		wantMIME string
	}{
		{"raw keeps its mediaType", Part{Raw: png, MediaType: "image/x-custom"}, types.ContentTypeImage, "image/x-custom"},
		{"raw from filename before bytes", Part{Raw: png, Filename: "scan.PDF"}, types.ContentTypeDocument, "application/pdf"},
		{"raw sniffed", Part{Raw: png}, types.ContentTypeImage, "image/png"},
		{"raw WAV sniffed as audio/wav", Part{Raw: []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00")},
			types.ContentTypeAudio, types.MIMETypeAudioWAV},
		{"raw M4A sniffed as audio", Part{Raw: []byte("\x00\x00\x00\x1cftypM4A \x00\x00\x00\x00M4A mp42isom")},
			types.ContentTypeAudio, "audio/m4a"},
		{"raw MP4 sniffed as video", Part{Raw: []byte("\x00\x00\x00\x1cftypisom\x00\x00\x02\x00isomiso2mp41")},
			types.ContentTypeVideo, types.MIMETypeVideoMP4},
		{"data URL scheme in capitals", Part{URL: testutil.Ptr("Data:audio/x-wav;base64,UklGRg==")},
			types.ContentTypeAudio, types.MIMETypeAudioWAV},
		{"raw text sniffed without charset", Part{Raw: []byte("notes")}, types.ContentTypeDocument, "text/plain"},
		{"url keeps its mediaType", Part{URL: testutil.Ptr("https://e.com/a.png"), MediaType: "audio/wav"},
			types.ContentTypeAudio, "audio/wav"},
		{"url from filename before path", Part{URL: testutil.Ptr("https://e.com/a.png"), Filename: "a.pdf"},
			types.ContentTypeDocument, "application/pdf"},
		{"url from data URL header", Part{URL: testutil.Ptr("data:image/gif;base64,R0lG")},
			types.ContentTypeImage, "image/gif"},
		{"url from path extension", Part{URL: testutil.Ptr("https://e.com/x/a.webp?v=2")},
			types.ContentTypeImage, "image/webp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PartToContentPart(&tt.part)
			if err != nil {
				t.Fatalf("PartToContentPart() error = %v", err)
			}
			if got.Type != tt.wantType || got.Media == nil || got.Media.MIMEType != tt.wantMIME {
				t.Fatalf("got type %q media %+v, want type %q mime %q", got.Type, got.Media, tt.wantType, tt.wantMIME)
			}
		})
	}
}

func TestPartToContentPart_RefusesContentOfNoKnownType(t *testing.T) {
	tests := []struct {
		name    string
		part    Part
		wantMsg string
	}{
		{"raw of no known type", Part{Raw: []byte{0, 1, 2, 3}},
			"a2a: content type not supported: raw part has no mediaType and its content is not a recognized type"},
		{"url naming no type", Part{URL: testutil.Ptr("https://e.com/download?id=1")},
			"a2a: content type not supported: url part has no mediaType and neither its filename nor its URL names a type"},
		{"declared octet-stream", Part{Raw: []byte("x"), MediaType: "application/octet-stream"},
			"a2a: content type not supported: application/octet-stream names no type of content"},
		{"undeclared text that is not UTF-8", Part{Raw: []byte("\xff\xfeh\x00i\x00")},
			"a2a: content type not supported: raw part has no mediaType and its text is not UTF-8"},
		{"unparseable url", Part{URL: testutil.Ptr("https://e.com/%zz.png")},
			"a2a: content type not supported: url part has no mediaType and neither its filename nor its URL names a type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PartToContentPart(&tt.part)
			if !errors.Is(err, ErrContentTypeNotSupported) || err.Error() != tt.wantMsg {
				t.Fatalf("error = %v, want %q wrapping ErrContentTypeNotSupported", err, tt.wantMsg)
			}
		})
	}
}

func TestPartToContentPart_DataURLWithoutCommaIsMalformed(t *testing.T) {
	for _, u := range []string{"data:", "data:image/png;base64"} {
		_, err := PartToContentPart(&Part{URL: testutil.Ptr(u)})
		if err == nil || err.Error() != "a2a: url part is a malformed data URL: it has no comma" ||
			errors.Is(err, ErrContentTypeNotSupported) {
			t.Fatalf("%q: error = %v, want the malformed data URL error", u, err)
		}
	}
}

func TestPartToContentPart_EmptyURLIsAnEmptyPart(t *testing.T) {
	_, err := PartToContentPart(&Part{URL: testutil.Ptr(""), MediaType: "image/png"})
	if err == nil || err.Error() != "a2a: empty part (no text, raw, url, or data)" || errors.Is(err, ErrContentTypeNotSupported) {
		t.Fatalf("error = %v, want the empty-part error", err)
	}
}

func TestMessageToMessage_DataPartBecomesJSONText(t *testing.T) {
	got, err := MessageToMessage(&Message{
		Role: RoleUser,
		Parts: []Part{
			{Text: testutil.Ptr("order:")},
			{Data: map[string]any{"id": 7}},
			{DataValue: []any{true, nil}},
		},
	})
	if err != nil {
		t.Fatalf("MessageToMessage() error = %v", err)
	}
	var texts []string
	for _, p := range got.Parts {
		if p.Type != types.ContentTypeText || p.Text == nil {
			t.Fatalf("part %+v is not a text part", p)
		}
		texts = append(texts, *p.Text)
	}
	if len(texts) != 3 || texts[0] != "order:" || texts[1] != `{"id":7}` || texts[2] != "[true,null]" {
		t.Fatalf("texts = %q", texts)
	}
}

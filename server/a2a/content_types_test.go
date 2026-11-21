package a2aserver

import (
	"context"
	"encoding/base64"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// File headers: enough for content sniffing.
var (
	pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	wavHeader = []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00")
	m4aHeader = []byte("\x00\x00\x00\x1cftypM4A \x00\x00\x00\x00M4A mp42isom")
	mp4Header = []byte("\x00\x00\x00\x1cftypisom\x00\x00\x02\x00isomiso2mp41")
)

func strPtr(s string) *string { return &s }

// recordingConv records the message each turn receives.
type recordingConv struct {
	mu  sync.Mutex
	got *types.Message
}

func (c *recordingConv) Send(_ context.Context, message any) (SendResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got, _ = message.(*types.Message)
	return &mockSendResult{parts: []types.ContentPart{types.NewTextPart("ok")}, text: "ok"}, nil
}

func (c *recordingConv) Close() error { return nil }

func (c *recordingConv) received() *types.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got
}

// A2A 1.0 makes mediaType optional on every part and lets a data part carry
// any JSON value. Each part must reach the conversation, with its media type
// taken from the filename, the URL or the bytes when the sender left it out.
func TestServer_SendMessage_PartContentReachesConversation(t *testing.T) {
	jsonBytes := []byte(`{"a":1}`)
	tests := []struct {
		name        string
		part        a2a.Part
		want        types.ContentPart
		wantContent string
	}{
		{
			name: "raw without mediaType is sniffed",
			part: a2a.Part{Raw: pngHeader},
			want: types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString(pngHeader)), MIMEType: "image/png",
			}},
		},
		{
			// http.DetectContentType says audio/wave; providers list audio/wav.
			name: "raw WAV without mediaType gets the name providers list",
			part: a2a.Part{Raw: wavHeader},
			want: types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString(wavHeader)), MIMEType: "audio/wav",
			}},
		},
		{
			// An M4A file sniffs as video/mp4; its ftyp brand says it is audio.
			name: "raw M4A without mediaType is audio",
			part: a2a.Part{Raw: m4aHeader},
			want: types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString(m4aHeader)), MIMEType: "audio/m4a",
			}},
		},
		{
			name: "raw MP4 video without mediaType stays video",
			part: a2a.Part{Raw: mp4Header},
			want: types.ContentPart{Type: types.ContentTypeVideo, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString(mp4Header)), MIMEType: "video/mp4",
			}},
		},
		{
			name: "declared alias gets the name providers list",
			part: a2a.Part{Raw: []byte{0, 1, 2}, MediaType: " Audio/X-WAV; rate=16000"},
			want: types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString([]byte{0, 1, 2})), MIMEType: "audio/wav",
			}},
		},
		{
			name: "filename extension is matched without regard to case",
			part: a2a.Part{Raw: []byte{0, 1, 2}, Filename: "Take 1.WAV"},
			want: types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString([]byte{0, 1, 2})), MIMEType: "audio/wav",
			}},
		},
		{
			name: "url .mov is video/mov, the name providers list",
			part: a2a.Part{URL: strPtr("https://example.com/clip.mov")},
			want: types.ContentPart{Type: types.ContentTypeVideo, Media: &types.MediaContent{
				URL: strPtr("https://example.com/clip.mov"), MIMEType: "video/mov",
			}},
		},
		{
			name: "data URL scheme and type are case-insensitive",
			part: a2a.Part{URL: strPtr("DATA:Image/PNG ;base64,iVBORw==")},
			want: types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{
				URL: strPtr("DATA:Image/PNG ;base64,iVBORw=="), MIMEType: "image/png",
			}},
		},
		{
			name: "data URL without a type is text/plain (RFC 2397)",
			part: a2a.Part{URL: strPtr("data:;base64,aGk=")},
			want: types.ContentPart{Type: types.ContentTypeDocument, Media: &types.MediaContent{
				URL: strPtr("data:;base64,aGk="), MIMEType: "text/plain",
			}},
		},
		{
			name: "raw text without mediaType is sniffed, without parameters",
			part: a2a.Part{Raw: []byte("plain notes")},
			want: types.ContentPart{Type: types.ContentTypeDocument, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString([]byte("plain notes"))), MIMEType: "text/plain",
			}},
		},
		{
			name: "raw without mediaType takes the filename's type first",
			part: a2a.Part{Raw: jsonBytes, Filename: "data.json"},
			want: types.ContentPart{Type: types.ContentTypeDocument, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString(jsonBytes)), MIMEType: "application/json",
			}},
		},
		{
			name: "raw with mediaType keeps it",
			part: a2a.Part{Raw: pngHeader, MediaType: "image/x-custom"},
			want: types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString(pngHeader)), MIMEType: "image/x-custom",
			}},
		},
		{
			name: "url without mediaType takes the path's extension",
			part: a2a.Part{URL: strPtr("https://example.com/pics/cat.JPG?size=large")},
			want: types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{
				URL: strPtr("https://example.com/pics/cat.JPG?size=large"), MIMEType: "image/jpeg",
			}},
		},
		{
			name: "url without mediaType takes the filename's type first",
			part: a2a.Part{URL: strPtr("https://example.com/download?id=7"), Filename: "scan.pdf"},
			want: types.ContentPart{Type: types.ContentTypeDocument, Media: &types.MediaContent{
				URL: strPtr("https://example.com/download?id=7"), MIMEType: "application/pdf",
			}},
		},
		{
			name: "url with mediaType keeps it",
			part: a2a.Part{URL: strPtr("https://example.com/clip.png"), MediaType: "video/mp4"},
			want: types.ContentPart{Type: types.ContentTypeVideo, Media: &types.MediaContent{
				URL: strPtr("https://example.com/clip.png"), MIMEType: "video/mp4",
			}},
		},
		{
			name: "data URL without mediaType takes the data URL's type",
			part: a2a.Part{URL: strPtr("data:image/webp;base64,UklGRg==")},
			want: types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{
				URL: strPtr("data:image/webp;base64,UklGRg=="), MIMEType: "image/webp",
			}},
		},
		{
			name:        "data object becomes its JSON text",
			part:        a2a.Part{Data: map[string]any{"order": 42, "sku": "A-1"}},
			want:        types.ContentPart{Type: types.ContentTypeText, Text: strPtr(`{"order":42,"sku":"A-1"}`)},
			wantContent: `{"order":42,"sku":"A-1"}`,
		},
		{
			name:        "data value that is not an object becomes its JSON text",
			part:        a2a.Part{DataValue: []any{1, "two"}},
			want:        types.ContentPart{Type: types.ContentTypeText, Text: strPtr(`[1,"two"]`)},
			wantContent: `[1,"two"]`,
		},
	}

	for _, method := range []string{a2a.MethodV1SendMessage, a2a.MethodV1SendStreamingMessage} {
		for _, tt := range tests {
			t.Run(method+"/"+tt.name, func(t *testing.T) {
				conv := &recordingConv{}
				_, ts := newTestServer(func(string) (Conversation, error) { return conv, nil })
				defer ts.Close()

				params := a2a.SendMessageRequest{
					Message: a2a.Message{
						MessageID: "m1",
						ContextID: "ctx-parts",
						Role:      a2a.RoleUser,
						Parts:     []a2a.Part{tt.part},
					},
					Configuration: &a2a.SendMessageConfiguration{Blocking: true},
				}
				if method == a2a.MethodV1SendStreamingMessage {
					events := readSSEEvents(t, ts, method, params)
					if len(events) == 0 {
						t.Fatal("stream sent no events")
					}
				} else {
					task := a2aRPCRequestTask(t, ts, method, params)
					if task.Status.State != a2a.TaskStateCompleted {
						t.Fatalf("state = %q, want completed", task.Status.State)
					}
				}

				got := conv.received()
				if got == nil {
					t.Fatal("conversation received no message")
				}
				if !reflect.DeepEqual(got.Parts, []types.ContentPart{tt.want}) {
					t.Fatalf("parts = %s, want %s", describeParts(got.Parts), describeParts([]types.ContentPart{tt.want}))
				}
				if got.Content != tt.wantContent {
					t.Fatalf("content = %q, want %q", got.Content, tt.wantContent)
				}
			})
		}
	}
}

// Content the server cannot type is ContentTypeNotSupportedError (-32005);
// a part that carries nothing is a malformed request (-32602). Either way no
// conversation is opened for the rejected message.
func TestServer_SendMessage_RejectsContentItCannotType(t *testing.T) {
	tests := []struct {
		name     string
		part     a2a.Part
		wantCode int
		wantMsg  string
	}{
		{
			name:     "raw bytes of no recognizable type",
			part:     a2a.Part{Raw: []byte{0x00, 0x01, 0x02, 0x03}},
			wantCode: a2a.ErrCodeContentTypeNotSupported,
			wantMsg: "Invalid message: a2a: converting part 0: a2a: content type not supported: " +
				"raw part has no mediaType and its content is not a recognized type",
		},
		{
			name:     "url naming no type",
			part:     a2a.Part{URL: strPtr("https://example.com/download?id=7")},
			wantCode: a2a.ErrCodeContentTypeNotSupported,
			wantMsg: "Invalid message: a2a: converting part 0: a2a: content type not supported: " +
				"url part has no mediaType and neither its filename nor its URL names a type",
		},
		{
			name:     "declared application/octet-stream",
			part:     a2a.Part{Raw: pngHeader, MediaType: "application/octet-stream"},
			wantCode: a2a.ErrCodeContentTypeNotSupported,
			wantMsg: "Invalid message: a2a: converting part 0: a2a: content type not supported: " +
				"application/octet-stream names no type of content",
		},
		{
			name:     "url declared application/octet-stream",
			part:     a2a.Part{URL: strPtr("https://example.com/a.png"), MediaType: "Application/Octet-Stream"},
			wantCode: a2a.ErrCodeContentTypeNotSupported,
			wantMsg: "Invalid message: a2a: converting part 0: a2a: content type not supported: " +
				"application/octet-stream names no type of content",
		},
		{
			name:     "data URL declaring application/octet-stream",
			part:     a2a.Part{URL: strPtr("data:application/octet-stream;base64,AAEC")},
			wantCode: a2a.ErrCodeContentTypeNotSupported,
			wantMsg: "Invalid message: a2a: converting part 0: a2a: content type not supported: " +
				"application/octet-stream names no type of content",
		},
		{
			name:     "raw with a filename extension of no known type",
			part:     a2a.Part{Raw: []byte{0x00, 0x01, 0x02, 0x03}, Filename: "setup.exe"},
			wantCode: a2a.ErrCodeContentTypeNotSupported,
			wantMsg: "Invalid message: a2a: converting part 0: a2a: content type not supported: " +
				"raw part has no mediaType and its content is not a recognized type",
		},
		{
			name:     "url with an extension of no known type",
			part:     a2a.Part{URL: strPtr("https://example.com/firmware.bin")},
			wantCode: a2a.ErrCodeContentTypeNotSupported,
			wantMsg: "Invalid message: a2a: converting part 0: a2a: content type not supported: " +
				"url part has no mediaType and neither its filename nor its URL names a type",
		},
		{
			name:     "raw UTF-16 text without mediaType",
			part:     a2a.Part{Raw: []byte("\xff\xfeh\x00i\x00")},
			wantCode: a2a.ErrCodeContentTypeNotSupported,
			wantMsg: "Invalid message: a2a: converting part 0: a2a: content type not supported: " +
				"raw part has no mediaType and its text is not UTF-8",
		},
		{
			name:     "raw Latin-1 text named by its filename",
			part:     a2a.Part{Raw: []byte("caf\xe9"), Filename: "menu.txt"},
			wantCode: a2a.ErrCodeContentTypeNotSupported,
			wantMsg: "Invalid message: a2a: converting part 0: a2a: content type not supported: " +
				"raw part has no mediaType and its text is not UTF-8",
		},
		{
			name:     "data URL without a comma",
			part:     a2a.Part{URL: strPtr("data:image/png;base64")},
			wantCode: a2a.ErrCodeInvalidParams,
			wantMsg:  "Invalid message: a2a: converting part 0: a2a: url part is a malformed data URL: it has no comma",
		},
		{
			name:     "empty url",
			part:     a2a.Part{URL: strPtr(""), MediaType: "image/png"},
			wantCode: a2a.ErrCodeInvalidParams,
			wantMsg:  "Invalid message: a2a: converting part 0: a2a: empty part (no text, raw, url, or data)",
		},
		{
			name:     "part with no content",
			part:     a2a.Part{},
			wantCode: a2a.ErrCodeInvalidParams,
			wantMsg:  "Invalid message: a2a: converting part 0: a2a: empty part (no text, raw, url, or data)",
		},
	}

	for _, method := range []string{a2a.MethodV1SendMessage, a2a.MethodV1SendStreamingMessage} {
		for _, tt := range tests {
			t.Run(method+"/"+tt.name, func(t *testing.T) {
				var opened atomic.Int32
				_, ts := newTestServer(func(string) (Conversation, error) {
					opened.Add(1)
					return completingMock(), nil
				})
				defer ts.Close()

				resp := a2aRPCRequest(t, ts, method, a2a.SendMessageRequest{
					Message: a2a.Message{
						MessageID: "m1",
						ContextID: "ctx-reject",
						Role:      a2a.RoleUser,
						Parts:     []a2a.Part{tt.part},
					},
					Configuration: &a2a.SendMessageConfiguration{Blocking: true},
				})
				if resp.Error == nil {
					t.Fatalf("expected error, got result %s", resp.Result)
				}
				if resp.Error.Code != tt.wantCode || resp.Error.Message != tt.wantMsg {
					t.Fatalf("error = %d %q, want %d %q", resp.Error.Code, resp.Error.Message, tt.wantCode, tt.wantMsg)
				}
				if n := opened.Load(); n != 0 {
					t.Fatalf("conversation opened %d times for a rejected message, want 0", n)
				}
			})
		}
	}
}

// A message mixing text and data parts reaches the conversation with every
// part in order, and its Content is the text parts' text.
func TestServer_SendMessage_TextAndDataPartsReachConversation(t *testing.T) {
	conv := &recordingConv{}
	_, ts := newTestServer(func(string) (Conversation, error) { return conv, nil })
	defer ts.Close()

	task := a2aSendMessageWithParts(t, ts, "ctx-mixed", []a2a.Part{
		{Text: strPtr("Ship this order: ")},
		{Data: map[string]any{"id": 7, "items": []any{"A-1"}}},
		{Text: strPtr(" today.")},
	})
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}

	got := conv.received()
	want := []types.ContentPart{
		{Type: types.ContentTypeText, Text: strPtr("Ship this order: ")},
		{Type: types.ContentTypeText, Text: strPtr(`{"id":7,"items":["A-1"]}`)},
		{Type: types.ContentTypeText, Text: strPtr(" today.")},
	}
	if got == nil || !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("parts = %+v, want %s", got, describeParts(want))
	}
	if wantContent := `Ship this order: {"id":7,"items":["A-1"]} today.`; got.Content != wantContent {
		t.Fatalf("content = %q, want %q", got.Content, wantContent)
	}
}

func describeParts(parts []types.ContentPart) string {
	out := "["
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p.Type
		if p.Text != nil {
			out += " text=" + *p.Text
		}
		if p.Media != nil {
			out += " mime=" + p.Media.MIMEType
			if p.Media.URL != nil {
				out += " url=" + *p.Media.URL
			}
			if p.Media.Data != nil {
				out += " data=" + *p.Media.Data
			}
		}
	}
	return out + "]"
}

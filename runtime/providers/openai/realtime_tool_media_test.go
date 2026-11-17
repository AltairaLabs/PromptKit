package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// openProviderToolSession opens a realtime session the way a host does, via
// the provider's CreateStreamSession (an Azure-hosted provider, so the socket
// goes to the capture server), and returns what CreateStreamSession returned.
func openProviderToolSession(t *testing.T, received chan map[string]any) providers.StreamInputSession {
	t.Helper()
	srv := newToolCaptureServer(t, received)
	t.Cleanup(srv.Close)
	p := NewProviderFromConfig(&ProviderConfig{
		ID: "rt", Model: "gpt-realtime", Credential: &tokenCredential{}, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL + "/"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	session, err := p.CreateStreamSession(ctx, audioStreamRequest())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// sendToolRound sends responses the way the duplex stage does: with their
// parts only when the session reports it can send them.
func sendToolRound(t *testing.T, session providers.StreamInputSession, responses []providers.ToolResponse) {
	t.Helper()
	ps, ok := session.(providers.ToolResponsePartsSupport)
	require.True(t, ok, "the realtime session does not report ToolResponsePartsSupport")
	require.True(t, ps.SupportsToolResponseParts())
	trs, ok := session.(providers.ToolResponseSupport)
	require.True(t, ok)
	require.NoError(t, trs.SendToolResponses(context.Background(), responses))
}

// recvEvents returns the next n client events, without their event_id.
func recvEvents(t *testing.T, received <-chan map[string]any, n int) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		ev := recvEvent(t, received)
		delete(ev, "event_id")
		out = append(out, ev)
	}
	return out
}

func functionOutputEvent(callID, output string) map[string]any {
	return map[string]any{"type": "conversation.item.create", "item": map[string]any{
		"type": "function_call_output", "call_id": callID, "output": output}}
}

func userMediaEvent(content ...map[string]any) map[string]any {
	items := make([]any, len(content))
	for i := range content {
		items[i] = content[i]
	}
	return map[string]any{"type": "conversation.item.create", "item": map[string]any{
		"type": "message", "role": "user", "content": items}}
}

var responseCreateEvent = map[string]any{"type": "response.create"}

// pcm16WAV is a canonical 44-byte-header WAV of 16-bit samples.
func pcm16WAV(t *testing.T, rate, channels int, pcm []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := func(v any) { require.NoError(t, binary.Write(&b, binary.LittleEndian, v)) }
	b.WriteString("RIFF")
	w(uint32(36 + len(pcm)))
	b.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1))
	w(uint16(channels))
	w(uint32(rate))
	w(uint32(rate * channels * 2))
	w(uint16(channels * 2))
	w(uint16(16))
	b.WriteString("data")
	w(uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}

func testPCM() []byte {
	pcm := make([]byte, 64)
	for i := range pcm {
		pcm[i] = byte(i * 7)
	}
	return pcm
}

// A tool that returns only an image: the function_call_output says where the
// image went (never an empty string), and the image follows in a user
// message as an input_image data URI, before the response.create.
func TestRealtimeToolResponse_ImageOnlyIsSentInAUserMessage(t *testing.T) {
	received := make(chan map[string]any, 16)
	session := openProviderToolSession(t, received)

	sendToolRound(t, session, []providers.ToolResponse{{
		ToolCallID: "call_img",
		Parts:      []types.ContentPart{types.NewImagePartFromData("iVBORw0KGgo=", types.MIMETypeImagePNG, nil)},
	}})

	assert.Equal(t, []map[string]any{
		functionOutputEvent("call_img", "[The image this tool returned is attached in the next user message.]"),
		userMediaEvent(
			map[string]any{"type": "input_text", "text": "Media returned by tool call call_img:"},
			map[string]any{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo="},
		),
		responseCreateEvent,
	}, recvEvents(t, received, 3))
}

// Text and an image: the text stays in the function_call_output, followed by
// the note; every output precedes the media messages, and one response.create
// ends the round.
func TestRealtimeToolResponse_TextAndImageKeepTheText(t *testing.T) {
	received := make(chan map[string]any, 16)
	session := openProviderToolSession(t, received)

	sendToolRound(t, session, []providers.ToolResponse{
		{
			ToolCallID: "call_chart",
			Result:     "Chart of Q3 sales",
			Parts: []types.ContentPart{
				types.NewTextPart("Chart of Q3 sales"),
				types.NewImagePartFromData("/9j/4AAQ", types.MIMETypeImageJPEG, nil),
			},
		},
		{ToolCallID: "call_text", Result: "plain"},
	})

	assert.Equal(t, []map[string]any{
		functionOutputEvent("call_chart",
			"Chart of Q3 sales\n[The image this tool returned is attached in the next user message.]"),
		functionOutputEvent("call_text", "plain"),
		userMediaEvent(
			map[string]any{"type": "input_text", "text": "Media returned by tool call call_chart:"},
			map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64,/9j/4AAQ"},
		),
		responseCreateEvent,
	}, recvEvents(t, received, 4))
}

// streamedWAV is a 24 kHz mono WAV as a streaming encoder writes it: RIFF
// and data sizes unknown (0xFFFFFFFF), and an odd-sized LIST chunk, padded to
// a word, before the data.
func streamedWAV(t *testing.T, pcm []byte) []byte {
	t.Helper()
	wav := pcm16WAV(t, 24000, 1, nil)
	header := append([]byte(nil), wav[:36]...)
	binary.LittleEndian.PutUint32(header[4:], 0xFFFFFFFF)
	header = append(header, []byte("LIST\x03\x00\x00\x00abc\x00")...)
	header = append(header, []byte("data\xff\xff\xff\xff")...)
	return append(header, pcm...)
}

// resampledTo24k is pcm at rate resampled to the session's 24 kHz.
func resampledTo24k(t *testing.T, pcm []byte, rate int) []byte {
	t.Helper()
	out, err := audio.ResamplePCM16(pcm, rate, DefaultRealtimeSampleRate)
	require.NoError(t, err)
	return out
}

// Audio is sent as input_audio in the session's input format, 16-bit PCM at
// 24 kHz: a 24 kHz WAV's samples as they are, and a WAV at another rate
// resampled.
func TestRealtimeToolResponse_AudioIsSentAsSessionPCM(t *testing.T) {
	pcm := testPCM()
	resampled := resampledTo24k(t, pcm, 16000)
	cases := []struct {
		name string
		part types.ContentPart
		want []byte
	}{
		{"24kHz WAV", types.NewAudioPartFromData(
			base64.StdEncoding.EncodeToString(pcm16WAV(t, 24000, 1, pcm)), types.MIMETypeAudioWAV), pcm},
		{"16kHz WAV", types.NewAudioPartFromData(
			base64.StdEncoding.EncodeToString(pcm16WAV(t, 16000, 1, pcm)), types.MIMETypeAudioWAV), resampled},
		{"streamed WAV", types.NewAudioPartFromData(
			base64.StdEncoding.EncodeToString(streamedWAV(t, pcm)), types.MIMETypeAudioWAV), pcm},
		{"raw PCM with its rate", types.NewAudioPartFromData(
			base64.StdEncoding.EncodeToString(pcm), "audio/pcm;rate=16000"), resampled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan map[string]any, 16)
			session := openProviderToolSession(t, received)

			sendToolRound(t, session, []providers.ToolResponse{{
				ToolCallID: "call_audio", Result: "Recording", Parts: []types.ContentPart{types.NewTextPart("Recording"), tc.part},
			}})

			assert.Equal(t, []map[string]any{
				functionOutputEvent("call_audio",
					"Recording\n[The audio this tool returned is attached in the next user message.]"),
				userMediaEvent(
					map[string]any{"type": "input_text", "text": "Media returned by tool call call_audio:"},
					map[string]any{"type": "input_audio", "audio": base64.StdEncoding.EncodeToString(tc.want)},
				),
				responseCreateEvent,
			}, recvEvents(t, received, 3))
		})
	}
}

// A result with several media parts sends them all in one user message, in
// order, and its output counts them.
func TestRealtimeToolResponse_SeveralMediaPartsShareOneMessage(t *testing.T) {
	received := make(chan map[string]any, 16)
	session := openProviderToolSession(t, received)
	pcm := testPCM()

	sendToolRound(t, session, []providers.ToolResponse{{
		ToolCallID: "call_two",
		Parts: []types.ContentPart{
			types.NewImagePartFromData("iVBORw0KGgo=", types.MIMETypeImagePNG, nil),
			types.NewAudioPartFromData(base64.StdEncoding.EncodeToString(pcm), "audio/pcm;rate=24000"),
		},
	}})

	assert.Equal(t, []map[string]any{
		functionOutputEvent("call_two", "[The 2 media parts this tool returned are attached in the next user message.]"),
		userMediaEvent(
			map[string]any{"type": "input_text", "text": "Media returned by tool call call_two:"},
			map[string]any{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo="},
			map[string]any{"type": "input_audio", "audio": base64.StdEncoding.EncodeToString(pcm)},
		),
		responseCreateEvent,
	}, recvEvents(t, received, 3))
}

// Media Realtime cannot take is described in the function_call_output, in the
// words the text providers use, and no user message is sent for it.
func TestRealtimeToolResponse_UnsendableMediaIsDescribed(t *testing.T) {
	received := make(chan map[string]any, 16)
	session := openProviderToolSession(t, received)

	video := types.ContentPart{Type: types.ContentTypeVideo, Media: &types.MediaContent{
		Data: strPtr("AAAA"), MIMEType: "video/mp4"}}
	sendToolRound(t, session, []providers.ToolResponse{{
		ToolCallID: "call_many",
		Parts: []types.ContentPart{
			video,
			types.NewImagePartFromData("R0lGOD", types.MIMETypeImageGIF, nil),
			types.NewAudioPartFromData("SUQz", types.MIMETypeAudioMP3),
			types.NewAudioPartFromData(base64.StdEncoding.EncodeToString(pcm16WAV(t, 24000, 2, testPCM())),
				types.MIMETypeAudioWAV),
		},
	}})

	assert.Equal(t, []map[string]any{
		functionOutputEvent("call_many", "[This tool returned a video (video/mp4) that was not sent: "+
			"OpenAI Realtime does not accept video input.]\n"+
			"[This tool returned an image (image/gif) that was not sent: "+
			"OpenAI Realtime does not accept image/gif images.]\n"+
			"[This tool returned an audio (audio/mpeg) that was not sent: "+
			"OpenAI Realtime takes tool audio only as 16-bit PCM WAV or audio/pcm;rate=N.]\n"+
			"[This tool returned an audio (audio/wav) that was not sent: "+
			"OpenAI Realtime takes tool audio only as mono 16-bit PCM; this WAV has 2 channels.]"),
		responseCreateEvent,
	}, recvEvents(t, received, 2))
}

// Media that cannot be loaded is described too, with why; the round is still
// answered.
func TestRealtimeToolResponse_UnloadableMediaIsDescribed(t *testing.T) {
	received := make(chan map[string]any, 16)
	session := openProviderToolSession(t, received)

	sendToolRound(t, session, []providers.ToolResponse{{
		ToolCallID: "call_ref",
		Result:     "see image",
		Parts: []types.ContentPart{
			types.NewTextPart("see image"),
			types.NewImagePartFromStorageRef("media/abc", types.MIMETypeImagePNG, nil),
		},
	}})

	assert.Equal(t, []map[string]any{
		functionOutputEvent("call_ref", "see image\n[This tool returned an image (image/png, stored as media/abc) "+
			"that was not sent: it could not be loaded: storage service not configured but storage reference provided: media/abc.]"),
		responseCreateEvent,
	}, recvEvents(t, received, 2))
}

// Tool audio Realtime cannot convert to its input format, and an image it
// cannot label, is described with why.
func TestRealtimeToolResponse_UnconvertibleMediaSaysWhy(t *testing.T) {
	pcm := testPCM()
	b64 := func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
	eightBit := pcm16WAV(t, 24000, 1, pcm)
	binary.LittleEndian.PutUint16(eightBit[34:], 8)
	noData := pcm16WAV(t, 24000, 1, pcm)[:36]
	cases := []struct {
		name string
		part types.ContentPart
		want string
	}{
		{"raw PCM without its rate", types.NewAudioPartFromData(b64(pcm), "audio/pcm"),
			"[This tool returned an audio (audio/pcm) that was not sent: " +
				"OpenAI Realtime takes tool audio only as 16-bit PCM WAV or audio/pcm;rate=N.]"},
		{"unparseable MIME type", types.NewAudioPartFromData(b64(pcm), "audio/"),
			"[This tool returned an audio (audio/) that was not sent: " +
				"OpenAI Realtime takes tool audio only as 16-bit PCM WAV or audio/pcm;rate=N.]"},
		{"not a WAV", types.NewAudioPartFromData(b64(pcm), types.MIMETypeAudioWAV),
			"[This tool returned an audio (audio/wav) that was not sent: it is not a WAV file.]"},
		{"8-bit WAV", types.NewAudioPartFromData(b64(eightBit), types.MIMETypeAudioWAV),
			"[This tool returned an audio (audio/wav) that was not sent: " +
				"OpenAI Realtime takes tool audio only as 16-bit PCM WAV or audio/pcm;rate=N.]"},
		{"WAV without data", types.NewAudioPartFromData(b64(noData), types.MIMETypeAudioWAV),
			"[This tool returned an audio (audio/wav) that was not sent: its WAV header is truncated.]"},
		{"invalid base64", types.NewAudioPartFromData("!!!", types.MIMETypeAudioWAV),
			"[This tool returned an audio (audio/wav) that was not sent: its data is not valid base64.]"},
		{"image without a MIME type", types.ContentPart{Type: types.ContentTypeImage,
			Media: &types.MediaContent{Data: strPtr("iVBOR")}},
			"[This tool returned an image that was not sent: its image format is not stated.]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan map[string]any, 16)
			session := openProviderToolSession(t, received)

			sendToolRound(t, session, []providers.ToolResponse{{ToolCallID: "call_x", Parts: []types.ContentPart{tc.part}}})

			assert.Equal(t, []map[string]any{functionOutputEvent("call_x", tc.want), responseCreateEvent},
				recvEvents(t, received, 2))
		})
	}
}

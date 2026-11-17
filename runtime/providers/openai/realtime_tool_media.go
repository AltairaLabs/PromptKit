package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"mime"
	"strconv"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A Realtime function_call_output takes a string only. Media a tool returns
// therefore reaches the model as it does on Chat Completions: in a user
// message (conversation.item.create) that follows the round's outputs and
// precedes its response.create, with the output saying it is attached. A
// user message takes images as input_image (PNG or JPEG, as a data URI) and
// audio as input_audio in the session's input format, 16-bit PCM at 24 kHz.
// Media it cannot take, or that cannot be loaded, is described in the output
// instead (providers.UnsentToolMedia).

// apiNameRealtime names the API in a note about media a tool result could
// not send.
const apiNameRealtime = "OpenAI Realtime"

const (
	contentInputImage = "input_image"
	contentInputAudio = "input_audio"
)

// realtimeToolCaps is what a Realtime user message takes from a tool result.
// Audio formats are checked when the audio is converted (realtimeInputPCM).
var realtimeToolCaps = providers.MultimodalCapabilities{
	SupportsImages: true,
	ImageFormats:   []string{types.MIMETypeImagePNG, types.MIMETypeImageJPEG},
	SupportsAudio:  true,
}

// realtimeMaxEventBytes is OpenAI Realtime's limit on one client event
// ("up to a maximum of 15 MiB"); realtimeEventOverhead is what the user
// message around its media takes, at most.
const (
	realtimeMaxEventBytes = 15 << 20
	realtimeEventOverhead = 4 << 10
	bytesPerMiB           = 1 << 20
)

// mimeAudioL16 is raw big-endian 16-bit PCM (RFC 2586), lower-cased as
// mime.ParseMediaType returns it.
const mimeAudioL16 = "audio/l16"

// realtimeToolAudioFormats says which tool audio Realtime takes.
const realtimeToolAudioFormats = apiNameRealtime + " takes tool audio only as 16-bit PCM WAV or audio/pcm;rate=N"

// SupportsToolResponseParts reports that the session sends a tool result's
// media (providers.ToolResponse.Parts). Implements
// providers.ToolResponsePartsSupport.
func (s *RealtimeSession) SupportsToolResponseParts() bool { return true }

// realtimeToolRound is what a round of tool responses becomes on the wire:
// one function_call_output per response, then one user message per response
// whose media a user message can carry.
type realtimeToolRound struct {
	outputs []ConversationItem
	media   []ConversationItem
}

// buildToolRound converts responses to the items they are sent as. A
// response without parts sends its Result as it is.
func (s *RealtimeSession) buildToolRound(ctx context.Context, responses []providers.ToolResponse) realtimeToolRound {
	var loader *providers.MediaLoader
	var round realtimeToolRound
	for i := range responses {
		resp := &responses[i]
		output := resp.Result
		if (&types.MessageToolResult{Parts: resp.Parts}).HasMedia() {
			if loader == nil {
				loader = s.mediaLoader()
			}
			var content []ConversationContent
			output, content = s.toolOutputAndMedia(ctx, loader, resp)
			if len(content) > 0 {
				id := fmt.Sprintf("pk_tool_media_%d", s.toolMediaSeq.Add(1))
				s.toolMediaItems.Store(id, struct{}{})
				round.media = append(round.media, ConversationItem{
					ID: id, Type: typeMessage, Role: roleUser,
					Content: append([]ConversationContent{{
						Type: contentInputText,
						Text: fmt.Sprintf("Media returned by tool call %s:", resp.ToolCallID),
					}}, content...),
				})
			}
		}
		round.outputs = append(round.outputs, ConversationItem{
			Type: typeFunctionCallOutput, CallID: resp.ToolCallID, Output: output,
		})
	}
	return round
}

// mediaLoader is the loader one tool round's media is read through.
func (s *RealtimeSession) mediaLoader() *providers.MediaLoader {
	if s.newLoader != nil {
		return s.newLoader()
	}
	return providers.NewMediaLoader(providers.MediaLoaderConfig{})
}

// toolOutputAndMedia returns a response's function_call_output text — its
// text, then where its media went or why it was not sent — and the user
// message content that carries the media sent.
func (s *RealtimeSession) toolOutputAndMedia(
	ctx context.Context, loader *providers.MediaLoader, resp *providers.ToolResponse,
) (string, []ConversationContent) {
	result := &types.MessageToolResult{ID: resp.ToolCallID, Parts: resp.Parts}
	var notes []string
	var content []ConversationContent
	var kind string
	room := realtimeMaxEventBytes - realtimeEventOverhead
	for i := range resp.Parts {
		part := &resp.Parts[i]
		if part.Type == types.ContentTypeText {
			continue
		}
		c, why := realtimeMediaContent(ctx, loader, part)
		if why == "" {
			why = eventRoomRefusal(len(c.ImageURL)+len(c.Audio), room)
		}
		if why != "" {
			notes = append(notes, providers.UnsentToolMedia(s.providerID, result, part, why))
			continue
		}
		room -= len(c.ImageURL) + len(c.Audio)
		content = append(content, c)
		kind = part.Type
	}
	if note := providers.AttachedToolMediaNote(len(content), kind); note != "" {
		notes = append([]string{note}, notes...)
	}
	return providers.JoinToolNotes(resp.Result, notes), content
}

// eventRoomRefusal says why media of size encoded bytes cannot join a user
// message with room bytes left of the event limit, or "" when it fits.
func eventRoomRefusal(size, room int) string {
	switch {
	case size > realtimeMaxEventBytes-realtimeEventOverhead:
		return fmt.Sprintf("it is %.1f MB encoded, over %s's 15 MiB limit for one event",
			float64(size)/bytesPerMiB, apiNameRealtime)
	case size > room:
		return fmt.Sprintf("with the media before it, the message would be over %s's 15 MiB limit for one event",
			apiNameRealtime)
	}
	return ""
}

// realtimeMediaContent converts a tool result's media part to user message
// content, or says why it cannot be sent.
func realtimeMediaContent(
	ctx context.Context, loader *providers.MediaLoader, part *types.ContentPart,
) (ConversationContent, string) {
	if part.Media == nil {
		return ConversationContent{}, "it has no media content"
	}
	if why := providers.ToolMediaRefusal(apiNameRealtime, realtimeToolCaps, part); why != "" {
		return ConversationContent{}, why
	}
	if part.Type == types.ContentTypeImage && part.Media.MIMEType == "" {
		return ConversationContent{}, "its image format is not stated"
	}
	data, err := loader.GetBase64Data(ctx, part.Media)
	if err != nil {
		return ConversationContent{}, "it could not be loaded: " + err.Error()
	}
	if part.Type == types.ContentTypeImage {
		return ConversationContent{
			Type:     contentInputImage,
			ImageURL: "data:" + part.Media.MIMEType + ";base64," + data,
		}, ""
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return ConversationContent{}, "its data is not valid base64"
	}
	pcm, why := realtimeInputPCM(part.Media.MIMEType, raw)
	if why != "" {
		return ConversationContent{}, why
	}
	return ConversationContent{Type: contentInputAudio, Audio: base64.StdEncoding.EncodeToString(pcm)}, ""
}

// realtimeInputPCM converts tool audio to the session's input format, mono
// 16-bit PCM at 24 kHz, or says why it cannot be: it must be a 16-bit PCM WAV
// or raw PCM whose MIME type states its rate (audio/pcm;rate=16000).
func realtimeInputPCM(mimeType string, raw []byte) ([]byte, string) {
	mediaType, params, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return nil, realtimeToolAudioFormats
	}
	var pcm []byte
	var rate int
	switch mediaType {
	case types.MIMETypeAudioWAV, "audio/x-wav", "audio/wave":
		var why string
		if pcm, rate, why = wavPCM16(raw); why != "" {
			return nil, why
		}
	case "audio/pcm", mimeAudioL16:
		if rate, err = strconv.Atoi(params["rate"]); err != nil || rate <= 0 {
			return nil, realtimeToolAudioFormats
		}
		pcm = raw[:len(raw)&^1] // whole 16-bit samples only
		if mediaType == mimeAudioL16 {
			pcm = swapPCM16(pcm) // L16 is big-endian (RFC 2586)
		}
	default:
		return nil, realtimeToolAudioFormats
	}
	if len(pcm) == 0 {
		return nil, "it holds no audio"
	}
	out, err := audio.ResamplePCM16(pcm, rate, DefaultRealtimeSampleRate)
	if err != nil {
		return nil, "it could not be resampled: " + err.Error()
	}
	return out, ""
}

// swapPCM16 is 16-bit samples with their byte order swapped.
func swapPCM16(pcm []byte) []byte {
	out := make([]byte, len(pcm))
	for i := 0; i+1 < len(pcm); i += 2 {
		out[i], out[i+1] = pcm[i+1], pcm[i]
	}
	return out
}

// WAV header layout: the RIFF header, then chunks of a 4-byte ID and a
// 4-byte little-endian size.
const (
	wavRIFFHeaderLen  = 12
	wavChunkHeaderLen = 8
	wavFmtLen         = 16
	wavFormatPCM      = 1
	wavFormatExt      = 0xFFFE // WAVE_FORMAT_EXTENSIBLE: the format is the subformat GUID's
	wavFmtExtLen      = 40
	wavSubFormatAt    = 24
	wavBitsPCM16      = 16
	wavUnknownSize    = 0xFFFFFFFF
	wavChunkFmt       = "fmt "
	wavChunkData      = "data"
	wavTruncated      = "its WAV header is truncated"
)

// ksSubtypeGUIDTail is the part every KSDATAFORMAT_SUBTYPE GUID shares after
// its leading format code (00000001-0000-0010-8000-00AA00389B71 is PCM).
var ksSubtypeGUIDTail = []byte{0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}

// wavPCM16 returns a WAV's samples and rate when it is mono 16-bit PCM, or
// says why it cannot be sent.
func wavPCM16(raw []byte) (pcm []byte, rate int, why string) {
	if len(raw) < wavRIFFHeaderLen || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, "it is not a WAV file"
	}
	fmtBody, data, ok := wavChunks(raw)
	if !ok || len(fmtBody) < wavFmtLen {
		return nil, 0, wavTruncated
	}
	format := binary.LittleEndian.Uint16(fmtBody)
	if format == wavFormatExt && len(fmtBody) >= wavFmtExtLen &&
		bytes.Equal(fmtBody[wavSubFormatAt+2:wavFmtExtLen], ksSubtypeGUIDTail) {
		format = binary.LittleEndian.Uint16(fmtBody[wavSubFormatAt:])
	}
	channels := binary.LittleEndian.Uint16(fmtBody[2:])
	rate = int(binary.LittleEndian.Uint32(fmtBody[4:]))
	bits := binary.LittleEndian.Uint16(fmtBody[14:])
	if format != wavFormatPCM || bits != wavBitsPCM16 {
		return nil, 0, realtimeToolAudioFormats
	}
	if channels != 1 {
		return nil, 0, fmt.Sprintf(
			"%s takes tool audio only as mono 16-bit PCM; this WAV has %d channels", apiNameRealtime, channels)
	}
	return data[:len(data)&^1], rate, ""
}

// wavChunks returns a WAV's fmt chunk and its data chunk; ok is false when
// either is missing. A chunk whose size is unknown (a streamed WAV, size
// 0xFFFFFFFF) or overstated runs to the end of the file.
func wavChunks(raw []byte) (fmtBody, data []byte, ok bool) {
	for off := wavRIFFHeaderLen; off+wavChunkHeaderLen <= len(raw); {
		size := binary.LittleEndian.Uint32(raw[off+4 : off+8])
		body := off + wavChunkHeaderLen
		end := len(raw)
		if size != wavUnknownSize && int64(body)+int64(size) < int64(end) {
			end = body + int(size)
		}
		switch string(raw[off : off+4]) {
		case wavChunkFmt:
			fmtBody = raw[body:end]
		case wavChunkData:
			return fmtBody, raw[body:end], fmtBody != nil
		}
		off = end + int(size&1) // chunks are word-aligned
	}
	return nil, nil, false
}

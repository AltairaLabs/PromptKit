package tts

import (
	"context"
	"encoding/binary"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// riffChunk is one WAV chunk: its ID, the size its header claims, and its body.
type riffChunk struct {
	id   string
	size uint32
	body []byte
}

func chunk(id string, body []byte) riffChunk {
	return riffChunk{id: id, size: uint32(len(body)), body: body}
}

// riff assembles a WAV from chunks, padding odd-length bodies as RIFF does.
func riff(chunks ...riffChunk) []byte {
	out := []byte("RIFF\x00\x00\x00\x00WAVE")
	for _, c := range chunks {
		out = append(out, c.id...)
		out = binary.LittleEndian.AppendUint32(out, c.size)
		out = append(out, c.body...)
		if len(c.body)%2 == 1 {
			out = append(out, 0)
		}
	}
	return out
}

// pcmFmt is a fmt chunk body for PCM at rate with the given channels and bits.
func pcmFmt(rate, channels, bits int) []byte {
	b := binary.LittleEndian.AppendUint16(nil, 1)
	b = binary.LittleEndian.AppendUint16(b, uint16(channels))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate*channels*bits/8))
	b = binary.LittleEndian.AppendUint16(b, uint16(channels*bits/8))
	return binary.LittleEndian.AppendUint16(b, uint16(bits))
}

func TestWavMonoPCM16(t *testing.T) {
	mono := pcmFmt(24000, 1, 16)
	tests := []struct {
		name     string
		raw      []byte
		wantPCM  []byte
		wantRate int
		why      string
	}{
		{"data then a manifest chunk", riff(chunk("fmt ", mono), chunk("data", testPCM), chunk("C2PA", []byte("m"))),
			testPCM, 24000, ""},
		{"odd-length chunk before data is padded", riff(chunk("LIST", []byte("abc")), chunk("fmt ", pcmFmt(16000, 1, 16)),
			chunk("data", testPCM)), testPCM, 16000, ""},
		{"empty data chunk then another chunk", riff(chunk("fmt ", mono), chunk("data", nil), chunk("C2PA", []byte("mm"))),
			[]byte{}, 24000, ""},
		{"empty data chunk at the very end", riff(chunk("fmt ", mono), chunk("data", nil)), []byte{}, 24000, ""},
		{"unknown data size, then a manifest", riff(chunk("fmt ", mono),
			riffChunk{id: "data", size: 0xFFFFFFFF, body: testPCM}, chunk("C2PA", []byte("manifest"))),
			nil, 0, `WAV "data" chunk is longer than the audio`},
		{"unknown data size at the end",
			riff(chunk("fmt ", mono), riffChunk{id: "data", size: 0xFFFFFFFF, body: testPCM}),
			nil, 0, `WAV "data" chunk is longer than the audio`},
		{"truncated data", riff(chunk("fmt ", mono), riffChunk{id: "data", size: 10, body: testPCM}),
			nil, 0, `WAV "data" chunk is longer than the audio`},
		{"two data chunks are joined", riff(chunk("fmt ", mono), chunk("data", testPCM[:4]), chunk("C2PA", []byte("m")),
			chunk("data", testPCM[4:])), testPCM, 24000, ""},
		{"header only", []byte("RIFF\x00\x00\x00\x00WAVE"), nil, 0, "WAV has no fmt and data chunks"},
		{"too short", []byte("RIFF\x00\x00\x00\x00WAV"), nil, 0, "audio is not a WAV"},
		{"not RIFF", append([]byte("ID3\x04"), make([]byte, 20)...), nil, 0, "audio is not a WAV"},
		{"RIFF but not WAVE", []byte("RIFF\x00\x00\x00\x00AVI LIST\x00\x00\x00\x00"), nil, 0, "audio is not a WAV"},
		{"fmt but no data", riff(chunk("fmt ", mono)), nil, 0, "WAV has no fmt and data chunks"},
		{"data but no fmt", riff(chunk("data", testPCM)), nil, 0, "WAV has no fmt and data chunks"},
		{"8-bit", riff(chunk("fmt ", pcmFmt(24000, 1, 8)), chunk("data", testPCM)), nil, 0, "WAV is not mono 16-bit PCM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pcm, rate, why := wavMonoPCM16(tt.raw)
			assert.Equal(t, tt.why, why)
			assert.Equal(t, tt.wantRate, rate)
			if tt.wantPCM == nil {
				assert.Nil(t, pcm)
			} else {
				assert.Equal(t, tt.wantPCM, append([]byte{}, pcm...))
			}
		})
	}
}

// An audio/L16 reply whose rate parameter is missing, zero or not a number
// is taken at Gemini's 24 kHz.
func TestGeminiAudioPCM_L16RateFallsBackTo24k(t *testing.T) {
	for _, mimeType := range []string{"audio/L16", "audio/L16;rate=0", "audio/L16;rate=abc", "audio/pcm;rate=-5"} {
		pcm, rate, err := geminiAudioPCM(mimeType, testPCM)
		require.NoError(t, err, mimeType)
		assert.Equal(t, 24000, rate, mimeType)
		assert.Equal(t, testPCM, pcm, mimeType)
	}
	_, rate, err := geminiAudioPCM("audio/L16;codec=pcm;rate=16000", testPCM)
	require.NoError(t, err)
	assert.Equal(t, 16000, rate)
}

// Gemini's audio/L16 is little-endian, unlike RFC 2586's network order: the
// bytes are returned as they came, with no swap. (Measured on
// gemini-2.5-flash-preview-tts: read little-endian, adjacent samples differ by
// 734 on average; byte-swapped, by 15315, which is noise, not speech.)
func TestGeminiAudioPCM_L16IsLittleEndian(t *testing.T) {
	le := []byte{0xe6, 0xff, 0x05, 0x00} // -26, 5 little-endian
	pcm, _, err := geminiAudioPCM("audio/L16;codec=pcm;rate=24000", le)
	require.NoError(t, err)
	assert.Equal(t, le, pcm)
}

// With no sample rate asked for, the audio comes back at 24 kHz, the rate
// FormatPCM16 and SupportedFormats name, whatever rate the reply was in.
func TestGeminiService_Synthesize_UnsetRateIs24k(t *testing.T) {
	src := make([]byte, 3200) // 100 ms at 16 kHz
	for i := range src {
		src[i] = byte(i * 3)
	}
	srv, _ := geminiTTSServer(t, http.StatusOK, geminiAudioResponse("audio/L16;codec=pcm;rate=16000", src))
	svc := NewGemini("k", base.WithBaseURL(srv.URL))
	rc, err := svc.Synthesize(context.Background(), "Hi", SynthesisConfig{})
	require.NoError(t, err)
	want, err := audio.ResamplePCM16(src, 16000, 24000)
	require.NoError(t, err)
	assert.Equal(t, want, readAllAudio(t, rc))
}

// A client given to a Vertex service keeps its transport and still sends the
// platform credential.
func TestNewGeminiOnVertex_WithClientKeepsTheCredential(t *testing.T) {
	srv, calls := geminiTTSServer(t, http.StatusOK,
		geminiAudioResponse("audio/wav", geminiWAV(testPCM, 24000, "")))
	var viaCustom bool
	custom := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		viaCustom = true
		return http.DefaultTransport.RoundTrip(r)
	})}
	svc := NewGeminiOnVertex(bearerCred("gcp-token"), "p", "us-central1",
		base.WithClient(custom), base.WithBaseURL(srv.URL))
	rc, err := svc.Synthesize(context.Background(), "Hi", SynthesisConfig{})
	require.NoError(t, err)
	_ = readAllAudio(t, rc)
	assert.Equal(t, "Bearer gcp-token", (<-calls).auth)
	assert.True(t, viaCustom, "the caller's transport carries the request")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// SynthesizeTTS honors the request's sample rate: the 24 kHz reply comes
// back resampled to it.
func TestGeminiService_SynthesizeTTSResamplesToTheRequestedRate(t *testing.T) {
	long := make([]byte, 4800)
	for i := range long {
		long[i] = byte(i * 7)
	}
	srv, _ := geminiTTSServer(t, http.StatusOK, geminiAudioResponse("audio/wav", geminiWAV(long, 24000, "")))
	svc := NewGemini("k", base.WithBaseURL(srv.URL))
	stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{Text: "Hi", SampleRate: 16000})
	require.NoError(t, err)
	got, _, err := base.ReadAllAudio(stream)
	require.NoError(t, err)
	want, err := audio.ResamplePCM16(long, 24000, 16000)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	_, err = svc.SynthesizeTTS(context.Background(), base.TTSRequest{Text: "Hi", Format: "mp3"})
	assert.ErrorIs(t, err, ErrInvalidFormat, "a requested format other than pcm is refused")
}

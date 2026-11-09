package tts

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// oneSecondAt24k is one second of a 440 Hz tone as the speech API returns
// PCM: 24 kHz, 16-bit little-endian mono.
func oneSecondAt24k() []byte {
	out := make([]byte, 24000*2)
	for i := 0; i < 24000; i++ {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/24000) * 12000)
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out
}

// The speech API only returns 24 kHz PCM. A caller asking for another rate
// gets audio at that rate: one second in is one second out, at the rate asked
// for. 0 or 24000 return the API's bytes unchanged.
func TestOpenAISynthesizeTTS_PCMIsResampledToTheRequestedRate(t *testing.T) {
	pcm := oneSecondAt24k()
	var gotFormat string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotFormat, _ = body["response_format"].(string)
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write(pcm)
	}))
	defer server.Close()
	svc := NewOpenAI("k", base.WithBaseURL(server.URL))

	for _, tc := range []struct {
		rate        int
		wantSamples int
	}{
		{0, 24000}, {24000, 24000}, {16000, 16000}, {8000, 8000}, {48000, 48000}, {22050, 22050},
	} {
		stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{
			Text: "hello", Format: "pcm", SampleRate: tc.rate,
		})
		require.NoError(t, err, "rate %d", tc.rate)
		got, _, err := base.ReadAllAudio(stream)
		require.NoError(t, err, "rate %d", tc.rate)

		assert.Equal(t, "pcm", gotFormat)
		assert.Equal(t, tc.wantSamples, len(got)/2, "samples for one second requested at %d Hz", tc.rate)
		if tc.wantSamples == 24000 {
			assert.Equal(t, pcm, got, "no resampling at the API's own rate")
		}
	}
}

// Resampling streams: the first resampled bytes reach the caller while the
// server is still holding the rest of the body.
func TestOpenAISynthesizeTTS_ResampledPCMStillStreams(t *testing.T) {
	pcm := oneSecondAt24k()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write(pcm[:8192])
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write(pcm[8192:])
	}))
	defer server.Close()

	stream, err := NewOpenAI("k", base.WithBaseURL(server.URL)).SynthesizeTTS(context.Background(),
		base.TTSRequest{Text: "hello", Format: "pcm", SampleRate: 16000})
	require.NoError(t, err)
	// Close drains the stream, so the server must be let finish first.
	defer func() {
		close(release)
		_ = stream.Close()
	}()

	select {
	case chunk := <-stream.Chunks():
		require.NoError(t, chunk.Error)
		assert.NotEmpty(t, chunk.Data, "resampled audio arrives before the body ends")
	case <-time.After(2 * time.Second):
		t.Fatal("no audio until the whole body arrived: resampling buffers the stream")
	}
}

// failingBody returns its data with an error in the same Read.
type failingBody struct {
	data []byte
	err  error
	done bool
}

func (b *failingBody) Read(p []byte) (int, error) {
	if b.done {
		return 0, b.err
	}
	b.done = true
	return copy(p, b.data), b.err
}

func (b *failingBody) Close() error { return nil }

// A source error that arrives with data is returned only after the audio it
// came with, and again on every later Read.
func TestResamplingReader_ErrorFollowsItsAudio(t *testing.T) {
	boom := errors.New("connection reset")
	r, err := newResamplingReader(&failingBody{data: make([]byte, 4800), err: boom}, 24000, 16000)
	require.NoError(t, err)

	got, err := io.ReadAll(r)
	require.ErrorIs(t, err, boom)
	assert.NotEmpty(t, got, "the audio read with the error is not lost")
	_, err = r.Read(make([]byte, 8))
	assert.ErrorIs(t, err, boom, "the error is sticky")

	n, err := r.Read(nil)
	assert.Equal(t, 0, n)
	assert.NoError(t, err, "an empty read does not touch the source")
}

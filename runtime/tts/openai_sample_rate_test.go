package tts

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

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

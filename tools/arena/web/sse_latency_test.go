package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
)

// TestSSELatency_PublishToWrite is a diagnostic probe. It measures the gap
// between AudioRouter.Publish (server emits a frame) and the moment the
// SSE writer flushes it to the wire. If gaps are sub-ms, server-side
// batching can be ruled out as the source of inter-turn playback offset
// on the live demo. If gaps are large, the SSE relay is buffering.
//
// Not strictly a regression-locking test; failure thresholds are
// generous. Run with -v to read the per-frame measurements.
func TestSSELatency_PublishToWrite(t *testing.T) {
	router := arenaaudio.NewAudioRouter(arenaaudio.Rate24k)
	defer router.Close()

	adapter := NewEventAdapter()
	adapter.AttachAudioRouter("probe", router, arenaaudio.Rate24k)

	srv := NewServer(adapter, nil, nil, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events?audio=1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer resp.Body.Close()

	// Read the initial keepalive so the handler has fully registered the
	// client before we start publishing. Without this the first few
	// frames can race the Register call.
	reader := bufio.NewReader(resp.Body)
	for {
		line, rerr := reader.ReadString('\n')
		if rerr != nil {
			t.Fatalf("initial read: %v", rerr)
		}
		if strings.HasPrefix(line, ": connected") {
			break
		}
	}

	const nFrames = 30
	const interval = 20 * time.Millisecond
	publishTimes := make([]time.Time, nFrames)

	go func() {
		for i := 0; i < nFrames; i++ {
			publishTimes[i] = time.Now()
			router.Publish(arenaaudio.Frame{
				Direction: arenaaudio.DirectionOutput,
				Samples:   make([]int16, 480), // 20ms @ 24kHz mono
				Timestamp: publishTimes[i],
			})
			time.Sleep(interval)
		}
	}()

	received := 0
	var maxLatency, sumLatency time.Duration
	for received < nFrames {
		line, rerr := reader.ReadString('\n')
		if rerr != nil {
			t.Fatalf("read after %d frames: %v", received, rerr)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		now := time.Now()
		// The line includes the frame's JSON payload. We don't decode it —
		// we only care that bytes arrived on the wire at `now`.
		if received < len(publishTimes) {
			latency := now.Sub(publishTimes[received])
			if latency > maxLatency {
				maxLatency = latency
			}
			sumLatency += latency
			t.Logf("frame %d publish→client %v", received, latency)
		}
		received++
	}

	avg := sumLatency / time.Duration(nFrames)
	t.Logf("SUMMARY frames=%d avg=%v max=%v", nFrames, avg, maxLatency)

	// Generous bound: if average exceeds half a chunk-duration (10ms),
	// there's a noticeable batching effect worth investigating.
	if avg > 50*time.Millisecond {
		t.Errorf("average publish→client latency %v exceeds 50ms — SSE relay is batching", avg)
	}
}

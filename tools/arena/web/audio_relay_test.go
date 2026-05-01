package web

import (
	"strings"
	"testing"
	"time"

	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
)

func TestEventAdapter_AttachAudioRouter_BroadcastsToAudioClients(t *testing.T) {
	a := NewEventAdapter()

	clientCh := make(chan []byte, 10)
	a.RegisterAudio(clientCh)
	defer a.UnregisterAudio(clientCh)

	router := arenaaudio.NewAudioRouter(arenaaudio.Rate24k)
	defer router.Close()

	a.AttachAudioRouter("test-run", router, arenaaudio.Rate24k)

	router.Publish(arenaaudio.Frame{
		Direction: arenaaudio.DirectionInput,
		Samples:   []int16{1, 2, 3, 4},
	})

	select {
	case msg := <-clientCh:
		s := string(msg)
		if !strings.Contains(s, "event: audio") {
			t.Fatalf("expected audio SSE event, got: %s", s)
		}
		if !strings.Contains(s, `"direction":"input"`) {
			t.Fatalf("expected direction=input, got: %s", s)
		}
		if !strings.Contains(s, `"run_id":"test-run"`) {
			t.Fatalf("expected run_id, got: %s", s)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("client did not receive audio SSE event")
	}
}

func TestEventAdapter_NoAudioToRegularClients(t *testing.T) {
	a := NewEventAdapter()
	regularCh := a.Register()
	defer a.Unregister(regularCh)

	router := arenaaudio.NewAudioRouter(arenaaudio.Rate24k)
	defer router.Close()

	a.AttachAudioRouter("test-run", router, arenaaudio.Rate24k)

	router.Publish(arenaaudio.Frame{
		Direction: arenaaudio.DirectionOutput,
		Samples:   []int16{1},
	})

	select {
	case msg := <-regularCh:
		t.Fatalf("regular SSE client received audio event: %s", msg)
	case <-time.After(100 * time.Millisecond):
		// Expected — no audio for regular clients
	}
}

func TestEventAdapter_AudioClientUnregisterStopsDelivery(t *testing.T) {
	a := NewEventAdapter()
	clientCh := make(chan []byte, 10)
	a.RegisterAudio(clientCh)

	router := arenaaudio.NewAudioRouter(arenaaudio.Rate24k)
	defer router.Close()

	a.AttachAudioRouter("test-run", router, arenaaudio.Rate24k)

	a.UnregisterAudio(clientCh)

	router.Publish(arenaaudio.Frame{
		Direction: arenaaudio.DirectionInput,
		Samples:   []int16{1},
	})

	select {
	case msg := <-clientCh:
		t.Fatalf("expected no message after unregister; got: %s", msg)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventAdapter_SlowClientGetsDropped(t *testing.T) {
	a := NewEventAdapter()
	// Tiny buffer simulates a stalled client
	slow := make(chan []byte, 1)
	a.RegisterAudio(slow)
	defer a.UnregisterAudio(slow)

	router := arenaaudio.NewAudioRouter(arenaaudio.Rate24k)
	defer router.Close()

	a.AttachAudioRouter("test-run", router, arenaaudio.Rate24k)

	for i := 0; i < 10; i++ {
		router.Publish(arenaaudio.Frame{Direction: arenaaudio.DirectionInput, Samples: []int16{int16(i)}})
	}
	time.Sleep(100 * time.Millisecond)

	count := 0
	for {
		select {
		case <-slow:
			count++
		default:
			goto done
		}
	}
done:
	// Slow client's channel only holds 1 buffered + 1 in-flight at most.
	// We just want to confirm it didn't crash and buffered something.
	if count == 0 {
		t.Fatal("expected slow client to receive at least one frame")
	}
	// Other deliveries were dropped — the test passes as long as we didn't deadlock.
}

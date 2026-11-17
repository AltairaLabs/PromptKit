package audio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

var playoutT0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestPCM16Duration(t *testing.T) {
	assert.Equal(t, 500*time.Millisecond, PCM16Duration(24000, 24000, 1))
	assert.Equal(t, 250*time.Millisecond, PCM16Duration(24000, 24000, 2), "stereo frames are 4 bytes")
	assert.Equal(t, 500*time.Millisecond, PCM16Duration(24000, 24000, 0), "no channel count is mono")
	assert.Zero(t, PCM16Duration(24000, 0, 1))
}

// Item A (5 s) then B (1 s), delivered at once; the listener stops 2 s in:
// A was heard for 2 s, B not at all.
func TestPlayout_StopCutsEachUnheardItem(t *testing.T) {
	c := &testClock{t: playoutT0}
	p := NewPlayout(c.now)
	p.Add("A", 0, 3*time.Second)
	p.Add("A", 0, 2*time.Second) // more of A, queued straight after
	p.Add("B", 0, time.Second)
	c.t = playoutT0.Add(2 * time.Second)

	assert.True(t, p.Playing())
	assert.Equal(t, []PlayoutCut{
		{ItemID: "A", ContentIndex: 0, Heard: 2 * time.Second},
		{ItemID: "B", ContentIndex: 0, Heard: 0},
	}, p.Stop())
	assert.False(t, p.Playing(), "Stop ends the playout")
	assert.Empty(t, p.Stop())
}

func TestPlayout_FullyHeardItemsAreNotCut(t *testing.T) {
	c := &testClock{t: playoutT0}
	p := NewPlayout(c.now)
	p.Add("A", 0, time.Second)
	p.Add("B", 1, time.Second)
	c.t = playoutT0.Add(1500 * time.Millisecond)

	assert.Equal(t, []PlayoutCut{{ItemID: "B", ContentIndex: 1, Heard: 500 * time.Millisecond}}, p.Stop())
}

func TestPlayout_AudioAfterSilencePlaysFromArrival(t *testing.T) {
	c := &testClock{t: playoutT0}
	p := NewPlayout(c.now)
	p.Add("A", 0, time.Second)
	c.t = playoutT0.Add(time.Second)
	assert.False(t, p.Playing(), "the end of the audio is not playing")
	c.t = playoutT0.Add(5 * time.Second)
	p.Add("A", 0, time.Second) // same item, but after a gap: plays from now
	c.t = playoutT0.Add(5200 * time.Millisecond)

	assert.Equal(t, []PlayoutCut{{ItemID: "A", Heard: 200 * time.Millisecond}}, p.Stop())
}

func TestPlayout_IgnoresEmptyAudio(t *testing.T) {
	p := NewPlayout(nil)
	p.Add("A", 0, 0)
	assert.False(t, p.Playing())
	assert.Empty(t, p.Stop())
}

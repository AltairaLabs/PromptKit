package audio

import "sync"

// JitterBuffer is a bounded FIFO ring buffer of int16 PCM samples (mono).
// It is safe for concurrent use: Push from a producer goroutine while Pull
// or Clear are called from the duplex playback loop.
//
// Overflow policy: when a Push would exceed capacity the OLDEST samples are
// dropped to make room for the newest audio, and the dropped count is added
// to the cumulative Drops counter.
//
// Underrun policy: Pull always returns exactly n samples; when fewer than n
// are buffered the tail is zero-filled (silence).
type JitterBuffer struct {
	mu       sync.Mutex
	buf      []int16
	head     int   // index of next sample to read
	count    int   // number of valid samples in buf
	capacity int   // maximum samples
	drops    int64 // cumulative dropped-sample count
}

// NewJitterBuffer returns a JitterBuffer with the given maximum capacity in
// samples.  A capacity of zero is valid but all pushes will drop immediately.
func NewJitterBuffer(capacitySamples int) *JitterBuffer {
	return &JitterBuffer{
		buf:      make([]int16, capacitySamples),
		capacity: capacitySamples,
	}
}

// Push appends samples to the buffer.  If appending would exceed capacity,
// the oldest samples are discarded first and the drop counter incremented.
func (j *JitterBuffer) Push(samples []int16) {
	if len(samples) == 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.capacity == 0 {
		j.drops += int64(len(samples))
		return
	}

	// If the incoming slice is larger than the total capacity, keep only the
	// last capacity samples (trim from the front of the incoming slice).
	if len(samples) > j.capacity {
		dropped := len(samples) - j.capacity
		j.drops += int64(dropped)
		samples = samples[dropped:]
	}

	// How many samples need to be evicted to fit the new ones?
	free := j.capacity - j.count
	if need := len(samples) - free; need > 0 {
		// Advance the read head to drop the oldest `need` samples.
		j.head = (j.head + need) % j.capacity
		j.count -= need
		j.drops += int64(need)
	}

	// Append samples into the ring buffer.
	for _, s := range samples {
		tail := (j.head + j.count) % j.capacity
		j.buf[tail] = s
		j.count++
	}
}

// Pull removes and returns exactly n samples from the front of the buffer.
// If fewer than n samples are available the returned slice is zero-filled
// from the point of underrun through index n-1.
func (j *JitterBuffer) Pull(n int) []int16 {
	out := make([]int16, n)
	if n == 0 {
		return out
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	// Zero-capacity buffers never hold samples; return n zeros without touching
	// the ring (the modulo below would divide by zero otherwise).
	if j.capacity == 0 {
		return out
	}

	avail := j.count
	if avail > n {
		avail = n
	}
	for i := 0; i < avail; i++ {
		out[i] = j.buf[j.head]
		j.head = (j.head + 1) % j.capacity
		j.count--
	}
	// Tail [avail:n] is already zero from make().
	return out
}

// Clear drops all buffered samples.  The next Pull will return silence.
func (j *JitterBuffer) Clear() {
	j.mu.Lock()
	j.head = 0
	j.count = 0
	j.mu.Unlock()
}

// Len returns the number of samples currently in the buffer.
func (j *JitterBuffer) Len() int {
	j.mu.Lock()
	n := j.count
	j.mu.Unlock()
	return n
}

// Drops returns the cumulative number of samples that have been dropped due
// to buffer overflow.
func (j *JitterBuffer) Drops() int64 {
	j.mu.Lock()
	d := j.drops
	j.mu.Unlock()
	return d
}

package audio

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// rmsCadenceHz is the target frequency at which RMSFrame events are emitted.
	rmsCadenceHz = 30
	// routerInBuffer is the size of the router's intake channel.
	routerInBuffer = 50
	// int16ScaleFactor normalises s16le samples to the [-1.0, 1.0] range.
	int16ScaleFactor = 32768.0
	// rmsDecayFactor is the multiplier applied to the rolling RMS accumulator
	// at each cadence tick to approximate a windowed average without storing
	// per-sample history.
	rmsDecayFactor = 0.5
	// rmsMaxLevel clamps RMS output to the documented [0.0, 1.0] range.
	rmsMaxLevel = 1.0
)

// AudioRouter is a per-run goroutine that fans audio frames out to
// per-consumer bounded channels. Slow consumers drop their own frames;
// other consumers and the router keep flowing.
//
// Lifecycle: NewAudioRouter starts the dispatch goroutine. Close stops it
// and closes all consumer channels. Close is idempotent.
type AudioRouter struct {
	rate int

	in      chan Frame
	rmsSubs []chan RMSFrame
	rmsMu   sync.RWMutex

	consumers map[string]chan Frame
	drops     map[string]*atomic.Int64
	mu        sync.RWMutex

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once

	// Per-direction RMS accumulator state.
	rmsAccumMu sync.Mutex
	rmsAccum   map[Direction]*rmsState
}

type rmsState struct {
	sumSq    float64
	count    int
	lastEmit time.Time
}

// NewAudioRouter constructs a router and starts its dispatch goroutine.
// canonicalRate is the rate frames are expected to arrive at; resampling
// happens upstream in MonitorTap before Publish is called.
func NewAudioRouter(canonicalRate int) *AudioRouter {
	ctx, cancel := context.WithCancel(context.Background())
	r := &AudioRouter{
		rate:      canonicalRate,
		in:        make(chan Frame, routerInBuffer),
		consumers: make(map[string]chan Frame),
		drops:     make(map[string]*atomic.Int64),
		ctx:       ctx,
		cancel:    cancel,
		rmsAccum: map[Direction]*rmsState{
			DirectionInput:  {},
			DirectionOutput: {},
		},
	}
	r.wg.Add(1)
	go r.dispatchLoop()
	return r
}

// Subscribe registers a new consumer. Returns a receive-only channel.
// bufSize controls how many frames may queue before drop-on-overflow.
func (r *AudioRouter) Subscribe(id string, bufSize int) <-chan Frame {
	ch := make(chan Frame, bufSize)
	r.mu.Lock()
	r.consumers[id] = ch
	r.drops[id] = &atomic.Int64{}
	r.mu.Unlock()
	return ch
}

// SubscribeRMS registers a level meter consumer for the RMSFrame stream.
func (r *AudioRouter) SubscribeRMS(bufSize int) <-chan RMSFrame {
	ch := make(chan RMSFrame, bufSize)
	r.rmsMu.Lock()
	r.rmsSubs = append(r.rmsSubs, ch)
	r.rmsMu.Unlock()
	return ch
}

// Unsubscribe removes a consumer and closes its channel.
// Subsequent Publish calls will not deliver to this consumer.
func (r *AudioRouter) Unsubscribe(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch, ok := r.consumers[id]; ok {
		close(ch)
		delete(r.consumers, id)
		delete(r.drops, id)
	}
}

// DropCount returns the number of frames dropped for a given consumer.
func (r *AudioRouter) DropCount(id string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if d, ok := r.drops[id]; ok {
		return d.Load()
	}
	return 0
}

// Publish enqueues a frame for fan-out. Non-blocking: drops the frame
// silently if the router input buffer is full.
func (r *AudioRouter) Publish(frame Frame) {
	select {
	case r.in <- frame:
	default:
		// Router input full; frame dropped at intake.
	}
}

// Close stops the router goroutine and closes all consumer channels.
// Safe to call multiple times.
func (r *AudioRouter) Close() {
	r.closeOnce.Do(func() {
		r.cancel()
		close(r.in)
		r.wg.Wait()

		r.mu.Lock()
		for id, ch := range r.consumers {
			close(ch)
			delete(r.consumers, id)
		}
		r.mu.Unlock()

		r.rmsMu.Lock()
		for _, ch := range r.rmsSubs {
			close(ch)
		}
		r.rmsSubs = nil
		r.rmsMu.Unlock()
	})
}

func (r *AudioRouter) dispatchLoop() {
	defer r.wg.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case frame, ok := <-r.in:
			if !ok {
				return
			}
			r.fanout(frame)
			r.accumulateRMS(frame)
		}
	}
}

func (r *AudioRouter) fanout(frame Frame) {
	r.mu.RLock()
	for id, ch := range r.consumers {
		select {
		case ch <- frame:
		default:
			r.drops[id].Add(1)
		}
	}
	r.mu.RUnlock()
}

func (r *AudioRouter) accumulateRMS(frame Frame) {
	r.rmsAccumMu.Lock()
	defer r.rmsAccumMu.Unlock()

	state := r.rmsAccum[frame.Direction]
	if state == nil {
		return
	}
	for _, s := range frame.Samples {
		f := float64(s) / int16ScaleFactor
		state.sumSq += f * f
		state.count++
	}

	now := frame.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	cadence := time.Second / rmsCadenceHz
	if now.Sub(state.lastEmit) < cadence {
		return
	}
	state.lastEmit = now

	r.emitRMS(now)

	// Decay accumulator (rolling window approximation: halve each cadence tick).
	for d := range r.rmsAccum {
		r.rmsAccum[d].sumSq *= rmsDecayFactor
		r.rmsAccum[d].count /= 2
	}
}

func (r *AudioRouter) emitRMS(timestamp time.Time) {
	user := r.computeRMSLocked(DirectionInput)
	agent := r.computeRMSLocked(DirectionOutput)

	frame := RMSFrame{
		UserLevel:  user,
		AgentLevel: agent,
		Timestamp:  timestamp,
	}

	r.rmsMu.RLock()
	for _, ch := range r.rmsSubs {
		select {
		case ch <- frame:
		default:
			// drop — meter consumer fell behind
		}
	}
	r.rmsMu.RUnlock()
}

// computeRMSLocked must be called with rmsAccumMu held.
func (r *AudioRouter) computeRMSLocked(d Direction) float32 {
	state := r.rmsAccum[d]
	if state == nil || state.count == 0 {
		return 0
	}
	rms := math.Sqrt(state.sumSq / float64(state.count))
	if rms > rmsMaxLevel {
		rms = rmsMaxLevel
	}
	return float32(rms)
}

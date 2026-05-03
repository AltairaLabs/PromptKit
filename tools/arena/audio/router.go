package audio

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

const (
	// rmsCadenceHz is the target frequency at which RMSFrame events are emitted.
	rmsCadenceHz = 30
	// routerInBuffer is the size of the router's intake channel. Sized to
	// absorb a multi-second utterance arriving in burst mode without
	// dropping at intake; the dispatch loop drains it as fast as it can
	// fan out to consumers.
	routerInBuffer = 512
	// int16ScaleFactor normalises s16le samples to the [-1.0, 1.0] range.
	int16ScaleFactor = 32768.0
	// rmsMaxLevel clamps RMS output to the documented [0.0, 1.0] range.
	rmsMaxLevel = 1.0
)

// intakeDropLogThrottle controls how often "intake full" warnings emit.
// Tuned to surface a problem without flooding logs in a sustained-overflow
// run.
const intakeDropLogThrottle = 100

// AudioRouter is a per-run goroutine that fans audio frames out to
// per-consumer bounded channels. Slow consumers drop their own frames;
// other consumers and the router keep flowing.
//
// All publish paths are non-blocking by design — see the package-level
// observer-model doc in types.go. Subscribers are observers; they may
// drop frames they can't keep up with, but they cannot push back on
// the producer. Cadence enforcement, if needed, belongs upstream in an
// AudioPacingStage on the data path, not here.
//
// Lifecycle: NewAudioRouter starts the dispatch goroutine. Close stops it
// and closes all consumer channels. Close is idempotent. Publish is safe
// to call after Close — it falls through to a no-op rather than panicking
// on send-to-closed-channel.
type AudioRouter struct {
	rate int

	in chan Frame

	consumers map[string]chan Frame
	drops     map[string]*atomic.Int64
	mu        sync.RWMutex

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once

	// closed gates Publish from sending into r.in after Close has run.
	// Read under closeMu's RLock; written under closeMu's Lock together
	// with the actual r.in close. This prevents the send-to-closed
	// panic when a stage publishes during shutdown.
	closeMu sync.RWMutex
	closed  bool

	// intakeDrops counts frames dropped at the router's input channel
	// (as opposed to per-consumer drops, which are tracked separately
	// in r.drops). Sustained intake drops mean the dispatch goroutine
	// is keeping up but producers are bursting faster than the
	// in-buffer can hold; this is observable but not a hard error.
	intakeDrops atomic.Int64

	// drainHandlers is an opt-in registry of subscribers that physically
	// play audio in real time (LocalSink). The duplex executor's turn
	// loop calls WaitOutputDrained between turns so the next turn
	// doesn't start while the previous one is still audibly playing.
	//
	// This is NOT backpressure on the data path — handlers are invoked
	// by an external controller (the turn loop), never by the dispatch
	// goroutine. Subscribers that don't physically play audio (SSE,
	// level meter) skip registration.
	drainHandlersMu sync.Mutex
	drainHandlers   []DrainHandler
}

// DrainHandler is an optional callback a subscriber can register with
// AudioRouter.RegisterDrainHandler to participate in the inter-turn
// drain barrier. The handler should block until its internal queues
// are empty or the deadline implied by ctx elapses, then return.
type DrainHandler func(ctx context.Context)

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
	}
	r.wg.Add(1)
	go r.dispatchLoop()
	return r
}

// Subscribe registers a new consumer. Returns a receive-only channel.
// bufSize controls how many frames may queue before drop-on-overflow.
//
// A duplicate id is treated as an overwrite: the previously-registered
// channel is closed (so any goroutine reading it observes EOF, same as
// Unsubscribe) and replaced with the new one. A duplicate is logged
// because it usually indicates a bug at the call site.
//
// Subscribe-after-Close returns an already-closed channel rather than
// registering a new consumer that would never be serviced (the
// dispatch goroutine has exited, so any frames published after Close
// are dropped — see Publish). The caller's range loop on the returned
// channel terminates immediately, which is what they would observe if
// they had subscribed before Close and the router had since shut down.
func (r *AudioRouter) Subscribe(id string, bufSize int) <-chan Frame {
	r.closeMu.RLock()
	defer r.closeMu.RUnlock()
	if r.closed {
		ch := make(chan Frame)
		close(ch)
		return ch
	}

	ch := make(chan Frame, bufSize)
	r.mu.Lock()
	if existing, ok := r.consumers[id]; ok {
		logger.Warn("audio router: subscribe id collision; closing previous channel",
			"id", id)
		close(existing)
	}
	r.consumers[id] = ch
	r.drops[id] = &atomic.Int64{}
	r.mu.Unlock()
	return ch
}

// Done returns a channel that is closed when the router has shut down.
// Useful for downstream owners (e.g. audio.Monitor) to drop a router from
// their registry without polling, since per-run routers outlive the run
// only briefly.
func (r *AudioRouter) Done() <-chan struct{} {
	return r.ctx.Done()
}

// RegisterDrainHandler adds a callback that the turn loop can wait on
// between turns via WaitOutputDrained. Used by subscribers that
// physically play audio (LocalSink) so the next user turn doesn't
// start while the previous assistant audio is still audibly playing.
//
// Handlers run only when an external controller calls
// WaitOutputDrained. They never run on the dispatch goroutine and
// never affect Publish — the data path stays observer-style.
//
// Multiple handlers are supported (one router can drive several local
// playback paths in theory). They run concurrently; WaitOutputDrained
// returns when all have completed or the deadline elapses.
func (r *AudioRouter) RegisterDrainHandler(h DrainHandler) {
	if h == nil {
		return
	}
	r.drainHandlersMu.Lock()
	r.drainHandlers = append(r.drainHandlers, h)
	r.drainHandlersMu.Unlock()
}

// WaitOutputDrained blocks until every registered DrainHandler has
// returned, or until ctx is canceled. Each handler is invoked with the
// same ctx; they're expected to apply their own timeout if needed.
//
// Safe to call when no handlers are registered (returns immediately).
// Safe to call after Close — handlers are still tracked.
func (r *AudioRouter) WaitOutputDrained(ctx context.Context) {
	r.drainHandlersMu.Lock()
	handlers := make([]DrainHandler, len(r.drainHandlers))
	copy(handlers, r.drainHandlers)
	r.drainHandlersMu.Unlock()

	if len(handlers) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, h := range handlers {
		wg.Add(1)
		go func(h DrainHandler) {
			defer wg.Done()
			h(ctx)
		}(h)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		// Returning here orphans the handler goroutines — they'll
		// complete on their own once their internal timeout fires.
		// The intent of WaitOutputDrained is "best-effort, don't
		// block the run"; orphans are acceptable.
	}
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

// IntakeDropCount returns the cumulative number of frames dropped at
// the router's intake (i.e. r.in was full when Publish ran). Distinct
// from DropCount, which measures per-consumer drop. A non-zero value
// here means producers are bursting faster than the dispatch goroutine
// can fan out — observable, recoverable, not a hard error.
func (r *AudioRouter) IntakeDropCount() int64 {
	return r.intakeDrops.Load()
}

// Publish enqueues a frame for fan-out. Non-blocking by contract: drops
// the frame silently if the router input buffer is full or the router
// has been closed. The non-blocking behaviour is load-bearing — see the
// observer-model doc in types.go. Do not change this to block.
//
// Safe to call after Close: the closed flag is checked under a read
// lock that excludes the actual channel close, so we never reach a
// send to a closed channel and never panic.
func (r *AudioRouter) Publish(frame Frame) {
	r.closeMu.RLock()
	defer r.closeMu.RUnlock()
	if r.closed {
		return
	}
	select {
	case r.in <- frame:
	default:
		// Router input full; frame dropped at intake. Track and log
		// occasionally so a sustained-overflow situation is visible
		// without flooding the log.
		n := r.intakeDrops.Add(1)
		if n%intakeDropLogThrottle == 1 {
			logger.Warn("audio router: intake buffer full, dropping frames",
				"intake_drops_total", n)
		}
	}
}

// Close stops the router goroutine and closes all consumer channels.
// Safe to call multiple times.
//
// Order of operations is important for the no-panic guarantee on
// Publish: take the close write lock, set r.closed, only then close
// r.in. Once we hold the write lock, no Publish is in flight (each
// Publish takes the read lock for its full duration). After this
// returns, all subsequent Publish calls observe r.closed and skip
// the send.
func (r *AudioRouter) Close() {
	r.closeOnce.Do(func() {
		r.closeMu.Lock()
		r.closed = true
		close(r.in)
		r.closeMu.Unlock()

		r.cancel()
		r.wg.Wait()

		r.mu.Lock()
		for id, ch := range r.consumers {
			close(ch)
			delete(r.consumers, id)
		}
		r.mu.Unlock()
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

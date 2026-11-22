package stage

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
)

// pacedElem is an element waiting at a flush-on-cut pacer, tagged with the
// number of cuts read before it: audio read before a later cut belongs to
// the cut reply and is dropped. preempts counts the reply audio read before
// it: a preemptible cue's audio read before later reply audio gives way to it.
type pacedElem struct {
	elem     StreamElement
	cuts     uint64
	preempts uint64
}

// preemptible reports whether item is audio of a preemptible cue.
func (item pacedElem) preemptible() bool {
	c := item.elem.Meta.CueAudio
	return c != nil && c.Preemptible
}

// pacerQueue is the read-ahead queue between a flush-on-cut pacer's reader
// and its pacing loop.
type pacerQueue struct {
	mu    sync.Mutex
	items []pacedElem
	cuts  uint64
	// preempts counts the reply audio (audio not played for a cue) read.
	preempts uint64
	closed   bool
	notify   chan struct{}
	// stopSleep ends the pacing loop's current sleep; a cut calls it, and so
	// does reply audio when the sleep is for a preemptible cue's audio.
	stopSleep context.CancelFunc
	// sleepPreemptible is set while the sleep is for a preemptible cue's audio.
	sleepPreemptible bool
}

// read moves input into the queue until input closes or ctx ends. A cut
// ends the pacing loop's sleep, so the cut reply's audio stops at once.
func (q *pacerQueue) read(ctx context.Context, input <-chan StreamElement) {
	defer func() {
		q.mu.Lock()
		q.closed = true
		q.mu.Unlock()
		q.wake()
	}()
	for {
		select {
		case elem, ok := <-input:
			if !ok {
				return
			}
			q.add(elem)
		case <-ctx.Done():
			return
		}
	}
}

// add queues elem. A cut ends the pacing loop's sleep, and reply audio ends a
// sleep for a preemptible cue's audio.
func (q *pacerQueue) add(elem StreamElement) {
	q.mu.Lock()
	if core.IsCut(&elem) {
		q.cuts++
		if q.stopSleep != nil {
			q.stopSleep()
		}
	} else if pacesAudio(&elem) && elem.Meta.CueAudio == nil {
		q.preempts++
		if q.stopSleep != nil && q.sleepPreemptible {
			q.stopSleep()
		}
	}
	q.items = append(q.items, pacedElem{elem: elem, cuts: q.cuts, preempts: q.preempts})
	q.mu.Unlock()
	q.wake()
}

// wake tells the pacing loop the queue changed.
func (q *pacerQueue) wake() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// next waits for the next queued element; ok is false once the input has
// closed and the queue is empty.
func (q *pacerQueue) next(ctx context.Context) (item pacedElem, ok bool, err error) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			item = q.items[0]
			q.items[0] = pacedElem{}
			q.items = q.items[1:]
			q.mu.Unlock()
			return item, true, nil
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return pacedElem{}, false, nil
		}
		select {
		case <-q.notify:
		case <-ctx.Done():
			return pacedElem{}, false, ctx.Err()
		}
	}
}

// stale reports whether a cut was read after item, making its audio part of
// a cut reply, or, for a preemptible cue's audio, reply audio was.
func (q *pacerQueue) stale(item pacedElem) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.staleLocked(item)
}

func (q *pacerQueue) staleLocked(item pacedElem) bool {
	return q.cuts != item.cuts || (item.preemptible() && q.preempts != item.preempts)
}

// sleepContext returns the context the pacing loop sleeps on for item: a
// cut read during the sleep ends it, as reply audio does for a preemptible
// cue's audio. ok is false when item is already stale.
func (q *pacerQueue) sleepContext(ctx context.Context, item pacedElem) (context.Context, func(), bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.staleLocked(item) {
		return nil, nil, false
	}
	sctx, cancel := context.WithCancel(ctx)
	q.stopSleep = cancel
	q.sleepPreemptible = item.preemptible()
	return sctx, func() {
		q.mu.Lock()
		q.stopSleep = nil
		q.sleepPreemptible = false
		q.mu.Unlock()
		cancel()
	}, true
}

// processFlushOnCut is Process for an output pacer that honors cuts (see
// WithFlushOnCut): a reader moves the input into a queue as fast as it
// arrives, and the pacing loop forwards it at playback speed, dropping the
// audio of a reply a later cut ended.
func (s *AudioPacingStage) processFlushOnCut(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)
	q := &pacerQueue{notify: make(chan struct{}, 1)}
	readCtx, stopReading := context.WithCancel(ctx)
	defer stopReading()
	go q.read(readCtx, input)

	for {
		item, ok, err := q.next(ctx)
		if err != nil || !ok {
			return err
		}
		if err := s.forwardQueued(ctx, q, item, output); err != nil {
			return err
		}
	}
}

// forwardQueued forwards one queued element: a cut at once, audio when it is
// due, anything else at once. Audio a cut dropped is stripped from its
// element, which still goes on when it carries anything else (a transcript,
// the reply's end). A cut or EndOfStream starts a new audio sequence, as does
// a preemptible cue's audio dropped for the reply audio that follows it.
func (s *AudioPacingStage) forwardQueued(
	ctx context.Context, q *pacerQueue, item pacedElem, output chan<- StreamElement,
) error {
	elem := item.elem
	if !core.IsCut(&elem) && pacesAudio(&elem) {
		played, err := s.paceQueued(ctx, q, item)
		if err != nil {
			return err
		}
		if !played {
			if item.preemptible() {
				// The reply audio that preempted it starts its own sequence.
				s.resetSequence()
			}
			elem.Audio = nil
			if onlyAudio(elem) {
				return nil
			}
		}
	}
	if core.IsCut(&elem) || elem.EndOfStream {
		s.resetSequence()
	}
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// onlyAudio reports whether elem, its audio stripped, carries nothing but
// bookkeeping: dropping it loses nothing.
func onlyAudio(elem StreamElement) bool {
	elem.Sequence = 0
	elem.Timestamp = time.Time{}
	elem.Source = ""
	elem.Priority = 0
	elem.Meta.TurnID = nil
	elem.Meta.StreamingDelta = false
	elem.Meta.CueAudio = nil
	return reflect.ValueOf(elem).IsZero()
}

// paceQueued waits until item's audio is due. It reports false when a cut
// read before or during the wait dropped it.
func (s *AudioPacingStage) paceQueued(ctx context.Context, q *pacerQueue, item pacedElem) (bool, error) {
	sctx, done, ok := q.sleepContext(ctx, item)
	if !ok {
		return false, nil
	}
	err := s.paceFor(sctx, &item.elem)
	done()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if q.stale(item) {
		return false, nil
	}
	return true, err
}

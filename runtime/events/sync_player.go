// Package events provides event storage and replay for session recording.
package events

import (
	"context"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
)

// Playback constants.
const (
	defaultAudioBufferSize = 4096
	audioTickInterval      = 20 * time.Millisecond
)

// AudioHandler is called with audio data during synchronized playback.
// Returns false to stop playback.
type AudioHandler func(data []byte, track TrackType, position time.Duration) bool

// AnnotationHandler is called when an annotation becomes active.
type AnnotationHandler func(annotation *annotations.Annotation, position time.Duration)

// SyncPlayerConfig configures synchronized playback behavior.
type SyncPlayerConfig struct {
	// Speed is the playback speed multiplier (1.0 = real-time).
	Speed float64

	// OnEvent is called for each event during replay.
	OnEvent PlayerCallback

	// OnAudio is called with audio data chunks during playback.
	// The handler receives raw PCM data that can be played through speakers.
	OnAudio AudioHandler

	// OnAnnotation is called when an annotation becomes active.
	OnAnnotation AnnotationHandler

	// OnStateChange is called when the player state changes.
	OnStateChange func(state PlayerState)

	// OnComplete is called when playback reaches the end.
	OnComplete func()

	// OnError is called when an error occurs during playback.
	OnError func(err error)

	// AudioBufferSize is the size of audio chunks delivered to OnAudio.
	// Default: 4096 bytes
	AudioBufferSize int

	// SkipTiming when true, delivers all events immediately without timing delays.
	SkipTiming bool
}

// DefaultSyncPlayerConfig returns sensible defaults for synchronized playback.
func DefaultSyncPlayerConfig() *SyncPlayerConfig {
	return &SyncPlayerConfig{
		Speed:           1.0,
		AudioBufferSize: defaultAudioBufferSize,
	}
}

// SyncPlayer provides synchronized playback of events, audio, and annotations.
type SyncPlayer struct {
	timeline    *MediaTimeline
	annotations []*annotations.Annotation
	config      *SyncPlayerConfig

	mu           sync.RWMutex
	state        PlayerState
	position     time.Duration // Current playback position
	eventIndex   int           // Current event index
	annotIndex   int           // Current annotation index
	startTime    time.Time     // When playback started
	pauseTime    time.Time     // When playback was paused
	audioReaders map[TrackType]*TrackReader

	ctx    context.Context    //nolint:containedctx // context stored for lifecycle management
	cancel context.CancelFunc //nolint:containedctx // lifecycle management
	done   chan struct{}
}

// NewSyncPlayer creates a new synchronized player.
func NewSyncPlayer(
	timeline *MediaTimeline,
	annots []*annotations.Annotation,
	config *SyncPlayerConfig,
) *SyncPlayer {
	if config == nil {
		config = DefaultSyncPlayerConfig()
	}
	if config.Speed <= 0 {
		config.Speed = 1.0
	}
	if config.AudioBufferSize <= 0 {
		config.AudioBufferSize = 4096
	}

	return &SyncPlayer{
		timeline:     timeline,
		annotations:  sortAnnotationsByTime(annots, timeline.SessionStart),
		config:       config,
		state:        PlayerStateStopped,
		audioReaders: make(map[TrackType]*TrackReader),
	}
}

// sortAnnotationsByTime sorts annotations by their target time.
func sortAnnotationsByTime(annots []*annotations.Annotation, sessionStart time.Time) []*annotations.Annotation {
	sorted := make([]*annotations.Annotation, len(annots))
	copy(sorted, annots)

	// Calculate effective time for each annotation
	getTime := func(a *annotations.Annotation) time.Duration {
		//nolint:exhaustive // Only handling time-based target types
		switch a.Target.Type {
		case annotations.TargetTimeRange:
			return a.Target.StartTime.Sub(sessionStart)
		case annotations.TargetEvent:
			// For event targets, use the event sequence as a proxy for time
			return time.Duration(a.Target.EventSequence) * time.Millisecond
		default:
			return 0
		}
	}

	// Sort by effective time
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if getTime(sorted[i]) > getTime(sorted[j]) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// State returns the current player state.
func (p *SyncPlayer) State() PlayerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// Position returns the current playback position.
func (p *SyncPlayer) Position() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.position
}

// Duration returns the total session duration.
func (p *SyncPlayer) Duration() time.Duration {
	return p.timeline.Duration()
}

// Play starts or resumes playback.
func (p *SyncPlayer) Play(ctx context.Context) error {
	p.mu.Lock()
	if p.state == PlayerStatePlaying {
		p.mu.Unlock()
		return nil
	}

	// Initialize audio readers
	if err := p.initAudioReaders(); err != nil {
		p.mu.Unlock()
		return err
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.done = make(chan struct{})
	p.state = PlayerStatePlaying
	p.startTime = time.Now()

	// If resuming from pause, adjust start time
	if !p.pauseTime.IsZero() && p.position > 0 {
		p.startTime = time.Now().Add(-time.Duration(float64(p.position) / p.config.Speed))
	}

	p.mu.Unlock()
	p.notifyStateChange(PlayerStatePlaying)

	go p.playbackLoop()
	return nil
}

// initAudioReaders initializes audio track readers.
func (p *SyncPlayer) initAudioReaders() error {
	for trackType := range p.timeline.Tracks {
		if trackType == TrackAudioInput || trackType == TrackAudioOutput {
			reader, err := p.timeline.NewTrackReader(trackType)
			if err != nil {
				return err
			}
			p.audioReaders[trackType] = reader
		}
	}
	return nil
}

// Pause pauses playback.
func (p *SyncPlayer) Pause() {
	p.mu.Lock()
	if p.state != PlayerStatePlaying {
		p.mu.Unlock()
		return
	}
	p.state = PlayerStatePaused
	p.pauseTime = time.Now()
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()
	p.notifyStateChange(PlayerStatePaused)
}

// Stop stops playback and resets position.
func (p *SyncPlayer) Stop() {
	p.mu.Lock()
	if p.state == PlayerStateStopped {
		p.mu.Unlock()
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.state = PlayerStateStopped
	p.position = 0
	p.eventIndex = 0
	p.annotIndex = 0
	p.pauseTime = time.Time{}
	p.closeAudioReaders()
	p.mu.Unlock()
	p.notifyStateChange(PlayerStateStopped)
}

// closeAudioReaders closes all audio readers.
func (p *SyncPlayer) closeAudioReaders() {
	for _, reader := range p.audioReaders {
		_ = reader.Close() // Error ignored during cleanup
	}
	p.audioReaders = make(map[TrackType]*TrackReader)
}

// SetSpeed changes the playback speed.
func (p *SyncPlayer) SetSpeed(speed float64) {
	if speed <= 0 {
		speed = 1.0
	}
	p.mu.Lock()
	oldSpeed := p.config.Speed
	p.config.Speed = speed

	// Adjust start time to maintain position when speed changes during playback
	if p.state == PlayerStatePlaying {
		realElapsed := time.Duration(float64(p.position) / oldSpeed)
		newRealElapsed := time.Duration(float64(p.position) / speed)
		p.startTime = p.startTime.Add(realElapsed - newRealElapsed)
	}
	p.mu.Unlock()
}

// Seek jumps to a specific position in the session.
func (p *SyncPlayer) Seek(position time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if position < 0 {
		position = 0
	}
	if position > p.timeline.Duration() {
		position = p.timeline.Duration()
	}

	p.position = position

	// Find the event at this position
	p.eventIndex = p.findEventAtPosition(position)

	// Find the annotation at this position
	p.annotIndex = p.findAnnotationAtPosition(position)

	// Seek audio readers
	for _, reader := range p.audioReaders {
		if err := reader.Seek(position); err != nil {
			return err
		}
	}

	// Adjust start time if playing
	if p.state == PlayerStatePlaying {
		p.startTime = time.Now().Add(-time.Duration(float64(position) / p.config.Speed))
	}

	return nil
}

// findEventAtPosition finds the event index at the given position.
func (p *SyncPlayer) findEventAtPosition(position time.Duration) int {
	targetTime := p.timeline.SessionStart.Add(position)
	for i, event := range p.timeline.Events {
		if event.Timestamp.After(targetTime) {
			if i > 0 {
				return i - 1
			}
			return 0
		}
	}
	return len(p.timeline.Events)
}

// findAnnotationAtPosition finds the annotation index at the given position.
func (p *SyncPlayer) findAnnotationAtPosition(position time.Duration) int {
	for i, annot := range p.annotations {
		annotTime := p.getAnnotationTime(annot)
		if annotTime > position {
			return i
		}
	}
	return len(p.annotations)
}

// getAnnotationTime returns the activation time for an annotation.
func (p *SyncPlayer) getAnnotationTime(annot *annotations.Annotation) time.Duration {
	//nolint:exhaustive // Only handling time-based target types
	switch annot.Target.Type {
	case annotations.TargetTimeRange:
		return annot.Target.StartTime.Sub(p.timeline.SessionStart)
	case annotations.TargetEvent:
		if annot.Target.EventSequence > 0 && int(annot.Target.EventSequence) < len(p.timeline.Events) {
			event := p.timeline.Events[annot.Target.EventSequence]
			return event.Timestamp.Sub(p.timeline.SessionStart)
		}
	}
	return 0
}

// playbackLoop runs the main synchronized playback loop.
func (p *SyncPlayer) playbackLoop() {
	defer close(p.done)

	// If skipping timing, deliver everything immediately
	p.mu.RLock()
	skipTiming := p.config.SkipTiming
	p.mu.RUnlock()

	if skipTiming {
		p.deliverAllImmediately()
		return
	}

	// Create ticker for audio delivery (e.g., every 20ms for smooth playback)
	audioTicker := time.NewTicker(audioTickInterval)
	defer audioTicker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return

		case <-audioTicker.C:
			p.mu.Lock()
			if p.state != PlayerStatePlaying {
				p.mu.Unlock()
				return
			}

			// Update position based on elapsed time
			elapsed := time.Since(p.startTime)
			p.position = time.Duration(float64(elapsed) * p.config.Speed)

			// Check if we've reached the end
			if p.position >= p.timeline.Duration() {
				p.mu.Unlock()
				p.onComplete()
				return
			}

			// Deliver events up to current position
			p.deliverEvents()

			// Deliver annotations up to current position
			p.deliverAnnotations()

			// Deliver audio
			p.deliverAudio()

			p.mu.Unlock()
		}
	}
}

// deliverAllImmediately delivers all events and annotations without timing delays.
func (p *SyncPlayer) deliverAllImmediately() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Deliver all events
	for p.eventIndex < len(p.timeline.Events) {
		event := p.timeline.Events[p.eventIndex]
		eventTime := event.Timestamp.Sub(p.timeline.SessionStart)
		p.position = eventTime

		if p.config.OnEvent != nil {
			if !p.config.OnEvent(event, eventTime) {
				p.onComplete()
				return
			}
		}
		p.eventIndex++
	}

	// Deliver all annotations
	for p.annotIndex < len(p.annotations) {
		annot := p.annotations[p.annotIndex]
		annotTime := p.getAnnotationTime(annot)

		if p.config.OnAnnotation != nil {
			p.config.OnAnnotation(annot, annotTime)
		}
		p.annotIndex++
	}

	p.position = p.timeline.Duration()
	p.onCompleteNoLock()
}

// onCompleteNoLock handles playback completion without locking (caller must hold lock).
func (p *SyncPlayer) onCompleteNoLock() {
	p.state = PlayerStateStopped
	p.closeAudioReaders()

	// Notify outside of lock
	go func() {
		p.notifyStateChange(PlayerStateStopped)
		if p.config.OnComplete != nil {
			p.config.OnComplete()
		}
	}()
}

// deliverEvents delivers all events up to the current position.
func (p *SyncPlayer) deliverEvents() {
	if p.config.OnEvent == nil {
		return
	}

	for p.eventIndex < len(p.timeline.Events) {
		event := p.timeline.Events[p.eventIndex]
		eventTime := event.Timestamp.Sub(p.timeline.SessionStart)

		if eventTime > p.position {
			break
		}

		if !p.config.OnEvent(event, eventTime) {
			return
		}
		p.eventIndex++
	}
}

// deliverAnnotations delivers all annotations up to the current position.
func (p *SyncPlayer) deliverAnnotations() {
	if p.config.OnAnnotation == nil {
		return
	}

	for p.annotIndex < len(p.annotations) {
		annot := p.annotations[p.annotIndex]
		annotTime := p.getAnnotationTime(annot)

		if annotTime > p.position {
			break
		}

		p.config.OnAnnotation(annot, annotTime)
		p.annotIndex++
	}
}

// deliverAudio reads and delivers audio data for the current position.
func (p *SyncPlayer) deliverAudio() {
	if p.config.OnAudio == nil {
		return
	}

	buf := make([]byte, p.config.AudioBufferSize)

	for trackType, reader := range p.audioReaders {
		n, err := reader.Read(buf)
		if err != nil {
			continue // EOF or error
		}
		if n > 0 {
			if !p.config.OnAudio(buf[:n], trackType, p.position) {
				return
			}
		}
	}
}

// onComplete handles playback completion.
func (p *SyncPlayer) onComplete() {
	p.mu.Lock()
	p.state = PlayerStateStopped
	p.closeAudioReaders()
	p.mu.Unlock()
	p.notifyStateChange(PlayerStateStopped)

	if p.config.OnComplete != nil {
		p.config.OnComplete()
	}
}

// notifyStateChange calls the state change callback if configured.
func (p *SyncPlayer) notifyStateChange(state PlayerState) {
	if p.config.OnStateChange != nil {
		p.config.OnStateChange(state)
	}
}

// Wait blocks until playback completes or is stopped.
func (p *SyncPlayer) Wait() {
	p.mu.RLock()
	done := p.done
	p.mu.RUnlock()
	if done != nil {
		<-done
	}
}

// Timeline returns the media timeline.
func (p *SyncPlayer) Timeline() *MediaTimeline {
	return p.timeline
}

// Annotations returns all annotations.
func (p *SyncPlayer) Annotations() []*annotations.Annotation {
	return p.annotations
}

// EventCount returns the number of events.
func (p *SyncPlayer) EventCount() int {
	return len(p.timeline.Events)
}

// AnnotationCount returns the number of annotations.
func (p *SyncPlayer) AnnotationCount() int {
	return len(p.annotations)
}

// GetEventsInRange returns events within the specified time range.
func (p *SyncPlayer) GetEventsInRange(start, end time.Duration) []*Event {
	var events []*Event
	startTime := p.timeline.SessionStart.Add(start)
	endTime := p.timeline.SessionStart.Add(end)

	for _, event := range p.timeline.Events {
		if event.Timestamp.After(startTime) && event.Timestamp.Before(endTime) {
			events = append(events, event)
		}
	}
	return events
}

// GetAnnotationsInRange returns annotations active within the specified time range.
func (p *SyncPlayer) GetAnnotationsInRange(start, end time.Duration) []*annotations.Annotation {
	var annots []*annotations.Annotation

	for _, annot := range p.annotations {
		annotTime := p.getAnnotationTime(annot)
		if annotTime >= start && annotTime <= end {
			annots = append(annots, annot)
		}
	}
	return annots
}

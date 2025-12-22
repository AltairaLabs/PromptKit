// Package events provides event storage and replay for session recording.
package events

import (
	"context"
	"sync"
	"time"
)

// PlayerState represents the current state of the session player.
type PlayerState int

const (
	// PlayerStateStopped indicates the player is stopped.
	PlayerStateStopped PlayerState = iota
	// PlayerStatePlaying indicates the player is actively replaying events.
	PlayerStatePlaying
	// PlayerStatePaused indicates the player is paused.
	PlayerStatePaused
)

// minEventsForDuration is the minimum number of events needed to calculate duration.
const minEventsForDuration = 2

// PlayerCallback is called for each event during replay.
// Return false to stop playback.
type PlayerCallback func(event *Event, position time.Duration) bool

// PlayerConfig configures session replay behavior.
type PlayerConfig struct {
	// Speed is the playback speed multiplier (1.0 = real-time, 2.0 = 2x speed, 0.5 = half speed).
	// Default: 1.0
	Speed float64

	// OnEvent is called for each event during replay.
	// If nil, events are still played but not observed.
	OnEvent PlayerCallback

	// OnStateChange is called when the player state changes.
	OnStateChange func(state PlayerState)

	// OnComplete is called when playback reaches the end.
	OnComplete func()

	// OnError is called when an error occurs during playback.
	OnError func(err error)

	// SkipTiming when true, delivers all events immediately without timing delays.
	// Useful for fast-forward or event analysis.
	SkipTiming bool
}

// DefaultPlayerConfig returns sensible defaults for playback.
func DefaultPlayerConfig() *PlayerConfig {
	return &PlayerConfig{
		Speed: 1.0,
	}
}

// SessionPlayer replays recorded session events with timing control.
type SessionPlayer struct {
	store     EventStore
	sessionID string
	config    *PlayerConfig

	mu        sync.RWMutex
	state     PlayerState
	events    []*Event
	position  int       // Current event index
	startTime time.Time // When playback started (for timing calculations)
	pauseTime time.Time // When playback was paused

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// NewSessionPlayer creates a new player for replaying a recorded session.
func NewSessionPlayer(store EventStore, sessionID string, config *PlayerConfig) *SessionPlayer {
	if config == nil {
		config = DefaultPlayerConfig()
	}
	if config.Speed <= 0 {
		config.Speed = 1.0
	}

	return &SessionPlayer{
		store:     store,
		sessionID: sessionID,
		config:    config,
		state:     PlayerStateStopped,
	}
}

// Load loads all events for the session into memory.
// Must be called before Play.
func (p *SessionPlayer) Load(ctx context.Context) error {
	events, err := p.store.Query(ctx, &EventFilter{SessionID: p.sessionID})
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.events = events
	p.position = 0
	p.mu.Unlock()
	return nil
}

// Events returns all loaded events.
func (p *SessionPlayer) Events() []*Event {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.events
}

// EventCount returns the number of loaded events.
func (p *SessionPlayer) EventCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.events)
}

// State returns the current player state.
func (p *SessionPlayer) State() PlayerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// Position returns the current event index.
func (p *SessionPlayer) Position() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.position
}

// Duration returns the total duration of the session.
func (p *SessionPlayer) Duration() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.events) < minEventsForDuration {
		return 0
	}
	return p.events[len(p.events)-1].Timestamp.Sub(p.events[0].Timestamp)
}

// CurrentTime returns the elapsed playback time from the session start.
func (p *SessionPlayer) CurrentTime() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.events) == 0 || p.position >= len(p.events) {
		return p.Duration()
	}
	if p.position == 0 {
		return 0
	}
	return p.events[p.position].Timestamp.Sub(p.events[0].Timestamp)
}

// Play starts or resumes playback.
func (p *SessionPlayer) Play(ctx context.Context) {
	p.mu.Lock()
	if p.state == PlayerStatePlaying {
		p.mu.Unlock()
		return
	}
	if len(p.events) == 0 {
		p.mu.Unlock()
		return
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.done = make(chan struct{})
	p.state = PlayerStatePlaying
	p.startTime = time.Now()

	// If resuming from pause, adjust start time
	if !p.pauseTime.IsZero() && p.position > 0 {
		elapsed := p.events[p.position].Timestamp.Sub(p.events[0].Timestamp)
		p.startTime = time.Now().Add(-time.Duration(float64(elapsed) / p.config.Speed))
	}

	p.mu.Unlock()
	p.notifyStateChange(PlayerStatePlaying)

	go p.playbackLoop()
}

// Pause pauses playback.
func (p *SessionPlayer) Pause() {
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
func (p *SessionPlayer) Stop() {
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
	p.pauseTime = time.Time{}
	p.mu.Unlock()
	p.notifyStateChange(PlayerStateStopped)
}

// SetSpeed changes the playback speed.
func (p *SessionPlayer) SetSpeed(speed float64) {
	if speed <= 0 {
		speed = 1.0
	}
	p.mu.Lock()
	oldSpeed := p.config.Speed
	p.config.Speed = speed

	// Adjust start time to maintain position when speed changes during playback
	if p.state == PlayerStatePlaying && p.position > 0 {
		elapsed := p.events[p.position].Timestamp.Sub(p.events[0].Timestamp)
		realElapsed := time.Duration(float64(elapsed) / oldSpeed)
		newRealElapsed := time.Duration(float64(elapsed) / speed)
		p.startTime = p.startTime.Add(realElapsed - newRealElapsed)
	}
	p.mu.Unlock()
}

// Seek jumps to a specific position in the session.
// The position is specified as a duration from the session start.
func (p *SessionPlayer) Seek(position time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.events) == 0 {
		return
	}

	sessionStart := p.events[0].Timestamp
	targetTime := sessionStart.Add(position)

	// Find the event closest to the target time
	for i, event := range p.events {
		if event.Timestamp.After(targetTime) || event.Timestamp.Equal(targetTime) {
			p.position = i
			if p.state == PlayerStatePlaying {
				p.startTime = time.Now().Add(-time.Duration(float64(position) / p.config.Speed))
			}
			return
		}
	}

	// Past the end
	p.position = len(p.events)
}

// SeekToEvent jumps to a specific event index.
func (p *SessionPlayer) SeekToEvent(index int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if index < 0 {
		index = 0
	}
	if index >= len(p.events) {
		index = len(p.events)
	}
	p.position = index

	if p.state == PlayerStatePlaying && index < len(p.events) {
		elapsed := p.events[index].Timestamp.Sub(p.events[0].Timestamp)
		p.startTime = time.Now().Add(-time.Duration(float64(elapsed) / p.config.Speed))
	}
}

// playbackEventInfo holds information about the current event being played.
type playbackEventInfo struct {
	event       *Event
	eventOffset time.Duration
	speed       float64
	skipTiming  bool
	position    int
}

// getPlaybackEventInfo returns info about the current event, or nil if at end.
func (p *SessionPlayer) getPlaybackEventInfo() *playbackEventInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.position >= len(p.events) {
		return nil
	}

	event := p.events[p.position]
	sessionStart := p.events[0].Timestamp
	return &playbackEventInfo{
		event:       event,
		eventOffset: event.Timestamp.Sub(sessionStart),
		speed:       p.config.Speed,
		skipTiming:  p.config.SkipTiming,
		position:    p.position,
	}
}

// waitForEventTiming waits until the event should be delivered based on timing.
// Returns false if context was canceled.
func (p *SessionPlayer) waitForEventTiming(info *playbackEventInfo) bool {
	if info.skipTiming || info.eventOffset <= 0 {
		return true
	}

	scaledOffset := time.Duration(float64(info.eventOffset) / info.speed)
	p.mu.RLock()
	targetTime := p.startTime.Add(scaledOffset)
	ctx := p.ctx
	p.mu.RUnlock()

	waitDuration := time.Until(targetTime)
	if waitDuration <= 0 {
		return true
	}

	select {
	case <-ctx.Done():
		return false
	case <-time.After(waitDuration):
		return true
	}
}

// deliverEvent delivers an event to the callback.
// Returns false if playback should stop.
func (p *SessionPlayer) deliverEvent(info *playbackEventInfo) bool {
	p.mu.RLock()
	callback := p.config.OnEvent
	p.mu.RUnlock()

	if callback == nil {
		return true
	}
	return callback(info.event, info.eventOffset)
}

// advancePosition moves to the next event if position hasn't changed.
func (p *SessionPlayer) advancePosition(expectedPos int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.position == expectedPos {
		p.position++
	}
}

// playbackLoop runs the main playback loop.
func (p *SessionPlayer) playbackLoop() {
	defer close(p.done)

	for {
		info := p.getPlaybackEventInfo()
		if info == nil {
			p.onComplete()
			return
		}

		if !p.waitForEventTiming(info) {
			return
		}

		// Check if we should stop
		p.mu.RLock()
		ctx := p.ctx
		p.mu.RUnlock()
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !p.deliverEvent(info) {
			p.Stop()
			return
		}

		p.advancePosition(info.position)
	}
}

// onComplete handles playback completion.
func (p *SessionPlayer) onComplete() {
	p.mu.Lock()
	p.state = PlayerStateStopped
	p.mu.Unlock()
	p.notifyStateChange(PlayerStateStopped)

	if p.config.OnComplete != nil {
		p.config.OnComplete()
	}
}

// notifyStateChange calls the state change callback if configured.
func (p *SessionPlayer) notifyStateChange(state PlayerState) {
	if p.config.OnStateChange != nil {
		p.config.OnStateChange(state)
	}
}

// Wait blocks until playback completes or is stopped.
func (p *SessionPlayer) Wait() {
	p.mu.RLock()
	done := p.done
	p.mu.RUnlock()
	if done != nil {
		<-done
	}
}

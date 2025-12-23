// Package recording provides session recording export, import, and replay.
package recording

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// Playback timing constants.
const (
	eventTolerance    = 50 * time.Millisecond
	recentEventWindow = 2 * time.Second
	secondsPerMinute  = 60
	millisPerSecond   = 1000
)

// ReplayPlayer provides synchronized playback of session recordings with event correlation.
// It allows seeking to any position and retrieving events/annotations at that time.
type ReplayPlayer struct {
	recording   *SessionRecording
	timeline    *events.MediaTimeline
	annotations []*annotations.Annotation

	// Current playback position (offset from session start)
	position time.Duration

	// Event indices for efficient lookup
	eventsByTime []indexedEvent
}

// indexedEvent holds an event with its offset for binary search.
type indexedEvent struct {
	Offset time.Duration
	Index  int
}

// NewReplayPlayer creates a new replay player for the given recording.
func NewReplayPlayer(rec *SessionRecording) (*ReplayPlayer, error) {
	timeline, err := rec.ToMediaTimeline(nil)
	if err != nil {
		return nil, fmt.Errorf("create timeline: %w", err)
	}

	rp := &ReplayPlayer{
		recording: rec,
		timeline:  timeline,
	}

	// Build time index for events
	rp.eventsByTime = make([]indexedEvent, len(rec.Events))
	for i := range rec.Events {
		rp.eventsByTime[i] = indexedEvent{
			Offset: rec.Events[i].Offset,
			Index:  i,
		}
	}
	// Ensure sorted by offset
	sort.Slice(rp.eventsByTime, func(i, j int) bool {
		return rp.eventsByTime[i].Offset < rp.eventsByTime[j].Offset
	})

	return rp, nil
}

// SetAnnotations adds annotations for correlation during playback.
func (rp *ReplayPlayer) SetAnnotations(anns []*annotations.Annotation) {
	rp.annotations = anns
}

// Seek moves the playback position to the specified offset from session start.
func (rp *ReplayPlayer) Seek(offset time.Duration) {
	if offset < 0 {
		offset = 0
	}
	if offset > rp.recording.Metadata.Duration {
		offset = rp.recording.Metadata.Duration
	}
	rp.position = offset
}

// Position returns the current playback position.
func (rp *ReplayPlayer) Position() time.Duration {
	return rp.position
}

// Duration returns the total recording duration.
func (rp *ReplayPlayer) Duration() time.Duration {
	return rp.recording.Metadata.Duration
}

// Timeline returns the media timeline for audio access.
func (rp *ReplayPlayer) Timeline() *events.MediaTimeline {
	return rp.timeline
}

// Recording returns the underlying session recording.
func (rp *ReplayPlayer) Recording() *SessionRecording {
	return rp.recording
}

// PlaybackState represents the state at a given playback position.
type PlaybackState struct {
	// Position is the current offset from session start.
	Position time.Duration

	// Timestamp is the absolute timestamp at this position.
	Timestamp time.Time

	// CurrentEvents are events occurring at exactly this position (within tolerance).
	CurrentEvents []RecordedEvent

	// RecentEvents are events that occurred in the last `window` duration.
	RecentEvents []RecordedEvent

	// ActiveAnnotations are annotations whose time range includes this position.
	ActiveAnnotations []*annotations.Annotation

	// Messages accumulated up to this point.
	Messages []MessageSnapshot

	// AudioInputActive indicates if user audio is present at this position.
	AudioInputActive bool

	// AudioOutputActive indicates if assistant audio is present at this position.
	AudioOutputActive bool
}

// MessageSnapshot represents a message at a point in time.
type MessageSnapshot struct {
	Role      string
	Content   string
	Timestamp time.Time
	Offset    time.Duration
}

// GetState returns the playback state at the current position.
func (rp *ReplayPlayer) GetState() *PlaybackState {
	return rp.GetStateAt(rp.position)
}

// GetStateAt returns the playback state at the specified offset.
func (rp *ReplayPlayer) GetStateAt(offset time.Duration) *PlaybackState {
	state := &PlaybackState{
		Position:  offset,
		Timestamp: rp.recording.Metadata.StartTime.Add(offset),
	}

	// Find events at this position (within tolerance)
	for _, ie := range rp.eventsByTime {
		diff := ie.Offset - offset
		if diff < 0 {
			diff = -diff
		}
		if diff <= eventTolerance {
			state.CurrentEvents = append(state.CurrentEvents, rp.recording.Events[ie.Index])
		}
	}

	// Find recent events (within recent window)
	for _, ie := range rp.eventsByTime {
		if ie.Offset <= offset && ie.Offset > offset-recentEventWindow {
			state.RecentEvents = append(state.RecentEvents, rp.recording.Events[ie.Index])
		}
	}

	// Find active annotations (those whose time range includes this position)
	absTime := rp.recording.Metadata.StartTime.Add(offset)
	for _, ann := range rp.annotations {
		if rp.annotationActiveAt(ann, absTime, offset) {
			state.ActiveAnnotations = append(state.ActiveAnnotations, ann)
		}
	}

	// Accumulate messages up to this point
	state.Messages = rp.getMessagesUpTo(offset)

	// Check audio activity
	state.AudioInputActive = rp.isAudioActiveAt(events.TrackAudioInput, offset)
	state.AudioOutputActive = rp.isAudioActiveAt(events.TrackAudioOutput, offset)

	return state
}

// annotationActiveAt checks if an annotation is active at the given time.
func (rp *ReplayPlayer) annotationActiveAt(ann *annotations.Annotation, absTime time.Time, offset time.Duration) bool {
	switch ann.Target.Type {
	case annotations.TargetSession:
		return true
	case annotations.TargetTimeRange:
		return !absTime.Before(ann.Target.StartTime) && !absTime.After(ann.Target.EndTime)
	case annotations.TargetEvent:
		// Find the target event and check if we're past it
		for _, ie := range rp.eventsByTime {
			if rp.recording.Events[ie.Index].Sequence == ann.Target.EventSequence {
				return ie.Offset <= offset
			}
		}
	case annotations.TargetTurn, annotations.TargetMessage:
		// Turn and message targets are always considered active once reached
		// (would need additional turn/message tracking to be more precise)
		return true
	}
	return false
}

// getMessagesUpTo returns all messages accumulated up to the given offset.
func (rp *ReplayPlayer) getMessagesUpTo(offset time.Duration) []MessageSnapshot {
	var messages []MessageSnapshot

	for _, ie := range rp.eventsByTime {
		if ie.Offset > offset {
			break
		}

		event := rp.recording.Events[ie.Index]
		if event.Type != events.EventMessageCreated {
			continue
		}

		// Try to extract message content
		var msgData events.MessageCreatedData
		if unmarshalEventData(event.Data, &msgData) == nil {
			messages = append(messages, MessageSnapshot{
				Role:      msgData.Role,
				Content:   msgData.Content,
				Timestamp: event.Timestamp,
				Offset:    event.Offset,
			})
		}
	}

	return messages
}

// isAudioActiveAt checks if the specified audio track has data at the given offset.
func (rp *ReplayPlayer) isAudioActiveAt(trackType events.TrackType, offset time.Duration) bool {
	track := rp.timeline.GetTrack(trackType)
	if track == nil {
		return false
	}

	seg, _ := track.OffsetInSegment(offset)
	return seg != nil
}

// GetEventsInRange returns all events within the specified time range.
func (rp *ReplayPlayer) GetEventsInRange(start, end time.Duration) []RecordedEvent {
	var result []RecordedEvent

	for _, ie := range rp.eventsByTime {
		if ie.Offset >= start && ie.Offset <= end {
			result = append(result, rp.recording.Events[ie.Index])
		}
		if ie.Offset > end {
			break
		}
	}

	return result
}

// GetEventsByType returns all events of the specified type.
func (rp *ReplayPlayer) GetEventsByType(eventType events.EventType) []RecordedEvent {
	var result []RecordedEvent

	for i := range rp.recording.Events {
		if rp.recording.Events[i].Type == eventType {
			result = append(result, rp.recording.Events[i])
		}
	}

	return result
}

// Advance moves the position forward by the specified duration and returns
// any events that occurred during that interval.
func (rp *ReplayPlayer) Advance(duration time.Duration) []RecordedEvent {
	start := rp.position
	end := start + duration
	if end > rp.recording.Metadata.Duration {
		end = rp.recording.Metadata.Duration
	}

	rangeEvents := rp.GetEventsInRange(start, end)
	rp.position = end

	return rangeEvents
}

// AdvanceTo moves to the specified position and returns events encountered.
func (rp *ReplayPlayer) AdvanceTo(target time.Duration) []RecordedEvent {
	if target <= rp.position {
		rp.position = target
		return nil
	}

	return rp.Advance(target - rp.position)
}

// FormatPosition returns a human-readable position string.
func (rp *ReplayPlayer) FormatPosition() string {
	total := rp.recording.Metadata.Duration
	pos := rp.position

	return fmt.Sprintf("%s / %s",
		formatDuration(pos),
		formatDuration(total))
}

// formatDuration formats a duration as MM:SS.mmm
func formatDuration(d time.Duration) string {
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % secondsPerMinute
	millis := int(d.Milliseconds()) % millisPerSecond

	return fmt.Sprintf("%02d:%02d.%03d", mins, secs, millis)
}

// EventIterator provides iteration over events in time order.
type EventIterator struct {
	player  *ReplayPlayer
	index   int
	endTime time.Duration
}

// NewEventIterator creates an iterator over events from start to end.
func (rp *ReplayPlayer) NewEventIterator(start, end time.Duration) *EventIterator {
	// Find starting index
	startIdx := sort.Search(len(rp.eventsByTime), func(i int) bool {
		return rp.eventsByTime[i].Offset >= start
	})

	return &EventIterator{
		player:  rp,
		index:   startIdx,
		endTime: end,
	}
}

// Next returns the next event and true, or false if no more events.
func (it *EventIterator) Next() (RecordedEvent, bool) {
	if it.index >= len(it.player.eventsByTime) {
		return RecordedEvent{}, false
	}

	ie := it.player.eventsByTime[it.index]
	if ie.Offset > it.endTime {
		return RecordedEvent{}, false
	}

	event := it.player.recording.Events[ie.Index]
	it.index++

	return event, true
}

// unmarshalEventData is a helper to unmarshal event data.
func unmarshalEventData(data []byte, target interface{}) error {
	if len(data) == 0 {
		return fmt.Errorf("empty data")
	}
	return json.Unmarshal(data, target)
}

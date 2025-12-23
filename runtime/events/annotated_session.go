// Package events provides event storage and replay for session recording.
package events

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/annotations"
)

// AnnotatedSession represents a complete session with events, media, and annotations.
// It provides a unified interface for loading and accessing all session data.
type AnnotatedSession struct {
	// SessionID is the unique session identifier.
	SessionID string

	// Events are all events in the session, sorted by timestamp.
	Events []*Event

	// Annotations are all annotations for this session.
	Annotations []*annotations.Annotation

	// Timeline is the assembled media timeline.
	Timeline *MediaTimeline

	// Metadata contains session-level metadata.
	Metadata SessionMetadata
}

// SessionMetadata contains high-level session information.
type SessionMetadata struct {
	// StartTime is when the session started.
	StartTime time.Time
	// EndTime is when the session ended.
	EndTime time.Time
	// Duration is the total session duration.
	Duration time.Duration

	// EventCounts by type.
	EventCounts map[EventType]int
	// AnnotationCounts by type.
	AnnotationCounts map[annotations.AnnotationType]int

	// HasAudioInput indicates if the session has audio input.
	HasAudioInput bool
	// HasAudioOutput indicates if the session has audio output.
	HasAudioOutput bool
	// HasVideo indicates if the session has video.
	HasVideo bool

	// TotalAudioInputDuration is the total duration of audio input.
	TotalAudioInputDuration time.Duration
	// TotalAudioOutputDuration is the total duration of audio output.
	TotalAudioOutputDuration time.Duration

	// ConversationTurns is the number of conversation turns.
	ConversationTurns int
	// ToolCalls is the number of tool calls.
	ToolCalls int
	// ProviderCalls is the number of provider calls.
	ProviderCalls int
}

// AnnotatedSessionLoader loads annotated sessions from storage.
type AnnotatedSessionLoader struct {
	eventStore  EventStore
	blobStore   BlobStore
	annotStore  annotations.Store
	computeMeta bool
}

// NewAnnotatedSessionLoader creates a new session loader.
func NewAnnotatedSessionLoader(
	eventStore EventStore,
	blobStore BlobStore,
	annotStore annotations.Store,
) *AnnotatedSessionLoader {
	return &AnnotatedSessionLoader{
		eventStore:  eventStore,
		blobStore:   blobStore,
		annotStore:  annotStore,
		computeMeta: true,
	}
}

// WithMetadata enables or disables metadata computation.
func (l *AnnotatedSessionLoader) WithMetadata(compute bool) *AnnotatedSessionLoader {
	l.computeMeta = compute
	return l
}

// Load loads a complete annotated session.
func (l *AnnotatedSessionLoader) Load(ctx context.Context, sessionID string) (*AnnotatedSession, error) {
	// Load events
	events, err := l.eventStore.Query(ctx, &EventFilter{SessionID: sessionID})
	if err != nil {
		return nil, fmt.Errorf("load events: %w", err)
	}

	// Load annotations if store is available
	var annots []*annotations.Annotation
	if l.annotStore != nil {
		annots, err = l.annotStore.Query(ctx, &annotations.Filter{SessionID: sessionID})
		if err != nil {
			return nil, fmt.Errorf("load annotations: %w", err)
		}
	}

	// Build media timeline
	timeline := NewMediaTimeline(sessionID, events, l.blobStore)

	session := &AnnotatedSession{
		SessionID:   sessionID,
		Events:      events,
		Annotations: annots,
		Timeline:    timeline,
	}

	// Compute metadata
	if l.computeMeta {
		session.Metadata = l.computeMetadata(session)
	}

	return session, nil
}

// computeMetadata computes session metadata from events and annotations.
func (l *AnnotatedSessionLoader) computeMetadata(session *AnnotatedSession) SessionMetadata {
	meta := SessionMetadata{
		EventCounts:      make(map[EventType]int),
		AnnotationCounts: make(map[annotations.AnnotationType]int),
	}

	l.computeTimeRange(session, &meta)
	l.computeEventStats(session, &meta)
	l.computeAnnotationStats(session, &meta)

	return meta
}

// computeTimeRange sets the start/end time and duration from events.
func (l *AnnotatedSessionLoader) computeTimeRange(session *AnnotatedSession, meta *SessionMetadata) {
	if len(session.Events) > 0 {
		meta.StartTime = session.Events[0].Timestamp
		meta.EndTime = session.Events[len(session.Events)-1].Timestamp
		meta.Duration = meta.EndTime.Sub(meta.StartTime)
	}
}

// computeEventStats gathers event counts and media statistics.
func (l *AnnotatedSessionLoader) computeEventStats(session *AnnotatedSession, meta *SessionMetadata) {
	for _, event := range session.Events {
		meta.EventCounts[event.Type]++
		l.updateMediaStats(event, meta)
	}
}

// updateMediaStats updates metadata based on event type.
func (l *AnnotatedSessionLoader) updateMediaStats(event *Event, meta *SessionMetadata) {
	switch event.Type { //nolint:exhaustive // Only counting specific event types
	case EventAudioInput:
		meta.HasAudioInput = true
		if data, ok := event.Data.(*AudioInputData); ok {
			meta.TotalAudioInputDuration += time.Duration(data.Metadata.DurationMs) * time.Millisecond
		}
	case EventAudioOutput:
		meta.HasAudioOutput = true
		if data, ok := event.Data.(*AudioOutputData); ok {
			meta.TotalAudioOutputDuration += time.Duration(data.Metadata.DurationMs) * time.Millisecond
		}
	case EventVideoFrame:
		meta.HasVideo = true
	case EventToolCallStarted:
		meta.ToolCalls++
	case EventProviderCallStarted:
		meta.ProviderCalls++
	case EventMessageCreated:
		if data, ok := event.Data.(*MessageCreatedData); ok && data.Role == "user" {
			meta.ConversationTurns++
		}
	}
}

// computeAnnotationStats counts annotations by type.
func (l *AnnotatedSessionLoader) computeAnnotationStats(session *AnnotatedSession, meta *SessionMetadata) {
	for _, annot := range session.Annotations {
		meta.AnnotationCounts[annot.Type]++
	}
}

// NewSyncPlayer creates a synchronized player for this session.
func (s *AnnotatedSession) NewSyncPlayer(config *SyncPlayerConfig) *SyncPlayer {
	return NewSyncPlayer(s.Timeline, s.Annotations, config)
}

// GetEventsByType returns all events of the specified type.
func (s *AnnotatedSession) GetEventsByType(eventType EventType) []*Event {
	var events []*Event
	for _, event := range s.Events {
		if event.Type == eventType {
			events = append(events, event)
		}
	}
	return events
}

// GetAnnotationsByType returns all annotations of the specified type.
func (s *AnnotatedSession) GetAnnotationsByType(annotType annotations.AnnotationType) []*annotations.Annotation {
	var annots []*annotations.Annotation
	for _, annot := range s.Annotations {
		if annot.Type == annotType {
			annots = append(annots, annot)
		}
	}
	return annots
}

// GetAnnotationsForEvent returns annotations targeting the specified event.
func (s *AnnotatedSession) GetAnnotationsForEvent(eventIndex int) []*annotations.Annotation {
	var annots []*annotations.Annotation
	for _, annot := range s.Annotations {
		if annot.Target.Type == annotations.TargetEvent &&
			annot.Target.EventSequence == int64(eventIndex) {
			annots = append(annots, annot)
		}
	}
	return annots
}

// GetAnnotationsInTimeRange returns annotations active during the specified time range.
func (s *AnnotatedSession) GetAnnotationsInTimeRange(start, end time.Duration) []*annotations.Annotation {
	var annots []*annotations.Annotation
	startTime := s.Metadata.StartTime.Add(start)
	endTime := s.Metadata.StartTime.Add(end)

	for _, annot := range s.Annotations {
		if annot.Target.Type == annotations.TargetTimeRange {
			// Check if ranges overlap
			if annot.Target.StartTime.Before(endTime) && annot.Target.EndTime.After(startTime) {
				annots = append(annots, annot)
			}
		}
	}
	return annots
}

// GetConversationMessages returns all message events in order.
func (s *AnnotatedSession) GetConversationMessages() []*Event {
	var messages []*Event
	for _, event := range s.Events {
		if event.Type == EventMessageCreated {
			messages = append(messages, event)
		}
	}
	return messages
}

// GetTranscriptions returns all transcription events.
func (s *AnnotatedSession) GetTranscriptions() []*Event {
	return s.GetEventsByType(EventAudioTranscription)
}

// Summary returns a human-readable summary of the session.
func (s *AnnotatedSession) Summary() string {
	return fmt.Sprintf(
		"Session %s: %v duration, %d events, %d annotations, %d turns, audio: in=%v out=%v",
		s.SessionID,
		s.Metadata.Duration.Round(time.Millisecond),
		len(s.Events),
		len(s.Annotations),
		s.Metadata.ConversationTurns,
		s.Metadata.HasAudioInput,
		s.Metadata.HasAudioOutput,
	)
}

// TimelineView represents a view of the session timeline.
type TimelineView struct {
	// Items are all items in the timeline, sorted by time.
	Items []TimelineItem
}

// TimelineItemType identifies the type of timeline item.
type TimelineItemType int

const (
	// TimelineItemEvent is an event item.
	TimelineItemEvent TimelineItemType = iota
	// TimelineItemAnnotation is an annotation item.
	TimelineItemAnnotation
	// TimelineItemMedia is a media segment item.
	TimelineItemMedia
)

// TimelineItem represents a single item in the timeline view.
type TimelineItem struct {
	// Type is the item type.
	Type TimelineItemType
	// Time is when this item occurs relative to session start.
	Time time.Duration
	// Duration is how long this item spans (0 for instantaneous items).
	Duration time.Duration
	// Event is the event (if Type == TimelineItemEvent).
	Event *Event
	// Annotation is the annotation (if Type == TimelineItemAnnotation).
	Annotation *annotations.Annotation
	// Track is the media track (if Type == TimelineItemMedia).
	Track TrackType
	// Segment is the media segment (if Type == TimelineItemMedia).
	Segment *MediaSegment
}

// BuildTimelineView creates a unified timeline view of all session content.
func (s *AnnotatedSession) BuildTimelineView() *TimelineView {
	var items []TimelineItem

	items = append(items, s.buildEventItems()...)
	items = append(items, s.buildAnnotationItems()...)
	items = append(items, s.buildMediaItems()...)

	sortTimelineItems(items)
	return &TimelineView{Items: items}
}

// buildEventItems creates timeline items from events.
func (s *AnnotatedSession) buildEventItems() []TimelineItem {
	items := make([]TimelineItem, 0, len(s.Events))
	for _, event := range s.Events {
		items = append(items, TimelineItem{
			Type:  TimelineItemEvent,
			Time:  event.Timestamp.Sub(s.Metadata.StartTime),
			Event: event,
		})
	}
	return items
}

// buildAnnotationItems creates timeline items from annotations.
func (s *AnnotatedSession) buildAnnotationItems() []TimelineItem {
	items := make([]TimelineItem, 0, len(s.Annotations))
	for _, annot := range s.Annotations {
		item := TimelineItem{
			Type:       TimelineItemAnnotation,
			Annotation: annot,
		}
		s.setAnnotationTime(&item, annot)
		items = append(items, item)
	}
	return items
}

// setAnnotationTime sets the time fields for an annotation timeline item.
func (s *AnnotatedSession) setAnnotationTime(item *TimelineItem, annot *annotations.Annotation) {
	switch annot.Target.Type { //nolint:exhaustive // Only handling time-based target types
	case annotations.TargetTimeRange:
		item.Time = annot.Target.StartTime.Sub(s.Metadata.StartTime)
		item.Duration = annot.Target.EndTime.Sub(annot.Target.StartTime)
	case annotations.TargetEvent:
		if annot.Target.EventSequence >= 0 && int(annot.Target.EventSequence) < len(s.Events) {
			item.Time = s.Events[annot.Target.EventSequence].Timestamp.Sub(s.Metadata.StartTime)
		}
	default:
		item.Time = 0
	}
}

// buildMediaItems creates timeline items from media segments.
func (s *AnnotatedSession) buildMediaItems() []TimelineItem {
	var items []TimelineItem
	for trackType, track := range s.Timeline.Tracks {
		for _, seg := range track.Segments {
			items = append(items, TimelineItem{
				Type:     TimelineItemMedia,
				Time:     seg.StartTime,
				Duration: seg.Duration,
				Track:    trackType,
				Segment:  seg,
			})
		}
	}
	return items
}

// sortTimelineItems sorts items by time using bubble sort.
func sortTimelineItems(items []TimelineItem) {
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Time > items[j].Time {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

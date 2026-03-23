// Package events provides event storage for session recording and replay.
package events

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/internal/lru"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// File system constants.
const (
	dirPermissions     = 0750
	filePermissions    = 0600
	scannerBufSize     = 1024 * 1024 // 1MB buffer for large events
	streamChanSize     = 100
	errOpenSessionFile = "open session file: %w"

	// DefaultMaxOpenFiles is the default maximum number of open file handles
	// in FileEventStore before LRU eviction closes the least recently used ones.
	DefaultMaxOpenFiles = 256
)

// EventStore persists events for later replay and analysis.
type EventStore interface {
	// Append adds an event to the store.
	Append(ctx context.Context, event *Event) error

	// OnEvent is a Listener-compatible method for wiring the store as a bus subscriber.
	// Events without a SessionID are silently skipped; errors are logged.
	// Usage: bus.SubscribeAll(store.OnEvent)
	OnEvent(event *Event)

	// Query returns events matching the filter.
	Query(ctx context.Context, filter *EventFilter) ([]*Event, error)

	// QueryRaw returns stored events with raw data preserved.
	// This is useful for export/import where data serialization must be preserved.
	QueryRaw(ctx context.Context, filter *EventFilter) ([]*StoredEvent, error)

	// Stream returns a channel of events for a session.
	// The channel is closed when all events have been sent or context is canceled.
	Stream(ctx context.Context, sessionID string) (<-chan *Event, error)

	// Close releases any resources held by the store.
	Close() error
}

// EventFilter specifies criteria for querying events.
type EventFilter struct {
	SessionID      string
	ConversationID string
	ExecutionID    string
	Types          []EventType
	Since          time.Time
	Until          time.Time
	Limit          int
}

// StoredEvent wraps an Event with storage metadata for serialization.
type StoredEvent struct {
	Sequence int64              `json:"seq"`
	ParentID int64              `json:"parent_id,omitempty"`
	Event    *SerializableEvent `json:"event"`
}

// SerializableEvent is a JSON-friendly version of Event.
// The Data field uses json.RawMessage to preserve type information during round-trips.
type SerializableEvent struct {
	Type           EventType       `json:"type"`
	Timestamp      time.Time       `json:"timestamp"`
	Sequence       int64           `json:"sequence,omitempty"`
	ExecutionID    string          `json:"execution_id,omitempty"`
	SessionID      string          `json:"session_id"`
	ConversationID string          `json:"conversation_id,omitempty"`
	UserID         string          `json:"user_id,omitempty"`
	DataType       string          `json:"data_type,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
}

// toSerializable converts an Event to SerializableEvent.
func toSerializable(e *Event) (*SerializableEvent, error) {
	se := &SerializableEvent{
		Type:           e.Type,
		Timestamp:      e.Timestamp,
		Sequence:       e.Sequence,
		ExecutionID:    e.ExecutionID,
		SessionID:      e.SessionID,
		ConversationID: e.ConversationID,
		UserID:         e.UserID,
	}
	if e.Data != nil {
		se.DataType = fmt.Sprintf("%T", e.Data)
		data, err := json.Marshal(e.Data)
		if err != nil {
			return nil, err
		}
		se.Data = data
	}
	return se, nil
}

// toEvent converts a SerializableEvent back to Event.
// It attempts to deserialize the Data field based on DataType.
func (se *SerializableEvent) toEvent() *Event {
	event := &Event{
		Type:           se.Type,
		Timestamp:      se.Timestamp,
		Sequence:       se.Sequence,
		ExecutionID:    se.ExecutionID,
		SessionID:      se.SessionID,
		ConversationID: se.ConversationID,
		UserID:         se.UserID,
	}

	// Attempt to deserialize Data based on DataType
	if len(se.Data) > 0 {
		event.Data = deserializeEventData(se.DataType, se.Data)
	}

	return event
}

// eventDataFactory is a function that creates a new EventData instance.
type eventDataFactory func() EventData

// eventDataRegistry maps type names to factory functions for deserialization.
// Entries for both canonical names (e.g., "*events.MiddlewareEventData") and
// legacy alias names (e.g., "*events.MiddlewareStartedData") are included
// so that old recordings can still be deserialized.
var eventDataRegistry = map[string]eventDataFactory{
	// Audio events (consolidated into AudioEventData)
	"*events.AudioEventData":         func() EventData { return &AudioEventData{} },
	"*events.AudioInputData":         func() EventData { return &AudioEventData{} },
	"*events.AudioOutputData":        func() EventData { return &AudioEventData{} },
	"*events.AudioTranscriptionData": func() EventData { return &AudioTranscriptionData{} },

	// Video/Image events (ImageInputData/ImageOutputData consolidated into ImageEventData)
	"*events.VideoFrameData":  func() EventData { return &VideoFrameData{} },
	"*events.ScreenshotData":  func() EventData { return &ScreenshotData{} },
	"*events.ImageEventData":  func() EventData { return &ImageEventData{} },
	"*events.ImageInputData":  func() EventData { return &ImageEventData{} },
	"*events.ImageOutputData": func() EventData { return &ImageEventData{} },

	// Message events
	"*events.MessageCreatedData":      func() EventData { return &MessageCreatedData{} },
	"*events.MessageUpdatedData":      func() EventData { return &MessageUpdatedData{} },
	"*events.ConversationStartedData": func() EventData { return &ConversationStartedData{} },

	// Pipeline events
	"*events.PipelineStartedData":   func() EventData { return &PipelineStartedData{} },
	"*events.PipelineCompletedData": func() EventData { return &PipelineCompletedData{} },
	"*events.PipelineFailedData":    func() EventData { return &PipelineFailedData{} },

	// Provider events
	"*events.ProviderCallStartedData":   func() EventData { return &ProviderCallStartedData{} },
	"*events.ProviderCallCompletedData": func() EventData { return &ProviderCallCompletedData{} },
	"*events.ProviderCallFailedData":    func() EventData { return &ProviderCallFailedData{} },

	// Tool events (consolidated into ToolCallEventData)
	"*events.ToolCallEventData":     func() EventData { return &ToolCallEventData{} },
	"*events.ToolCallStartedData":   func() EventData { return &ToolCallEventData{} },
	"*events.ToolCallCompletedData": func() EventData { return &ToolCallEventData{} },
	"*events.ToolCallFailedData":    func() EventData { return &ToolCallEventData{} },

	// Custom events
	"*events.CustomEventData": func() EventData { return &CustomEventData{} },

	// Stage events (consolidated into StageEventData)
	"*events.StageEventData":     func() EventData { return &StageEventData{} },
	"*events.StageStartedData":   func() EventData { return &StageEventData{} },
	"*events.StageCompletedData": func() EventData { return &StageEventData{} },
	"*events.StageFailedData":    func() EventData { return &StageEventData{} },

	// Middleware events (consolidated into MiddlewareEventData)
	"*events.MiddlewareEventData":     func() EventData { return &MiddlewareEventData{} },
	"*events.MiddlewareStartedData":   func() EventData { return &MiddlewareEventData{} },
	"*events.MiddlewareCompletedData": func() EventData { return &MiddlewareEventData{} },
	"*events.MiddlewareFailedData":    func() EventData { return &MiddlewareEventData{} },

	// Validation events (consolidated into ValidationEventData)
	"*events.ValidationEventData":   func() EventData { return &ValidationEventData{} },
	"*events.ValidationStartedData": func() EventData { return &ValidationEventData{} },
	"*events.ValidationPassedData":  func() EventData { return &ValidationEventData{} },
	"*events.ValidationFailedData":  func() EventData { return &ValidationEventData{} },

	// Context/State events (StateLoadedData/StateSavedData consolidated into StateEventData)
	"*events.ContextBuiltData":        func() EventData { return &ContextBuiltData{} },
	"*events.TokenBudgetExceededData": func() EventData { return &TokenBudgetExceededData{} },
	"*events.StateEventData":          func() EventData { return &StateEventData{} },
	"*events.StateLoadedData":         func() EventData { return &StateEventData{} },
	"*events.StateSavedData":          func() EventData { return &StateEventData{} },
	"*events.StreamInterruptedData":   func() EventData { return &StreamInterruptedData{} },

	// Workflow events
	"*events.WorkflowTransitionedData": func() EventData { return &WorkflowTransitionedData{} },
	"*events.WorkflowCompletedData":    func() EventData { return &WorkflowCompletedData{} },
}

// deserializeEventData attempts to deserialize event data based on the type name.
func deserializeEventData(dataType string, data json.RawMessage) EventData {
	factory, ok := eventDataRegistry[dataType]
	if !ok {
		return nil
	}

	result := factory()
	if json.Unmarshal(data, result) != nil {
		return nil
	}

	return result
}

// RawData returns the raw JSON data for custom unmarshaling.
func (se *SerializableEvent) RawData() json.RawMessage {
	return se.Data
}

// FileEventStore implements EventStore using JSON Lines files.
// Each session is stored in a separate file for efficient streaming.
// Open file handles are managed with LRU eviction to bound resource usage.
type FileEventStore struct {
	dir          string
	mu           sync.RWMutex
	files        *lru.Cache[string, *os.File]
	maxOpenFiles int
	sequence     atomic.Int64
}

// FileEventStoreOption configures a FileEventStore.
type FileEventStoreOption func(*FileEventStore)

// WithMaxOpenFiles sets the maximum number of open file handles.
// Default is DefaultMaxOpenFiles (256).
func WithMaxOpenFiles(maxFiles int) FileEventStoreOption {
	return func(s *FileEventStore) {
		s.maxOpenFiles = maxFiles
	}
}

// NewFileEventStore creates a file-based event store.
// Events are stored as JSON Lines in the specified directory.
func NewFileEventStore(dir string, opts ...FileEventStoreOption) (*FileEventStore, error) {
	if err := os.MkdirAll(dir, dirPermissions); err != nil {
		return nil, fmt.Errorf("create event store directory: %w", err)
	}
	s := &FileEventStore{
		dir:          dir,
		maxOpenFiles: DefaultMaxOpenFiles,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.files = lru.New[string, *os.File](s.maxOpenFiles, func(_ string, f *os.File) {
		// Sync and close evicted file handles
		_ = f.Sync()
		_ = f.Close()
	})
	return s, nil
}

// Append adds an event to the store.
func (s *FileEventStore) Append(ctx context.Context, event *Event) error {
	if event.SessionID == "" {
		return fmt.Errorf("event has no session ID")
	}
	if err := validateSessionID(event.SessionID); err != nil {
		return err
	}

	se, err := toSerializable(event)
	if err != nil {
		return fmt.Errorf("serialize event: %w", err)
	}

	stored := StoredEvent{
		Sequence: s.sequence.Add(1),
		Event:    se,
	}

	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.getOrCreateFile(event.SessionID)
	if err != nil {
		return err
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write event: %w", err)
	}

	return nil
}

// Query returns events matching the filter.
func (s *FileEventStore) Query(ctx context.Context, filter *EventFilter) ([]*Event, error) {
	f, err := s.openSessionFile(filter.SessionID)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, nil
	}
	defer f.Close()

	typeSet := buildTypeSet(filter.Types)
	var events []*Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return events, ctx.Err()
		default:
		}

		var stored StoredEvent
		if json.Unmarshal(scanner.Bytes(), &stored) != nil {
			continue // Skip malformed lines
		}

		event := stored.Event.toEvent()
		if s.matchesFilterWithSet(event, filter, typeSet) {
			events = append(events, event)
			if filter.Limit > 0 && len(events) >= filter.Limit {
				break
			}
		}
	}

	return events, scanner.Err()
}

// QueryRaw returns stored events with raw data preserved.
func (s *FileEventStore) QueryRaw(ctx context.Context, filter *EventFilter) ([]*StoredEvent, error) {
	f, err := s.openSessionFile(filter.SessionID)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, nil
	}
	defer f.Close()

	typeSet := buildTypeSet(filter.Types)
	var stored []*StoredEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return stored, ctx.Err()
		default:
		}

		var se StoredEvent
		if json.Unmarshal(scanner.Bytes(), &se) != nil {
			continue // Skip malformed lines
		}

		event := se.Event.toEvent()
		if s.matchesFilterWithSet(event, filter, typeSet) {
			stored = append(stored, &se)
			if filter.Limit > 0 && len(stored) >= filter.Limit {
				break
			}
		}
	}

	return stored, scanner.Err()
}

// Stream returns a channel of events for a session.
func (s *FileEventStore) Stream(ctx context.Context, sessionID string) (<-chan *Event, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	path := s.sessionPath(sessionID)
	f, err := os.Open(path) //nolint:gosec // path is constructed from trusted sessionID
	if err != nil {
		if os.IsNotExist(err) {
			ch := make(chan *Event)
			close(ch)
			return ch, nil
		}
		return nil, fmt.Errorf(errOpenSessionFile, err)
	}

	ch := make(chan *Event, streamChanSize)
	go func() {
		defer close(ch)
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

		for scanner.Scan() {
			var stored StoredEvent
			if json.Unmarshal(scanner.Bytes(), &stored) != nil {
				continue
			}

			select {
			case <-ctx.Done():
				return
			case ch <- stored.Event.toEvent():
			}
		}
	}()

	return ch, nil
}

// Sync flushes all pending writes to disk.
func (s *FileEventStore) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for _, key := range s.files.Keys() {
		if f, ok := s.files.Get(key); ok {
			if err := f.Sync(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("sync files: %v", errs)
	}
	return nil
}

// Close releases all resources.
func (s *FileEventStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for _, key := range s.files.Keys() {
		if f, ok := s.files.Get(key); ok {
			if err := f.Sync(); err != nil {
				errs = append(errs, err)
			}
			if err := f.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	// Replace with empty cache (eviction callbacks already closed files above)
	s.files = lru.New[string, *os.File](s.maxOpenFiles, func(_ string, f *os.File) {
		_ = f.Sync()
		_ = f.Close()
	})

	if len(errs) > 0 {
		return fmt.Errorf("close files: %v", errs)
	}
	return nil
}

// errInvalidSessionID is returned when a session ID contains path traversal sequences.
var errInvalidSessionID = fmt.Errorf("invalid session ID: contains path separator or traversal sequence")

// validateSessionID checks that a session ID does not contain path traversal sequences.
func validateSessionID(sessionID string) error {
	if strings.ContainsAny(sessionID, "/\\") || strings.Contains(sessionID, "..") {
		return errInvalidSessionID
	}
	return nil
}

// openSessionFile validates the session ID and opens the corresponding file.
// Returns (nil, nil) if the file does not exist.
func (s *FileEventStore) openSessionFile(sessionID string) (*os.File, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID required for query")
	}
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	path := s.sessionPath(sessionID)
	f, err := os.Open(path) //nolint:gosec // path is constructed from validated sessionID
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf(errOpenSessionFile, err)
	}
	return f, nil
}

// sessionPath returns the file path for a session.
func (s *FileEventStore) sessionPath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".jsonl")
}

// getOrCreateFile returns the file for a session, creating it if needed.
// Uses LRU eviction to bound the number of open file handles.
// Caller must hold s.mu.
func (s *FileEventStore) getOrCreateFile(sessionID string) (*os.File, error) {
	if f, ok := s.files.Get(sessionID); ok {
		return f, nil
	}

	path := s.sessionPath(sessionID)
	//nolint:gosec // path is constructed from trusted sessionID
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, filePermissions)
	if err != nil {
		return nil, fmt.Errorf("create session file: %w", err)
	}

	s.files.Put(sessionID, f)
	return f, nil
}

// buildTypeSet pre-builds a set from the filter's Types slice for O(1) lookups.
func buildTypeSet(types []EventType) map[EventType]struct{} {
	if len(types) == 0 {
		return nil
	}
	m := make(map[EventType]struct{}, len(types))
	for _, t := range types {
		m[t] = struct{}{}
	}
	return m
}

// matchesFilter checks if an event matches the filter criteria.
func (s *FileEventStore) matchesFilter(event *Event, filter *EventFilter) bool {
	if !s.matchesBasicCriteria(event, filter) {
		return false
	}
	return s.matchesEventTypes(event.Type, filter.Types)
}

// matchesBasicCriteria checks conversation, run, and time filters.
func (s *FileEventStore) matchesBasicCriteria(event *Event, filter *EventFilter) bool {
	if filter.ConversationID != "" && event.ConversationID != filter.ConversationID {
		return false
	}
	if filter.ExecutionID != "" && event.ExecutionID != filter.ExecutionID {
		return false
	}
	if !filter.Since.IsZero() && event.Timestamp.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && event.Timestamp.After(filter.Until) {
		return false
	}
	return true
}

// matchesEventTypes checks if the event type is in the allowed list.
// For filters with many types, callers should use matchesEventTypeSet for O(1) lookup.
func (s *FileEventStore) matchesEventTypes(eventType EventType, types []EventType) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if eventType == t {
			return true
		}
	}
	return false
}

// matchesEventTypeSet checks event type membership using a pre-built set for O(1) lookup.
func matchesEventTypeSet(eventType EventType, typeSet map[EventType]struct{}) bool {
	if len(typeSet) == 0 {
		return true
	}
	_, ok := typeSet[eventType]
	return ok
}

// matchesFilterWithSet checks if an event matches the filter using a pre-built type set.
func (s *FileEventStore) matchesFilterWithSet(
	event *Event, filter *EventFilter, typeSet map[EventType]struct{},
) bool {
	if !s.matchesBasicCriteria(event, filter) {
		return false
	}
	return matchesEventTypeSet(event.Type, typeSet)
}

// OnEvent is a Listener-compatible method that persists an event to the store.
// Events without a SessionID are silently skipped.
// This allows wiring the store as a regular bus subscriber:
//
//	bus.SubscribeAll(store.OnEvent)
func (s *FileEventStore) OnEvent(event *Event) {
	if event.SessionID == "" {
		return
	}
	if err := s.Append(context.Background(), event); err != nil {
		logger.Warn("event store append failed",
			"event_type", string(event.Type),
			"session_id", event.SessionID,
			"error", err,
		)
	}
}

// Ensure FileEventStore implements EventStore.
var _ EventStore = (*FileEventStore)(nil)

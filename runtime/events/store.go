// Package events provides event storage for session recording and replay.
package events

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// File system constants.
const (
	dirPermissions  = 0750
	filePermissions = 0600
	scannerBufSize  = 1024 * 1024 // 1MB buffer for large events
	streamChanSize  = 100
)

// EventStore persists events for later replay and analysis.
type EventStore interface {
	// Append adds an event to the store.
	Append(ctx context.Context, event *Event) error

	// Query returns events matching the filter.
	Query(ctx context.Context, filter *EventFilter) ([]*Event, error)

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
	RunID          string
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
	RunID          string          `json:"run_id,omitempty"`
	SessionID      string          `json:"session_id"`
	ConversationID string          `json:"conversation_id,omitempty"`
	DataType       string          `json:"data_type,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
}

// toSerializable converts an Event to SerializableEvent.
func toSerializable(e *Event) (*SerializableEvent, error) {
	se := &SerializableEvent{
		Type:           e.Type,
		Timestamp:      e.Timestamp,
		RunID:          e.RunID,
		SessionID:      e.SessionID,
		ConversationID: e.ConversationID,
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
// Note: Data is left as nil since the concrete type cannot be recovered.
// Use the raw Data field for analysis.
func (se *SerializableEvent) toEvent() *Event {
	return &Event{
		Type:           se.Type,
		Timestamp:      se.Timestamp,
		RunID:          se.RunID,
		SessionID:      se.SessionID,
		ConversationID: se.ConversationID,
		// Data is not deserialized - use RawData() for access
	}
}

// RawData returns the raw JSON data for custom unmarshaling.
func (se *SerializableEvent) RawData() json.RawMessage {
	return se.Data
}

// FileEventStore implements EventStore using JSON Lines files.
// Each session is stored in a separate file for efficient streaming.
type FileEventStore struct {
	dir      string
	mu       sync.RWMutex
	files    map[string]*os.File
	sequence atomic.Int64
}

// NewFileEventStore creates a file-based event store.
// Events are stored as JSON Lines in the specified directory.
func NewFileEventStore(dir string) (*FileEventStore, error) {
	if err := os.MkdirAll(dir, dirPermissions); err != nil {
		return nil, fmt.Errorf("create event store directory: %w", err)
	}
	return &FileEventStore{
		dir:   dir,
		files: make(map[string]*os.File),
	}, nil
}

// Append adds an event to the store.
func (s *FileEventStore) Append(ctx context.Context, event *Event) error {
	if event.SessionID == "" {
		return fmt.Errorf("event has no session ID")
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
	if filter.SessionID == "" {
		return nil, fmt.Errorf("session ID required for query")
	}

	path := s.sessionPath(filter.SessionID)
	f, err := os.Open(path) //nolint:gosec // path is constructed from trusted sessionID
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open session file: %w", err)
	}
	defer f.Close()

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
		if err := json.Unmarshal(scanner.Bytes(), &stored); err != nil {
			continue // Skip malformed lines
		}

		event := stored.Event.toEvent()
		if s.matchesFilter(event, filter) {
			events = append(events, event)
			if filter.Limit > 0 && len(events) >= filter.Limit {
				break
			}
		}
	}

	return events, scanner.Err()
}

// Stream returns a channel of events for a session.
func (s *FileEventStore) Stream(ctx context.Context, sessionID string) (<-chan *Event, error) {
	path := s.sessionPath(sessionID)
	f, err := os.Open(path) //nolint:gosec // path is constructed from trusted sessionID
	if err != nil {
		if os.IsNotExist(err) {
			ch := make(chan *Event)
			close(ch)
			return ch, nil
		}
		return nil, fmt.Errorf("open session file: %w", err)
	}

	ch := make(chan *Event, streamChanSize)
	go func() {
		defer close(ch)
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

		for scanner.Scan() {
			var stored StoredEvent
			if err := json.Unmarshal(scanner.Bytes(), &stored); err != nil {
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
	for _, f := range s.files {
		if err := f.Sync(); err != nil {
			errs = append(errs, err)
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
	for _, f := range s.files {
		if err := f.Sync(); err != nil {
			errs = append(errs, err)
		}
		if err := f.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	s.files = make(map[string]*os.File)

	if len(errs) > 0 {
		return fmt.Errorf("close files: %v", errs)
	}
	return nil
}

// sessionPath returns the file path for a session.
func (s *FileEventStore) sessionPath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".jsonl")
}

// getOrCreateFile returns the file for a session, creating it if needed.
// Caller must hold s.mu.
func (s *FileEventStore) getOrCreateFile(sessionID string) (*os.File, error) {
	if f, ok := s.files[sessionID]; ok {
		return f, nil
	}

	path := s.sessionPath(sessionID)
	//nolint:gosec // path is constructed from trusted sessionID
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, filePermissions)
	if err != nil {
		return nil, fmt.Errorf("create session file: %w", err)
	}

	s.files[sessionID] = f
	return f, nil
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
	if filter.RunID != "" && event.RunID != filter.RunID {
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

// Ensure FileEventStore implements EventStore.
var _ EventStore = (*FileEventStore)(nil)

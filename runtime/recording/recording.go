// Package recording provides session recording export and import for replay and analysis.
package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// Format specifies the recording file format.
type Format string

const (
	// FormatJSON uses JSON encoding (human-readable, larger files).
	FormatJSON Format = "json"
	// FormatJSONLines uses JSON Lines encoding (streamable, one event per line).
	FormatJSONLines Format = "jsonl"
)

// filePermissions for recording files.
const filePermissions = 0600

// SessionRecording is a self-contained artifact for replay and analysis.
// It contains all information needed to replay a session without access
// to the original event store.
type SessionRecording struct {
	// Metadata about the recording
	Metadata Metadata `json:"metadata"`

	// Events in chronological order
	Events []RecordedEvent `json:"events"`
}

// Metadata contains session-level information.
type Metadata struct {
	// SessionID is the unique identifier for this session.
	SessionID string `json:"session_id"`

	// ConversationID groups related turns within a session.
	ConversationID string `json:"conversation_id,omitempty"`

	// StartTime is when the session began.
	StartTime time.Time `json:"start_time"`

	// EndTime is when the session ended.
	EndTime time.Time `json:"end_time"`

	// Duration is the total session length.
	Duration time.Duration `json:"duration"`

	// EventCount is the total number of events.
	EventCount int `json:"event_count"`

	// ProviderName is the LLM provider used (e.g., "openai", "gemini").
	ProviderName string `json:"provider_name,omitempty"`

	// Model is the model identifier used.
	Model string `json:"model,omitempty"`

	// Version is the recording format version.
	Version string `json:"version"`

	// CreatedAt is when this recording was exported.
	CreatedAt time.Time `json:"created_at"`

	// Custom allows arbitrary metadata to be attached.
	Custom map[string]any `json:"custom,omitempty"`
}

// RecordedEvent wraps an event with additional recording-specific data.
type RecordedEvent struct {
	// Sequence is the event's position in the recording.
	Sequence int64 `json:"seq"`

	// ParentSequence links to a parent event (for causality).
	ParentSequence int64 `json:"parent_seq,omitempty"`

	// Type is the event type.
	Type events.EventType `json:"type"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`

	// Offset is the time since session start.
	Offset time.Duration `json:"offset"`

	// SessionID identifies the session.
	SessionID string `json:"session_id"`

	// ConversationID identifies the conversation within the session.
	ConversationID string `json:"conversation_id,omitempty"`

	// RunID identifies the specific run/request.
	RunID string `json:"run_id,omitempty"`

	// DataType is the Go type name of the original data.
	DataType string `json:"data_type,omitempty"`

	// Data is the event payload as raw JSON.
	Data json.RawMessage `json:"data,omitempty"`
}

// recordingVersion is the current format version.
const recordingVersion = "1.0"

// Export creates a SessionRecording from stored events.
func Export(ctx context.Context, store events.EventStore, sessionID string) (*SessionRecording, error) {
	storedEvents, err := store.QueryRaw(ctx, &events.EventFilter{SessionID: sessionID})
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}

	if len(storedEvents) == 0 {
		return nil, fmt.Errorf("no events found for session %s", sessionID)
	}

	// Sort by timestamp to ensure chronological order
	sort.Slice(storedEvents, func(i, j int) bool {
		return storedEvents[i].Event.Timestamp.Before(storedEvents[j].Event.Timestamp)
	})

	sessionStart := storedEvents[0].Event.Timestamp
	sessionEnd := storedEvents[len(storedEvents)-1].Event.Timestamp

	recording := &SessionRecording{
		Metadata: Metadata{
			SessionID:  sessionID,
			StartTime:  sessionStart,
			EndTime:    sessionEnd,
			Duration:   sessionEnd.Sub(sessionStart),
			EventCount: len(storedEvents),
			Version:    recordingVersion,
			CreatedAt:  time.Now(),
		},
		Events: make([]RecordedEvent, len(storedEvents)),
	}

	// Convert stored events to recorded format
	for i, se := range storedEvents {
		e := se.Event
		recorded := RecordedEvent{
			Sequence:       se.Sequence,
			ParentSequence: se.ParentID,
			Type:           e.Type,
			Timestamp:      e.Timestamp,
			Offset:         e.Timestamp.Sub(sessionStart),
			SessionID:      e.SessionID,
			ConversationID: e.ConversationID,
			RunID:          e.RunID,
			DataType:       e.DataType,
			Data:           e.Data,
		}

		// Capture conversation ID for metadata if not set
		if recording.Metadata.ConversationID == "" && e.ConversationID != "" {
			recording.Metadata.ConversationID = e.ConversationID
		}

		recording.Events[i] = recorded
	}

	return recording, nil
}

// ExportOptions configures the export process.
type ExportOptions struct {
	// ProviderName to include in metadata.
	ProviderName string

	// Model to include in metadata.
	Model string

	// Custom metadata to include.
	Custom map[string]any
}

// ExportWithOptions creates a SessionRecording with additional metadata.
func ExportWithOptions(
	ctx context.Context,
	store events.EventStore,
	sessionID string,
	opts ExportOptions,
) (*SessionRecording, error) {
	rec, err := Export(ctx, store, sessionID)
	if err != nil {
		return nil, err
	}

	rec.Metadata.ProviderName = opts.ProviderName
	rec.Metadata.Model = opts.Model
	rec.Metadata.Custom = opts.Custom

	return rec, nil
}

// SaveTo writes the recording to a file.
func (r *SessionRecording) SaveTo(path string, format Format) error {
	var data []byte
	var err error

	switch format {
	case FormatJSON:
		data, err = json.MarshalIndent(r, "", "  ")
	case FormatJSONLines:
		data, err = r.marshalJSONLines()
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return fmt.Errorf("marshal recording: %w", err)
	}

	if err := os.WriteFile(path, data, filePermissions); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// marshalJSONLines creates JSONL output with metadata on first line.
func (r *SessionRecording) marshalJSONLines() ([]byte, error) {
	// First line is metadata
	metaLine, err := json.Marshal(map[string]any{
		"type":     "metadata",
		"metadata": r.Metadata,
	})
	if err != nil {
		return nil, err
	}

	var result []byte
	result = append(result, metaLine...)
	result = append(result, '\n')

	// Subsequent lines are events
	for i := range r.Events {
		eventLine, err := json.Marshal(map[string]any{
			"type":  "event",
			"event": r.Events[i],
		})
		if err != nil {
			return nil, err
		}
		result = append(result, eventLine...)
		result = append(result, '\n')
	}

	return result, nil
}

// Load reads a recording from a file.
func Load(path string) (*SessionRecording, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is user-provided
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Try JSON first
	var rec SessionRecording
	if err := json.Unmarshal(data, &rec); err == nil && rec.Metadata.Version != "" {
		return &rec, nil
	}

	// Try JSONL
	return loadJSONLines(data)
}

// loadJSONLines parses JSONL format.
func loadJSONLines(data []byte) (*SessionRecording, error) {
	rec := &SessionRecording{}
	lines := splitLines(data)

	for i, line := range lines {
		if len(line) == 0 {
			continue
		}

		var wrapper struct {
			Type     string        `json:"type"`
			Metadata Metadata      `json:"metadata,omitempty"`
			Event    RecordedEvent `json:"event,omitempty"`
		}

		if err := json.Unmarshal(line, &wrapper); err != nil {
			return nil, fmt.Errorf("parse line %d: %w", i+1, err)
		}

		switch wrapper.Type {
		case "metadata":
			rec.Metadata = wrapper.Metadata
		case "event":
			rec.Events = append(rec.Events, wrapper.Event)
		}
	}

	if rec.Metadata.Version == "" {
		return nil, fmt.Errorf("invalid recording: missing metadata")
	}

	return rec, nil
}

// splitLines splits data into lines without using bufio.Scanner.
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// ToEvents converts recorded events back to Event objects.
// Note: Data is left as nil since concrete types cannot be recovered.
func (r *SessionRecording) ToEvents() []*events.Event {
	result := make([]*events.Event, len(r.Events))
	for i := range r.Events {
		re := &r.Events[i]
		result[i] = &events.Event{
			Type:           re.Type,
			Timestamp:      re.Timestamp,
			SessionID:      re.SessionID,
			ConversationID: re.ConversationID,
			RunID:          re.RunID,
		}
	}
	return result
}

// Duration returns the total duration of the recording.
func (r *SessionRecording) Duration() time.Duration {
	return r.Metadata.Duration
}

// String returns a human-readable summary of the recording.
func (r *SessionRecording) String() string {
	return fmt.Sprintf("SessionRecording{session=%s, events=%d, duration=%v}",
		r.Metadata.SessionID, r.Metadata.EventCount, r.Metadata.Duration)
}

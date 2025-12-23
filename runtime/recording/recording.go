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

// eventDataRegistry maps type names to factory functions for event data types.
// This enables deserialization of recorded events back to their typed structs.
var eventDataRegistry = map[string]func() events.EventData{
	// Audio events
	"*events.AudioInputData":         func() events.EventData { return &events.AudioInputData{} },
	"events.AudioInputData":          func() events.EventData { return &events.AudioInputData{} },
	"*events.AudioOutputData":        func() events.EventData { return &events.AudioOutputData{} },
	"events.AudioOutputData":         func() events.EventData { return &events.AudioOutputData{} },
	"*events.AudioTranscriptionData": func() events.EventData { return &events.AudioTranscriptionData{} },
	"events.AudioTranscriptionData":  func() events.EventData { return &events.AudioTranscriptionData{} },

	// Video/Image events
	"*events.VideoFrameData":  func() events.EventData { return &events.VideoFrameData{} },
	"events.VideoFrameData":   func() events.EventData { return &events.VideoFrameData{} },
	"*events.ScreenshotData":  func() events.EventData { return &events.ScreenshotData{} },
	"events.ScreenshotData":   func() events.EventData { return &events.ScreenshotData{} },
	"*events.ImageInputData":  func() events.EventData { return &events.ImageInputData{} },
	"events.ImageInputData":   func() events.EventData { return &events.ImageInputData{} },
	"*events.ImageOutputData": func() events.EventData { return &events.ImageOutputData{} },
	"events.ImageOutputData":  func() events.EventData { return &events.ImageOutputData{} },

	// Message events
	"*events.MessageCreatedData":      func() events.EventData { return &events.MessageCreatedData{} },
	"events.MessageCreatedData":       func() events.EventData { return &events.MessageCreatedData{} },
	"*events.MessageUpdatedData":      func() events.EventData { return &events.MessageUpdatedData{} },
	"events.MessageUpdatedData":       func() events.EventData { return &events.MessageUpdatedData{} },
	"*events.ConversationStartedData": func() events.EventData { return &events.ConversationStartedData{} },
	"events.ConversationStartedData":  func() events.EventData { return &events.ConversationStartedData{} },

	// Pipeline events
	"*events.PipelineStartedData":   func() events.EventData { return &events.PipelineStartedData{} },
	"events.PipelineStartedData":    func() events.EventData { return &events.PipelineStartedData{} },
	"*events.PipelineCompletedData": func() events.EventData { return &events.PipelineCompletedData{} },
	"events.PipelineCompletedData":  func() events.EventData { return &events.PipelineCompletedData{} },
	"*events.PipelineFailedData":    func() events.EventData { return &events.PipelineFailedData{} },
	"events.PipelineFailedData":     func() events.EventData { return &events.PipelineFailedData{} },

	// Provider events
	"*events.ProviderCallStartedData":   func() events.EventData { return &events.ProviderCallStartedData{} },
	"events.ProviderCallStartedData":    func() events.EventData { return &events.ProviderCallStartedData{} },
	"*events.ProviderCallCompletedData": func() events.EventData { return &events.ProviderCallCompletedData{} },
	"events.ProviderCallCompletedData":  func() events.EventData { return &events.ProviderCallCompletedData{} },
	"*events.ProviderCallFailedData":    func() events.EventData { return &events.ProviderCallFailedData{} },
	"events.ProviderCallFailedData":     func() events.EventData { return &events.ProviderCallFailedData{} },

	// Tool events
	"*events.ToolCallStartedData":   func() events.EventData { return &events.ToolCallStartedData{} },
	"events.ToolCallStartedData":    func() events.EventData { return &events.ToolCallStartedData{} },
	"*events.ToolCallCompletedData": func() events.EventData { return &events.ToolCallCompletedData{} },
	"events.ToolCallCompletedData":  func() events.EventData { return &events.ToolCallCompletedData{} },
	"*events.ToolCallFailedData":    func() events.EventData { return &events.ToolCallFailedData{} },
	"events.ToolCallFailedData":     func() events.EventData { return &events.ToolCallFailedData{} },

	// Custom events
	"*events.CustomEventData": func() events.EventData { return &events.CustomEventData{} },
	"events.CustomEventData":  func() events.EventData { return &events.CustomEventData{} },

	// Stage events
	"*events.StageStartedData":   func() events.EventData { return &events.StageStartedData{} },
	"events.StageStartedData":    func() events.EventData { return &events.StageStartedData{} },
	"*events.StageCompletedData": func() events.EventData { return &events.StageCompletedData{} },
	"events.StageCompletedData":  func() events.EventData { return &events.StageCompletedData{} },
	"*events.StageFailedData":    func() events.EventData { return &events.StageFailedData{} },
	"events.StageFailedData":     func() events.EventData { return &events.StageFailedData{} },

	// Middleware events
	"*events.MiddlewareStartedData":   func() events.EventData { return &events.MiddlewareStartedData{} },
	"events.MiddlewareStartedData":    func() events.EventData { return &events.MiddlewareStartedData{} },
	"*events.MiddlewareCompletedData": func() events.EventData { return &events.MiddlewareCompletedData{} },
	"events.MiddlewareCompletedData":  func() events.EventData { return &events.MiddlewareCompletedData{} },
	"*events.MiddlewareFailedData":    func() events.EventData { return &events.MiddlewareFailedData{} },
	"events.MiddlewareFailedData":     func() events.EventData { return &events.MiddlewareFailedData{} },

	// Validation events
	"*events.ValidationStartedData": func() events.EventData { return &events.ValidationStartedData{} },
	"events.ValidationStartedData":  func() events.EventData { return &events.ValidationStartedData{} },
	"*events.ValidationPassedData":  func() events.EventData { return &events.ValidationPassedData{} },
	"events.ValidationPassedData":   func() events.EventData { return &events.ValidationPassedData{} },
	"*events.ValidationFailedData":  func() events.EventData { return &events.ValidationFailedData{} },
	"events.ValidationFailedData":   func() events.EventData { return &events.ValidationFailedData{} },

	// Context/State events
	"*events.ContextBuiltData":        func() events.EventData { return &events.ContextBuiltData{} },
	"events.ContextBuiltData":         func() events.EventData { return &events.ContextBuiltData{} },
	"*events.TokenBudgetExceededData": func() events.EventData { return &events.TokenBudgetExceededData{} },
	"events.TokenBudgetExceededData":  func() events.EventData { return &events.TokenBudgetExceededData{} },
	"*events.StateLoadedData":         func() events.EventData { return &events.StateLoadedData{} },
	"events.StateLoadedData":          func() events.EventData { return &events.StateLoadedData{} },
	"*events.StateSavedData":          func() events.EventData { return &events.StateSavedData{} },
	"events.StateSavedData":           func() events.EventData { return &events.StateSavedData{} },
	"*events.StreamInterruptedData":   func() events.EventData { return &events.StreamInterruptedData{} },
	"events.StreamInterruptedData":    func() events.EventData { return &events.StreamInterruptedData{} },
}

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
// Supports multiple formats:
// - JSON: Full SessionRecording struct
// - JSONL (SessionRecording): First line is {"type":"metadata",...}, subsequent lines are {"type":"event",...}
// - JSONL (EventStore): Lines are {"seq":N,"event":{...}} format from FileEventStore
func Load(path string) (*SessionRecording, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is user-provided
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Try JSON first (full SessionRecording struct)
	var rec SessionRecording
	if err := json.Unmarshal(data, &rec); err == nil && rec.Metadata.Version != "" {
		return &rec, nil
	}

	// Try JSONL formats
	return loadJSONLines(data)
}

// loadJSONLines parses JSONL format.
// It auto-detects between SessionRecording format and EventStore format.
func loadJSONLines(data []byte) (*SessionRecording, error) {
	lines := splitLines(data)
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty recording file")
	}

	// Detect format by checking first non-empty line
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		// Check if it's EventStore format (has "seq" and "event" fields)
		var probe struct {
			Seq   int64           `json:"seq"`
			Event json.RawMessage `json:"event"`
		}
		if err := json.Unmarshal(line, &probe); err == nil && probe.Seq > 0 && len(probe.Event) > 0 {
			return loadEventStoreFormat(lines)
		}
		// Otherwise try SessionRecording format
		break
	}

	return loadSessionRecordingFormat(lines)
}

// loadSessionRecordingFormat parses the SessionRecording JSONL format.
// First line is {"type":"metadata",...}, subsequent lines are {"type":"event",...}.
func loadSessionRecordingFormat(lines [][]byte) (*SessionRecording, error) {
	rec := &SessionRecording{}

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

// storedEventFormat matches the FileEventStore format.
type storedEventFormat struct {
	Sequence int64 `json:"seq"`
	ParentID int64 `json:"parent_id,omitempty"`
	Event    struct {
		Type           events.EventType `json:"type"`
		Timestamp      time.Time        `json:"timestamp"`
		RunID          string           `json:"run_id,omitempty"`
		SessionID      string           `json:"session_id"`
		ConversationID string           `json:"conversation_id,omitempty"`
		DataType       string           `json:"data_type,omitempty"`
		Data           json.RawMessage  `json:"data,omitempty"`
	} `json:"event"`
}

// loadEventStoreFormat parses the FileEventStore JSONL format.
// Each line is {"seq":N,"parent_id":M,"event":{...}}.
//
//nolint:gocognit // Sequential parsing steps are straightforward despite high count
func loadEventStoreFormat(lines [][]byte) (*SessionRecording, error) {
	rec := &SessionRecording{
		Metadata: Metadata{
			Version: recordingVersion,
		},
	}

	var sessionStart, sessionEnd time.Time

	for i, line := range lines {
		if len(line) == 0 {
			continue
		}

		var stored storedEventFormat
		if err := json.Unmarshal(line, &stored); err != nil {
			return nil, fmt.Errorf("parse line %d: %w", i+1, err)
		}

		e := stored.Event

		// Track session boundaries
		if sessionStart.IsZero() || e.Timestamp.Before(sessionStart) {
			sessionStart = e.Timestamp
		}
		if sessionEnd.IsZero() || e.Timestamp.After(sessionEnd) {
			sessionEnd = e.Timestamp
		}

		// Capture session/conversation IDs for metadata
		if rec.Metadata.SessionID == "" && e.SessionID != "" {
			rec.Metadata.SessionID = e.SessionID
		}
		if rec.Metadata.ConversationID == "" && e.ConversationID != "" {
			rec.Metadata.ConversationID = e.ConversationID
		}

		recorded := RecordedEvent{
			Sequence:       stored.Sequence,
			ParentSequence: stored.ParentID,
			Type:           e.Type,
			Timestamp:      e.Timestamp,
			SessionID:      e.SessionID,
			ConversationID: e.ConversationID,
			RunID:          e.RunID,
			DataType:       e.DataType,
			Data:           e.Data,
		}

		rec.Events = append(rec.Events, recorded)
	}

	if len(rec.Events) == 0 {
		return nil, fmt.Errorf("no events found in recording")
	}

	// Calculate offsets and finalize metadata
	rec.Metadata.StartTime = sessionStart
	rec.Metadata.EndTime = sessionEnd
	rec.Metadata.Duration = sessionEnd.Sub(sessionStart)
	rec.Metadata.EventCount = len(rec.Events)
	rec.Metadata.CreatedAt = time.Now()

	// Calculate offsets from session start
	for i := range rec.Events {
		rec.Events[i].Offset = rec.Events[i].Timestamp.Sub(sessionStart)
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
// Note: Data is left as nil since concrete types cannot be recovered without deserialization.
// Use ToTypedEvents() for full data recovery.
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

// ToTypedEvents converts recorded events back to Event objects with properly typed Data fields.
// This enables reconstruction of audio/video tracks via MediaTimeline.
func (r *SessionRecording) ToTypedEvents() ([]*events.Event, error) {
	result := make([]*events.Event, len(r.Events))
	for i := range r.Events {
		re := &r.Events[i]
		event := &events.Event{
			Type:           re.Type,
			Timestamp:      re.Timestamp,
			SessionID:      re.SessionID,
			ConversationID: re.ConversationID,
			RunID:          re.RunID,
		}

		// Deserialize the Data field based on DataType
		if len(re.Data) > 0 && re.DataType != "" {
			data, err := deserializeEventData(re.DataType, re.Data)
			if err != nil {
				// Log warning but continue - some types may not be recognized
				// This allows forward compatibility with new event types
				event.Data = nil
			} else {
				event.Data = data
			}
		}

		result[i] = event
	}
	return result, nil
}

// deserializeEventData deserializes JSON data to the appropriate typed struct based on dataType.
// Uses the eventDataRegistry map for O(1) lookup instead of a large switch statement.
func deserializeEventData(dataType string, data json.RawMessage) (events.EventData, error) {
	factory, ok := eventDataRegistry[dataType]
	if !ok {
		// Unknown type - return nil without error for forward compatibility
		return nil, nil
	}

	target := factory()
	if err := json.Unmarshal(data, target); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", dataType, err)
	}

	return target, nil
}

// Duration returns the total duration of the recording.
func (r *SessionRecording) Duration() time.Duration {
	return r.Metadata.Duration
}

// ToMediaTimeline creates a MediaTimeline from this recording for audio/video reconstruction.
// The blobStore is optional and used for loading external blob references (nil for inline data only).
func (r *SessionRecording) ToMediaTimeline(blobStore events.BlobStore) (*events.MediaTimeline, error) {
	typedEvents, err := r.ToTypedEvents()
	if err != nil {
		return nil, fmt.Errorf("convert to typed events: %w", err)
	}

	return events.NewMediaTimeline(r.Metadata.SessionID, typedEvents, blobStore), nil
}

// String returns a human-readable summary of the recording.
func (r *SessionRecording) String() string {
	return fmt.Sprintf("SessionRecording{session=%s, events=%d, duration=%v}",
		r.Metadata.SessionID, r.Metadata.EventCount, r.Metadata.Duration)
}

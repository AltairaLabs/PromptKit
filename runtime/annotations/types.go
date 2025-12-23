// Package annotations provides out-of-band annotations for session recordings.
// Annotations allow layering evaluations, feedback, and metadata on recorded sessions
// without modifying the authoritative event record.
package annotations

import (
	"time"
)

// AnnotationType identifies the kind of annotation.
type AnnotationType string

// Annotation types.
const (
	// TypeScore represents a numeric evaluation score.
	TypeScore AnnotationType = "score"
	// TypeLabel represents a categorical label.
	TypeLabel AnnotationType = "label"
	// TypeComment represents a textual comment or note.
	TypeComment AnnotationType = "comment"
	// TypeFlag represents a binary flag (e.g., safety, policy).
	TypeFlag AnnotationType = "flag"
	// TypeMetric represents a performance or quality metric.
	TypeMetric AnnotationType = "metric"
	// TypeAssertion represents an assertion result (pass/fail).
	TypeAssertion AnnotationType = "assertion"
	// TypeGroundTruth represents ground truth labels for training.
	TypeGroundTruth AnnotationType = "ground_truth"
)

// TargetType identifies what the annotation targets.
type TargetType string

// Target types.
const (
	// TargetSession targets the entire session.
	TargetSession TargetType = "session"
	// TargetEvent targets a specific event.
	TargetEvent TargetType = "event"
	// TargetTimeRange targets a time range within the session.
	TargetTimeRange TargetType = "time_range"
	// TargetTurn targets a specific conversation turn.
	TargetTurn TargetType = "turn"
	// TargetMessage targets a specific message.
	TargetMessage TargetType = "message"
)

// Annotation represents an out-of-band annotation on a session or event.
type Annotation struct {
	// ID is a unique identifier for this annotation.
	ID string `json:"id"`

	// Type identifies the kind of annotation.
	Type AnnotationType `json:"type"`

	// SessionID is the session this annotation belongs to.
	SessionID string `json:"session_id"`

	// Target specifies what this annotation targets.
	Target Target `json:"target"`

	// Key is the annotation key (e.g., "quality", "category", "safety").
	Key string `json:"key"`

	// Value holds the annotation value (type depends on annotation type).
	Value AnnotationValue `json:"value"`

	// Metadata contains additional structured data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// CreatedAt is when this annotation was created.
	CreatedAt time.Time `json:"created_at"`

	// CreatedBy identifies who created this annotation.
	CreatedBy string `json:"created_by,omitempty"`

	// Version is the annotation version (for corrections/updates).
	Version int `json:"version"`

	// PreviousID references the previous version if this is an update.
	PreviousID string `json:"previous_id,omitempty"`
}

// Target specifies what an annotation targets.
type Target struct {
	// Type identifies the target type.
	Type TargetType `json:"type"`

	// EventID is the target event ID (for TargetEvent).
	EventID string `json:"event_id,omitempty"`

	// EventSequence is the target event sequence number (alternative to EventID).
	EventSequence int64 `json:"event_sequence,omitempty"`

	// TurnIndex is the target turn index (for TargetTurn).
	TurnIndex int `json:"turn_index,omitempty"`

	// MessageIndex is the target message index (for TargetMessage).
	MessageIndex int `json:"message_index,omitempty"`

	// StartTime is the start of the time range (for TargetTimeRange).
	StartTime time.Time `json:"start_time,omitempty"`

	// EndTime is the end of the time range (for TargetTimeRange).
	EndTime time.Time `json:"end_time,omitempty"`
}

// AnnotationValue holds the value of an annotation.
// The actual type depends on the annotation type.
type AnnotationValue struct {
	// Score is the numeric value (for TypeScore, TypeMetric).
	Score *float64 `json:"score,omitempty"`

	// Label is the categorical value (for TypeLabel, TypeGroundTruth).
	Label string `json:"label,omitempty"`

	// Labels is a list of categorical values (for multi-label scenarios).
	Labels []string `json:"labels,omitempty"`

	// Text is the textual value (for TypeComment).
	Text string `json:"text,omitempty"`

	// Flag is the boolean value (for TypeFlag).
	Flag *bool `json:"flag,omitempty"`

	// Passed indicates assertion result (for TypeAssertion).
	Passed *bool `json:"passed,omitempty"`

	// Message is an optional message (for TypeAssertion, TypeComment).
	Message string `json:"message,omitempty"`

	// Unit is the unit of measurement (for TypeMetric).
	Unit string `json:"unit,omitempty"`
}

// ForSession creates a target for the entire session.
func ForSession() Target {
	return Target{Type: TargetSession}
}

// AtEvent creates a target for a specific event.
func AtEvent(eventID string) Target {
	return Target{
		Type:    TargetEvent,
		EventID: eventID,
	}
}

// AtEventSequence creates a target for an event by sequence number.
func AtEventSequence(seq int64) Target {
	return Target{
		Type:          TargetEvent,
		EventSequence: seq,
	}
}

// AtTurn creates a target for a specific conversation turn.
func AtTurn(turnIndex int) Target {
	return Target{
		Type:      TargetTurn,
		TurnIndex: turnIndex,
	}
}

// AtMessage creates a target for a specific message.
func AtMessage(messageIndex int) Target {
	return Target{
		Type:         TargetMessage,
		MessageIndex: messageIndex,
	}
}

// InTimeRange creates a target for a time range.
func InTimeRange(start, end time.Time) Target {
	return Target{
		Type:      TargetTimeRange,
		StartTime: start,
		EndTime:   end,
	}
}

// NewScoreValue creates a score annotation value.
func NewScoreValue(score float64) AnnotationValue {
	return AnnotationValue{Score: &score}
}

// NewLabelValue creates a label annotation value.
func NewLabelValue(label string) AnnotationValue {
	return AnnotationValue{Label: label}
}

// NewLabelsValue creates a multi-label annotation value.
func NewLabelsValue(labels ...string) AnnotationValue {
	return AnnotationValue{Labels: labels}
}

// NewCommentValue creates a comment annotation value.
func NewCommentValue(text string) AnnotationValue {
	return AnnotationValue{Text: text}
}

// NewFlagValue creates a flag annotation value.
func NewFlagValue(flag bool) AnnotationValue {
	return AnnotationValue{Flag: &flag}
}

// NewAssertionValue creates an assertion annotation value.
func NewAssertionValue(passed bool, message string) AnnotationValue {
	return AnnotationValue{
		Passed:  &passed,
		Message: message,
	}
}

// NewMetricValue creates a metric annotation value with optional unit.
func NewMetricValue(value float64, unit string) AnnotationValue {
	return AnnotationValue{
		Score: &value,
		Unit:  unit,
	}
}

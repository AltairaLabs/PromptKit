// Package sources provides built-in SessionSourceAdapter implementations.
package sources

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
	"github.com/AltairaLabs/PromptKit/tools/arena/generate"
)

// RecordingsAdapter bridges existing recording adapters to the SessionSourceAdapter interface.
// It uses the adapters.Registry to enumerate and load recording files.
type RecordingsAdapter struct {
	source   string
	registry *adapters.Registry
}

// NewRecordingsAdapter creates a RecordingsAdapter that reads from the given source glob.
func NewRecordingsAdapter(source string) *RecordingsAdapter {
	return &RecordingsAdapter{
		source:   source,
		registry: adapters.NewRegistry(),
	}
}

// Name returns "recordings".
func (a *RecordingsAdapter) Name() string {
	return "recordings"
}

// List enumerates recording files and returns a summary for each.
func (a *RecordingsAdapter) List(
	_ context.Context,
	opts generate.ListOptions,
) ([]generate.SessionSummary, error) {
	refs, err := a.registry.Enumerate(a.source, "")
	if err != nil {
		return nil, fmt.Errorf("enumerating recordings: %w", err)
	}

	var summaries []generate.SessionSummary
	for _, ref := range refs {
		msgs, meta, loadErr := a.registry.Load(ref)
		if loadErr != nil {
			continue // skip unloadable recordings
		}

		summary := generate.SessionSummary{
			ID:        ref.ID,
			Source:    ref.Source,
			TurnCount: len(msgs),
		}
		if meta != nil {
			summary.Tags = meta.Tags
			if meta.SessionID != "" {
				summary.ID = meta.SessionID
			}
			if len(meta.Timestamps) > 0 {
				summary.Timestamp = meta.Timestamps[0]
			}
		}

		summaries = append(summaries, summary)

		if opts.Limit > 0 && len(summaries) >= opts.Limit {
			break
		}
	}

	return summaries, nil
}

// Get loads a single recording by ID and returns its full session detail.
func (a *RecordingsAdapter) Get(_ context.Context, sessionID string) (*generate.SessionDetail, error) {
	ref := adapters.RecordingReference{
		ID:     sessionID,
		Source: sessionID,
	}

	msgs, meta, err := a.registry.Load(ref)
	if err != nil {
		return nil, fmt.Errorf("loading recording %q: %w", sessionID, err)
	}

	detail := &generate.SessionDetail{
		SessionSummary: generate.SessionSummary{
			ID:        sessionID,
			Source:    sessionID,
			TurnCount: len(msgs),
			Timestamp: time.Now(),
		},
		Messages: msgs,
		// Recordings don't carry eval data.
		EvalResults:     nil,
		TurnEvalResults: nil,
	}

	if meta != nil {
		detail.Tags = meta.Tags
		if meta.SessionID != "" {
			detail.ID = meta.SessionID
		}
		if len(meta.Timestamps) > 0 {
			detail.Timestamp = meta.Timestamps[0]
		}
	}

	return detail, nil
}

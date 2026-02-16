package adaptersdk

import (
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// percentMultiplier converts a 0.0-1.0 fraction to a 0-100 percentage.
const percentMultiplier = 100

// maxPercent is the upper bound for a valid percentage.
const maxPercent = 100

// ParsePack deserializes a .pack.json byte slice into a prompt.Pack struct.
func ParsePack(packJSON []byte) (*prompt.Pack, error) {
	var pack prompt.Pack
	if err := json.Unmarshal(packJSON, &pack); err != nil {
		return nil, err
	}
	return &pack, nil
}

// ProgressReporter wraps a deploy.ApplyCallback and provides convenient
// methods for emitting progress, resource, and error events.
type ProgressReporter struct {
	callback deploy.ApplyCallback
}

// NewProgressReporter creates a ProgressReporter that sends events through
// the given ApplyCallback.
func NewProgressReporter(callback deploy.ApplyCallback) *ProgressReporter {
	return &ProgressReporter{callback: callback}
}

// Progress emits a progress event with a human-readable message and a
// completion percentage (0.0 to 1.0).
func (pr *ProgressReporter) Progress(message string, pct float64) error {
	return pr.callback(&deploy.ApplyEvent{
		Type:    "progress",
		Message: formatProgress(message, pct),
	})
}

// Resource emits a resource result event.
func (pr *ProgressReporter) Resource(result *deploy.ResourceResult) error {
	return pr.callback(&deploy.ApplyEvent{
		Type:     "resource",
		Resource: result,
	})
}

// Error emits an error event.
func (pr *ProgressReporter) Error(err error) error {
	return pr.callback(&deploy.ApplyEvent{
		Type:    "error",
		Message: err.Error(),
	})
}

// formatProgress builds a progress message string that includes the
// percentage when it is within the valid 0-100 range.
func formatProgress(message string, pct float64) string {
	pctInt := int(pct * percentMultiplier)
	if pctInt < 0 || pctInt > maxPercent {
		return message
	}
	return fmt.Sprintf("%s (%d%%)", message, pctInt)
}

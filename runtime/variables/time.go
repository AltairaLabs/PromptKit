package variables

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// TimeProvider provides current time and date variables.
// Useful for prompts that need temporal context like "What day is it?"
// or time-sensitive instructions.
type TimeProvider struct {
	// Format is the time format string for current_time variable.
	// Defaults to time.RFC3339 if empty.
	Format string

	// Location specifies the timezone. Defaults to UTC if nil.
	Location *time.Location

	// nowFunc allows injecting a custom time source for testing.
	// If nil, time.Now() is used.
	nowFunc func() time.Time
}

// NewTimeProvider creates a TimeProvider with default settings (UTC, RFC3339 format).
func NewTimeProvider() *TimeProvider {
	return &TimeProvider{}
}

// NewTimeProviderWithLocation creates a TimeProvider for a specific timezone.
func NewTimeProviderWithLocation(loc *time.Location) *TimeProvider {
	return &TimeProvider{Location: loc}
}

// NewTimeProviderWithFormat creates a TimeProvider with a custom time format.
func NewTimeProviderWithFormat(format string) *TimeProvider {
	return &TimeProvider{Format: format}
}

// Name returns the provider identifier.
func (p *TimeProvider) Name() string {
	return "time"
}

// Provide returns time-related variables.
// Variables provided:
//   - current_time: Full timestamp in configured format
//   - current_date: Date in YYYY-MM-DD format
//   - current_year: Four-digit year
//   - current_month: Full month name (e.g., "January")
//   - current_weekday: Full weekday name (e.g., "Monday")
//   - current_hour: Hour in 24-hour format (00-23)
func (p *TimeProvider) Provide(ctx context.Context, state *statestore.ConversationState) (map[string]string, error) {
	now := p.now()

	if p.Location != nil {
		now = now.In(p.Location)
	}

	format := p.Format
	if format == "" {
		format = time.RFC3339
	}

	return map[string]string{
		"current_time":    now.Format(format),
		"current_date":    now.Format("2006-01-02"),
		"current_year":    now.Format("2006"),
		"current_month":   now.Month().String(),
		"current_weekday": now.Weekday().String(),
		"current_hour":    now.Format("15"),
	}, nil
}

// now returns the current time, using the injected nowFunc if available.
func (p *TimeProvider) now() time.Time {
	if p.nowFunc != nil {
		return p.nowFunc()
	}
	return time.Now()
}

// WithNowFunc sets a custom time source (primarily for testing).
func (p *TimeProvider) WithNowFunc(fn func() time.Time) *TimeProvider {
	p.nowFunc = fn
	return p
}

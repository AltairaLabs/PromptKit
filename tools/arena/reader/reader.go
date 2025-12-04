// Package reader provides abstract result reading interfaces for Arena.
package reader

import (
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// ResultReader provides abstract access to test result retrieval
// from various sources (filesystem, database, API, etc.)
type ResultReader interface {
	// ListResults returns metadata about available results (for browsing)
	ListResults() ([]ResultMetadata, error)

	// LoadResult loads a single result by ID
	LoadResult(runID string) (*statestore.RunResult, error)

	// LoadResults loads multiple results by IDs
	LoadResults(runIDs []string) ([]*statestore.RunResult, error)

	// LoadAllResults loads all available results
	LoadAllResults() ([]*statestore.RunResult, error)

	// SupportsFiltering returns true if the reader can filter on the server side
	SupportsFiltering() bool

	// FilterResults returns filtered result metadata (if supported)
	FilterResults(filter *ResultFilter) ([]ResultMetadata, error)
}

// ResultMetadata contains summary information about a result
// (used for displaying in file browser without loading full result)
type ResultMetadata struct {
	RunID     string        `json:"run_id"`
	Scenario  string        `json:"scenario"`
	Provider  string        `json:"provider"`
	Region    string        `json:"region"`
	StartTime time.Time     `json:"start_time"`
	Duration  time.Duration `json:"duration"`
	Status    string        `json:"status"` // "success", "failed"
	Error     string        `json:"error,omitempty"`
	Cost      float64       `json:"cost"`
	Location  string        `json:"location"` // Source-specific location (file path, DB ID, etc.)
}

// ResultFilter defines filtering criteria for results
type ResultFilter struct {
	Scenarios []string   `json:"scenarios,omitempty"`
	Providers []string   `json:"providers,omitempty"`
	Regions   []string   `json:"regions,omitempty"`
	Status    []string   `json:"status,omitempty"` // "success", "failed"
	StartDate *time.Time `json:"start_date,omitempty"`
	EndDate   *time.Time `json:"end_date,omitempty"`
}

// MatchesFilter checks if metadata matches the given filter
func (m *ResultMetadata) MatchesFilter(filter *ResultFilter) bool {
	return m.matchesStringFilters(filter) && m.matchesDateRange(filter)
}

// matchesStringFilters checks if metadata matches string-based filters
func (m *ResultMetadata) matchesStringFilters(filter *ResultFilter) bool {
	if len(filter.Scenarios) > 0 && !contains(filter.Scenarios, m.Scenario) {
		return false
	}
	if len(filter.Providers) > 0 && !contains(filter.Providers, m.Provider) {
		return false
	}
	if len(filter.Regions) > 0 && !contains(filter.Regions, m.Region) {
		return false
	}
	if len(filter.Status) > 0 && !contains(filter.Status, m.Status) {
		return false
	}
	return true
}

// matchesDateRange checks if metadata matches date range filters
func (m *ResultMetadata) matchesDateRange(filter *ResultFilter) bool {
	if filter.StartDate != nil && m.StartTime.Before(*filter.StartDate) {
		return false
	}
	if filter.EndDate != nil && m.StartTime.After(*filter.EndDate) {
		return false
	}
	return true
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Package engine defines future-proofing interfaces for PromptKit
// These interfaces will be implemented in future versions to support
// distributed execution, optimization, and advanced features.
package engine

import (
	"context"
	"time"
)

// WorkUnit represents a single unit of work in distributed execution
// Planned for v0.2.0 - Distributed Execution Support
type WorkUnit interface {
	// ID returns unique identifier for this work unit
	ID() string

	// Execute performs the work unit and returns results
	Execute(ctx context.Context) (WorkResult, error)

	// Dependencies returns work units that must complete before this one
	Dependencies() []string

	// Priority returns execution priority (higher number = higher priority)
	Priority() int

	// EstimatedDuration returns expected execution time
	EstimatedDuration() time.Duration
}

// WorkResult represents the result of executing a WorkUnit
type WorkResult interface {
	// WorkUnitID returns the ID of the work unit that produced this result
	WorkUnitID() string

	// Success indicates if the work unit completed successfully
	Success() bool

	// Data returns the result data (conversation, analysis, etc.)
	Data() interface{}

	// Error returns any error that occurred during execution
	Error() error

	// Metadata returns additional metadata about the execution
	Metadata() map[string]interface{}
}

// ResultSink handles storage and processing of work results
// Planned for v0.2.0 - Distributed Execution Support
type ResultSink interface {
	// Store saves a work result
	Store(ctx context.Context, result WorkResult) error

	// Retrieve gets a work result by work unit ID
	Retrieve(ctx context.Context, workUnitID string) (WorkResult, error)

	// Query searches for results matching criteria
	Query(ctx context.Context, criteria map[string]interface{}) ([]WorkResult, error)

	// Subscribe to real-time result updates
	Subscribe(ctx context.Context, filter func(WorkResult) bool) (<-chan WorkResult, error)
}

// WorkSource generates work units for execution
// Planned for v0.2.0 - Distributed Execution Support
type WorkSource interface {
	// Generate creates work units from configuration
	Generate(ctx context.Context, config interface{}) ([]WorkUnit, error)

	// Stream provides work units as they become available
	Stream(ctx context.Context, config interface{}) (<-chan WorkUnit, error)

	// EstimateWorkload returns expected number of work units
	EstimateWorkload(ctx context.Context, config interface{}) (int, error)
}

// DistributedExecutor coordinates distributed execution of work units
// Planned for v0.2.0 - Distributed Execution Support
type DistributedExecutor interface {
	// Execute runs work units across distributed workers
	Execute(ctx context.Context, units []WorkUnit) error

	// Status returns current execution status
	Status() ExecutionStatus

	// Workers returns information about available workers
	Workers() []WorkerInfo

	// Scale adjusts the number of workers
	Scale(ctx context.Context, workerCount int) error
}

// ExecutionStatus represents the state of distributed execution
type ExecutionStatus struct {
	Total     int           `json:"total"`      // Total work units
	Completed int           `json:"completed"`  // Completed work units
	Running   int           `json:"running"`    // Currently executing
	Failed    int           `json:"failed"`     // Failed work units
	StartTime time.Time     `json:"start_time"` // Execution start time
	Duration  time.Duration `json:"duration"`   // Total duration so far
	Workers   int           `json:"workers"`    // Active worker count
}

// WorkerInfo contains information about a distributed worker
type WorkerInfo struct {
	ID       string                 `json:"id"`        // Worker identifier
	Status   string                 `json:"status"`    // online, offline, busy
	Capacity int                    `json:"capacity"`  // Max concurrent work units
	Current  int                    `json:"current"`   // Current work units
	LastSeen time.Time              `json:"last_seen"` // Last heartbeat
	Metadata map[string]interface{} `json:"metadata"`  // Additional worker info
}

// Package engine provides the core execution engine for PromptArena test runs.
package engine

import "time"

// ExecutionObserver receives callbacks during test execution for monitoring and UI updates.
// Implementations must be goroutine-safe as callbacks are invoked from concurrent worker goroutines.
//
// The observer pattern enables real-time progress tracking without coupling the engine
// to specific UI implementations. Multiple observers can be chained if needed.
//
// Example usage:
//
//	type MyObserver struct{}
//
//	func (o *MyObserver) OnRunStarted(runID, scenario, provider, region string) {
//	    fmt.Printf("Starting: %s/%s/%s\n", provider, scenario, region)
//	}
//
//	func (o *MyObserver) OnRunCompleted(runID string, duration time.Duration, cost float64) {
//	    fmt.Printf("Completed %s in %v ($%.4f)\n", runID, duration, cost)
//	}
//
//	func (o *MyObserver) OnRunFailed(runID string, err error) {
//	    fmt.Printf("Failed %s: %v\n", runID, err)
//	}
//
//	engine.SetObserver(&MyObserver{})
type ExecutionObserver interface {
	// OnRunStarted is called when a test run begins execution.
	// This is invoked from a worker goroutine and must be thread-safe.
	//
	// Parameters:
	//   - runID: Unique identifier for this run
	//   - scenario: Scenario ID being executed
	//   - provider: Provider ID being used
	//   - region: Region for the run
	OnRunStarted(runID, scenario, provider, region string)

	// OnRunCompleted is called when a test run finishes successfully.
	// This is invoked from a worker goroutine and must be thread-safe.
	//
	// Parameters:
	//   - runID: Unique identifier for this run
	//   - duration: Total execution time
	//   - cost: Total cost in USD
	OnRunCompleted(runID string, duration time.Duration, cost float64)

	// OnRunFailed is called when a test run fails with an error.
	// This is invoked from a worker goroutine and must be thread-safe.
	//
	// Parameters:
	//   - runID: Unique identifier for this run
	//   - err: Error that caused the failure
	OnRunFailed(runID string, err error)

	// OnTurnStarted is called when a turn begins execution.
	// turnIndex is zero-based.
	OnTurnStarted(runID string, turnIndex int, role string, scenario string)

	// OnTurnCompleted is called when a turn finishes (success or error).
	// err is nil on success.
	OnTurnCompleted(runID string, turnIndex int, role string, scenario string, err error)
}

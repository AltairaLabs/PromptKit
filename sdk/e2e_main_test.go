//go:build e2e

package sdk

import (
	"os"
	"testing"
)

// TestMain is the entry point for e2e tests.
// It runs all tests and prints a cost report at the end.
func TestMain(m *testing.M) {
	// Run all tests
	exitCode := m.Run()

	// Print cost report after all tests complete
	GetCostTracker().PrintReport()

	os.Exit(exitCode)
}

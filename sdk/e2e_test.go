//go:build e2e

// End-to-end tests for the SDK.
//
// These tests verify the complete integration path from conversation
// through pipeline stages to event emission. They use mock providers
// to avoid real API calls while testing the full integration.
//
// # Running E2E Tests
//
// Run all e2e tests:
//
//	go test -tags=e2e ./sdk/...
//
// Run specific test suites:
//
//	go test -tags=e2e -run TestE2E_Events ./sdk/...
//
// Run with verbose output:
//
//	go test -tags=e2e -v ./sdk/...
//
// # Test Organization
//
// E2E tests are organized by feature area:
//
//   - e2e_events_test.go: Event emission tests (provider calls, streaming tokens)
//   - e2e_helpers_test.go: Shared test utilities and conversation factories
//
// Future test files:
//   - e2e_streaming_test.go: Streaming mode tests
//   - e2e_tools_test.go: Tool execution tests
//   - e2e_validation_test.go: Response validation tests
//
// # CI Pipeline Integration
//
// These tests are designed to run as a separate CI pipeline stage:
//
//	# In CI workflow
//	- name: Run E2E Tests
//	  run: go test -tags=e2e -v ./sdk/...
//
// The build tag ensures these tests don't run during regular `go test ./...`
// and can be explicitly included when needed.
package sdk

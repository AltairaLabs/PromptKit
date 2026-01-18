//go:build e2e

// End-to-end tests for the PromptKit SDK.
//
// These tests verify the complete integration path from conversation
// through pipeline stages across multiple providers. They support both
// mock providers (for fast CI testing) and real providers (for integration
// verification).
//
// # Running E2E Tests
//
// Basic usage:
//
//	go test -tags=e2e ./sdk/...
//
// Using make (recommended):
//
//	make test-e2e           # All tests with available providers
//	make test-e2e-mock      # Mock provider only (no API keys)
//	make test-e2e-coverage  # With coverage + HTML report
//	make test-e2e-ci        # CI mode (JSON + JUnit output)
//
// Run specific suites:
//
//	make test-e2e-suite SUITE=text
//	make test-e2e-suite SUITE=vision
//	make test-e2e-suite SUITE=tools
//	make test-e2e-suite SUITE=events
//
// Run specific providers:
//
//	make test-e2e-provider PROVIDER=openai
//	make test-e2e-provider PROVIDER=anthropic
//
// # Environment Variables
//
//	OPENAI_API_KEY      Enable OpenAI provider tests
//	ANTHROPIC_API_KEY   Enable Anthropic provider tests
//	GEMINI_API_KEY      Enable Gemini provider tests (or GOOGLE_API_KEY)
//	E2E_PROVIDERS       Limit to specific providers (comma-separated)
//	E2E_SKIP_PROVIDERS  Skip specific providers (comma-separated)
//	E2E_CONFIG          Path to JSON config file
//	E2E_TEST_IMAGE      Path to test image for vision tests
//
// # Test Organization
//
// Tests are organized by feature area and run in a matrix across providers:
//
//	e2e_config_test.go     - Configuration and provider matrix
//	e2e_providers_test.go  - Provider setup helpers
//	e2e_helpers_test.go    - Mock-based test utilities
//	e2e_text_test.go       - Text conversation tests
//	e2e_vision_test.go     - Vision/multimodal tests
//	e2e_tools_test.go      - Tool execution tests
//	e2e_events_test.go     - Event emission tests
//
// # Provider Capabilities Matrix
//
//	Provider      | text | streaming | vision | audio | video | tools | json | realtime
//	--------------|------|-----------|--------|-------|-------|-------|------|----------
//	openai        |  ✓   |     ✓     |   ✓    |       |       |   ✓   |  ✓   |
//	openai-rt     |      |           |        |   ✓   |       |       |      |    ✓
//	anthropic     |  ✓   |     ✓     |   ✓    |       |       |   ✓   |  ✓   |
//	gemini        |  ✓   |     ✓     |   ✓    |       |   ✓   |   ✓   |  ✓   |
//	gemini-rt     |      |           |        |   ✓   |   ✓   |       |      |    ✓
//	mock          |  ✓   |     ✓     |        |       |       |   ✓   |      |
//
// # CI Pipeline Integration
//
// For CI, use make test-e2e-ci which outputs:
//   - coverage.out: Coverage profile
//   - results.json: Test results in JSON format
//   - junit.xml: JUnit XML for test reporting
//
// Example GitHub Actions workflow:
//
//   - name: Run E2E Tests
//     run: make test-e2e-ci
//     env:
//     OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
//     ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
//
//   - name: Upload Results
//     uses: actions/upload-artifact@v4
//     with:
//     name: e2e-results
//     path: sdk/e2e-results/
//
// # Adding New Tests
//
// To add tests for a new feature:
//
//  1. Create e2e_<feature>_test.go
//  2. Use RunForProviders() for matrix testing
//  3. Use RequireCapability() to filter providers
//  4. Add capability to e2e_config_test.go if needed
//
// Example:
//
//	func TestE2E_Feature_Something(t *testing.T) {
//	    RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
//	        conv := NewProviderConversation(t, provider)
//	        defer conv.Close()
//	        // ... test code
//	    })
//	}
package sdk

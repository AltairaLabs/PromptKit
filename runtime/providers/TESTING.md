# Provider Contract Testing

## Overview

This package implements a comprehensive contract testing framework for all LLM provider implementations. Contract tests ensure that all providers meet the minimum requirements of the `Provider` interface and behave consistently across the system.

**The contract tests would have caught the production latency bug before it shipped.**

## Quick Start

```bash
# Run all contract tests (offline tests only, API tests skip without keys)
cd runtime/providers
go test -v -run "Contract" -timeout 120s

# Run with OpenAI API (requires OPENAI_API_KEY)
go test -v -run "TestOpenAI.*Contract" -timeout 120s

# Run specific latency regression tests
go test -v -run "ChatWithToolsLatency" -timeout 120s

# Run with all provider APIs (may incur costs!)
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GEMINI_API_KEY="..."
go test -v -run "Contract" -timeout 120s
```

## What Happened (The Bug)

In production, we discovered that assistant messages in self-play scenarios were missing the `latency_ms` field in JSON output. Investigation revealed:

1. **Root Cause**: `OpenAIToolProvider.ChatWithTools()` didn't track or set the `Latency` field
2. **Why it disappeared**: The `latency_ms` JSON field has `omitempty` tag, so `Latency=0` was omitted from output
3. **Impact**: No latency data for any tool-enabled OpenAI requests (the most common case in production)
4. **Other providers**: Claude and Gemini already had latency tracking in their ChatWithTools implementations

## The Solution

**Contract testing** - A reusable test suite that validates all critical aspects of the Provider interface:

### Critical Requirements Tested

1. **✅ Chat() returns non-zero latency** - Would have caught the bug immediately
2. **✅ ChatWithTools() returns non-zero latency** - Specific test for tool-enabled requests  
3. **✅ Chat() returns cost information** - Ensures billing data is present
4. **✅ Chat() returns non-empty responses** - Basic functionality check
5. **✅ CalculateCost() returns reasonable values** - Validates cost calculations
6. **✅ SupportsStreaming() matches actual capability** - Ensures capability flags are correct
7. **✅ ChatStream() returns latency** - Validates streaming latency tracking
8. **✅ Provider returns non-empty ID** - Basic interface requirement

## Test Coverage Status

All major providers have comprehensive contract test coverage:

### OpenAI

- `TestOpenAIProvider_Contract` - Full contract suite for base provider
- `TestOpenAIToolProvider_ChatWithToolsLatency` - Specific regression test for the latency bug
- Requires `OPENAI_API_KEY` for API tests

### Claude  

- `TestClaudeProvider_Contract` - Full contract suite for base provider
- `TestClaudeToolProvider_Contract` - Full contract suite including ChatWithTools
- `TestClaudeToolProvider_ChatWithToolsLatency` - Latency regression test
- Requires `ANTHROPIC_API_KEY` for API tests

### Gemini

- `TestGeminiProvider_Contract` - Full contract suite for base provider  
- `TestGeminiToolProvider_Contract` - Full contract suite including ChatWithTools
- `TestGeminiToolProvider_ChatWithToolsLatency` - Latency regression test
- Requires `GEMINI_API_KEY` for API tests

## How to Use Contract Tests

## The Solution: Contract Testing

The `provider_contract_test.go` file defines a comprehensive test suite that **every provider must pass**. These tests verify:

### Critical Requirements
- ✅ **Latency is always non-zero** (catches the production bug!)
- ✅ Cost information is populated correctly
- ✅ Response content is non-empty
- ✅ Token counts are reasonable
- ✅ Streaming works (if supported)

### How to Use

For each provider implementation, create a contract test file:

```go
// openai_contract_test.go
package providers

import (
    "os"
    "testing"
)

func TestOpenAIProvider_Contract(t *testing.T) {
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        t.Skip("Skipping - OPENAI_API_KEY not set")
    }

    provider := NewOpenAIProvider(
        "openai-test",
        "gpt-4o-mini",
        "",
        ProviderDefaults{Temperature: 0.7, MaxTokens: 100},
        false,
    )
    defer provider.Close()

    // Run ALL contract tests
    RunProviderContractTests(t, ProviderContractTests{
        Provider:                  provider,
        SupportsToolsExpected:     false,
        SupportsStreamingExpected: true,
    })
}
```

## Test-Driven Development Workflow

### Adding a New Provider

1. **Create the provider struct** (e.g., `anthropic.go`)
2. **Create the contract test FIRST** (e.g., `anthropic_contract_test.go`)
3. **Run tests** - they will fail
4. **Implement the provider** until all contract tests pass
5. **Add provider-specific tests** for unique features

### Fixing a Bug

1. **Add a failing test** that reproduces the bug
2. **Fix the implementation**
3. **Verify the test passes**
4. **Run the full contract suite** to prevent regressions

## Running the Tests

### Basic Usage

```bash
# Navigate to providers directory
cd runtime/providers

# Run ALL contract tests (offline tests run, API tests skip without keys)
go test -v -run "Contract" -timeout 120s

# Run with shorter output (just pass/fail summary)
go test -run "Contract" -timeout 120s
```

**Expected Output:**

- All offline tests pass (ID, CalculateCost, SupportsStreaming)
- API tests skip gracefully with "SKIP: no API key" messages
- Total time: ~5-10 seconds

### Running with API Keys

```bash
# OpenAI only (the one with the fixed bug)
export OPENAI_API_KEY="sk-proj-..."
go test -v -run "TestOpenAI.*Contract" -timeout 120s

# Claude only
export ANTHROPIC_API_KEY="sk-ant-..."
go test -v -run "TestClaude.*Contract" -timeout 120s

# Gemini only
export GEMINI_API_KEY="..."
go test -v -run "TestGemini.*Contract" -timeout 120s

# All providers (may incur API costs!)
export OPENAI_API_KEY="sk-proj-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GEMINI_API_KEY="..."
go test -v -run "Contract" -timeout 120s
```

**Note:** API tests make real requests and may incur small costs (typically < $0.01 per test run).

### Running Specific Tests

```bash
# Just the latency regression tests (verifies the bug fix)
go test -v -run "ChatWithToolsLatency" -timeout 120s

# Just the OpenAI tool provider latency test (the critical one)
go test -v -run "TestOpenAIToolProvider_ChatWithToolsLatency" -timeout 60s

# Just cost calculation tests (offline only)
go test -v -run "CalculateCost" -timeout 30s

# Just streaming tests
go test -v -run "ChatStream" -timeout 120s
```

### Continuous Integration

For CI/CD pipelines, run without API keys to validate offline behavior:

```bash
# CI-friendly: fast, no API costs, validates structure
go test -run "Contract" -timeout 60s -short

# Expected: All tests pass or skip gracefully
# Exit code 0 = success (tests pass or skip)
# Exit code 1 = failure (actual test failures)
```

### Interpreting Results

**PASS** - Test executed successfully, all assertions passed

**SKIP** - Test skipped (usually means no API key or API error), this is OK!

**FAIL** - Test executed but assertions failed - investigate immediately

Example successful output:

```text
TestOpenAIProvider_Contract (3.39s)
  Contract_ID .......................................... PASS
  Contract_Chat_ReturnsLatency ......................... PASS (0.86s)
  Contract_ChatWithTools_ReturnsLatency ................ SKIP
  Contract_Chat_ReturnsCostInfo ........................ PASS (0.43s)
  ...
```

## Current Provider Coverage

## Adding New Contract Tests

If you discover a new requirement that ALL providers should meet:

1. Add the test to `provider_contract_test.go`
2. Update all provider implementations to pass
3. Document the new requirement here

## Benefits

- 🐛 **Catches bugs early** - Before they reach production
- 📊 **Ensures consistency** - All providers behave the same way
- 🔄 **Enables refactoring** - Change internals safely
- 📚 **Living documentation** - Tests show how providers should work
- ⚡ **Faster debugging** - Failing tests pinpoint the exact issue

## Debugging Tips

If a contract test fails:

1. Check the test output for the exact assertion that failed
2. Use the Debug Middleware (see `runtime/pipeline/middleware/debug.go`)
3. Add logging to your provider implementation
4. Compare against a working provider (e.g., OpenAI)

## Related Files

- `provider_contract_test.go` - The main contract test suite
- `provider_latency_test.go` - Middleware tests using mocks
- `provider_latency_bug_test.go` - Bug reproduction tests
- `debug.go` - Debug middleware for tracing execution
- `debug_test.go` - Debug middleware tests

## Philosophy

> "If it's not tested, it's broken."

Contract testing ensures that interface promises are kept. It's not just about finding bugs—it's about **preventing entire classes of bugs** through systematic validation.

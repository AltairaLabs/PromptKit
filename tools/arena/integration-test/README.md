# Arena Integration Tests

This directory contains integration tests for the Arena testing framework, designed to validate CI/CD integration and multi-format output generation.

## Overview

The integration tests verify that Arena can:
- Execute test scenarios with both passing and failing cases
- Generate multiple output formats (JSON, JUnit XML, HTML) 
- Handle metadata-driven mock provider responses
- Provide proper error reporting and assertion validation

## Directory Structure

```
integration-test/
├── README.md                    # This file
├── arena.yaml                   # Main Arena configuration
├── mock-config.yaml             # Mock provider response definitions
├── mock-config-with-errors.yaml # Error scenario configurations
├── prompts/
│   └── integration-test.yaml    # Prompt configuration for tests
├── providers/
│   └── mock-provider.yaml       # Provider configuration
└── scenarios/
    ├── successful-test.yaml     # Test scenario that should pass
    ├── failing-test.yaml        # Test scenario that should fail  
    └── timeout-test.yaml        # Test scenario for timeout handling
```

## Test Scenarios

### 1. Successful Test (`successful-test.yaml`)
- **Purpose**: Validates successful test execution and output generation
- **Expected Result**: All assertions should pass
- **Use Case**: Confirms normal CI/CD integration workflow

### 2. Failing Test (`failing-test.yaml`)  
- **Purpose**: Tests error handling and assertion failure reporting
- **Expected Result**: Assertions should fail as designed
- **Use Case**: Validates proper error reporting in CI/CD pipelines

### 3. Timeout Test (`timeout-test.yaml`)
- **Purpose**: Tests timeout handling and resource management
- **Expected Result**: Should handle timeouts gracefully
- **Use Case**: Ensures robust behavior under resource constraints

## Mock Provider Configuration

The tests use a mock provider with predefined responses defined in `mock-config.yaml`:
- **Scenario-based responses**: Different responses for each test scenario
- **Turn-based progression**: Specific responses for each conversation turn
- **Predictable behavior**: Ensures consistent test results

## Running the Tests

### Prerequisites

1. **Build Arena**: Ensure the Arena tool is built and available
   ```bash
   cd /path/to/promptkit-public
   make build
   ```

2. **Set up environment**: Make sure you're in the integration test directory
   ```bash
   cd tools/arena/integration-test
   ```

### Basic Test Execution

Run all integration tests:
```bash
../../../bin/promptarena run --config arena.yaml --mock-config mock-config.yaml --mock-provider
```

### Advanced Options

**Verbose output for debugging:**
```bash
../../../bin/promptarena run --config arena.yaml --mock-config mock-config.yaml --mock-provider --verbose
```

**Run specific scenario:**
```bash
../../../bin/promptarena run --config arena.yaml --mock-config mock-config.yaml --mock-provider --scenario successful-test
```

**Custom output directory:**
```bash
../../../bin/promptarena run --config arena.yaml --mock-config mock-config.yaml --mock-provider --out-dir ./test-results
```

**Debug mode with detailed logging:**
```bash
../../../bin/promptarena debug --config arena.yaml --mock-config mock-config.yaml
```

## Output Formats

The tests generate multiple output formats for CI/CD integration:

### 1. JSON Output (`output/results.json`)
- Detailed test results with metadata
- Machine-readable format for automation
- Includes timing, costs, and detailed assertions

### 2. JUnit XML Output (`output/junit.xml`)
- Standard JUnit format for CI/CD integration
- Compatible with Jenkins, GitLab CI, GitHub Actions
- Provides test suite and individual test case results

### 3. HTML Report (`output/report.html`)
- Human-readable test report
- Interactive interface for result analysis
- Includes charts, timelines, and detailed breakdowns

## Interpreting Results

### Successful Run
- Exit code: 0
- All scenarios execute without critical errors
- Output files generated in specified directory
- HTML report shows green status indicators

### Failed Run  
- Exit code: 1 (for assertion failures) or 2 (for system errors)
- Failed assertions clearly marked in output
- Error messages provide debugging information
- HTML report highlights failed test cases

### Expected Behavior

**Normal execution should show:**
- ✅ `successful-test` scenario passes
- ❌ `failing-test` scenario fails (by design)
- ⚠️  `timeout-test` scenario may warn about timeouts

This mixed result pattern validates that both success and failure cases are properly handled.

## Troubleshooting

### Common Issues

**1. Mock provider not responding correctly:**
```bash
# Verify mock configuration is loaded
../../../bin/promptarena debug --config arena.yaml --mock-config mock-config.yaml
```

**2. Output files not generated:**
```bash
# Check permissions and create output directory
mkdir -p output
chmod 755 output
```

**3. Assertion failures beyond expected:**
```bash
# Run with verbose logging to see detailed execution
../../../bin/promptarena run --config arena.yaml --mock-config mock-config.yaml --mock-provider --verbose --log-level debug
```

### Validation Commands

**Check configuration syntax:**
```bash
../../../bin/promptarena validate --config arena.yaml
```

**List available scenarios:**
```bash
../../../bin/promptarena list scenarios --config arena.yaml
```

**Verify mock responses:**
```bash
../../../bin/promptarena debug --config arena.yaml --mock-config mock-config.yaml --scenario successful-test
```

## Integration with CI/CD

### GitHub Actions Example
```yaml
- name: Run Arena Integration Tests
  run: |
    cd tools/arena/integration-test
    ../../../bin/promptarena run --config arena.yaml --mock-config mock-config.yaml --mock-provider
    
- name: Upload Test Results
  uses: actions/upload-artifact@v3
  if: always()
  with:
    name: arena-test-results
    path: tools/arena/integration-test/output/
```

### Jenkins Integration
```groovy
stage('Arena Integration Tests') {
    steps {
        dir('tools/arena/integration-test') {
            sh '../../../bin/promptarena run --config arena.yaml --mock-config mock-config.yaml --mock-provider'
        }
    }
    post {
        always {
            publishTestResults testResultsPattern: 'tools/arena/integration-test/output/junit.xml'
            archiveArtifacts artifacts: 'tools/arena/integration-test/output/**/*'
        }
    }
}
```

## Metadata Extension Testing

These integration tests specifically validate the new **metadata extension feature**:

- **Scenario Context**: Mock provider receives scenario information via `ChatRequest.Metadata`
- **Turn Tracking**: Turn numbers passed through metadata for stateful responses  
- **Provider Integration**: Tests the `ExecutionContext.Metadata` → `ChatRequest.Metadata` flow
- **Arena Compatibility**: Ensures Arena middleware works correctly with metadata system

### Metadata Flow Validation

The tests verify this metadata flow:
```
Arena Scenario → ExecutionContext.Metadata → ProviderMiddleware → ChatRequest.Metadata → MockProvider
```

Each test scenario includes metadata that the mock provider uses to determine appropriate responses, validating the complete metadata pipeline implemented in the recent feature work.

## Support

For issues with integration tests:
1. Check the troubleshooting section above
2. Review Arena documentation in `/docs`
3. Examine verbose output logs for detailed error information
4. Validate configuration files for syntax errors

---

**Last Updated**: November 2025  
**Compatibility**: Arena v1.0.0+  
**Metadata Extension**: Fully supported
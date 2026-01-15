#!/usr/bin/env bash
#
# E2E Test Runner for PromptKit SDK
#
# Runs e2e tests with coverage, timing, and multiple output formats.
# Designed for both local development and CI pipelines.
#
# Usage:
#   ./scripts/run-e2e-tests.sh [options]
#
# Options:
#   --providers=LIST    Comma-separated list of providers (e.g., "openai,anthropic")
#   --skip=LIST         Comma-separated providers to skip
#   --suite=NAME        Run specific test suite (text, vision, tools, events)
#   --coverage          Generate coverage report
#   --json              Output JSON results
#   --junit             Output JUnit XML (for CI)
#   --html              Generate HTML coverage report
#   --verbose           Verbose output
#   --parallel=N        Max parallel tests (default: 4)
#   --timeout=DURATION  Test timeout (default: 5m)
#   --mock-only         Only run mock provider tests
#   --real-only         Only run real provider tests (skip mock)
#   --help              Show this help
#
# Environment Variables:
#   OPENAI_API_KEY      Enable OpenAI provider tests
#   ANTHROPIC_API_KEY   Enable Anthropic provider tests
#   GEMINI_API_KEY      Enable Gemini provider tests (or GOOGLE_API_KEY)
#   E2E_PROVIDERS       Override provider list
#   E2E_SKIP_PROVIDERS  Skip specific providers
#
# Examples:
#   ./scripts/run-e2e-tests.sh --coverage --html
#   ./scripts/run-e2e-tests.sh --providers=openai --suite=text
#   ./scripts/run-e2e-tests.sh --junit --json  # For CI
#   E2E_PROVIDERS=mock ./scripts/run-e2e-tests.sh  # Mock only

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Default settings
COVERAGE=false
JSON_OUTPUT=false
JUNIT_OUTPUT=false
HTML_REPORT=false
VERBOSE=false
PARALLEL=4
TIMEOUT="5m"
SUITE=""
PROVIDERS=""
SKIP_PROVIDERS=""
MOCK_ONLY=false
REAL_ONLY=false

# Output directory
OUTPUT_DIR="sdk/e2e-results"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --providers=*)
            PROVIDERS="${1#*=}"
            shift
            ;;
        --skip=*)
            SKIP_PROVIDERS="${1#*=}"
            shift
            ;;
        --suite=*)
            SUITE="${1#*=}"
            shift
            ;;
        --coverage)
            COVERAGE=true
            shift
            ;;
        --json)
            JSON_OUTPUT=true
            shift
            ;;
        --junit)
            JUNIT_OUTPUT=true
            shift
            ;;
        --html)
            HTML_REPORT=true
            COVERAGE=true  # HTML needs coverage
            shift
            ;;
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --parallel=*)
            PARALLEL="${1#*=}"
            shift
            ;;
        --timeout=*)
            TIMEOUT="${1#*=}"
            shift
            ;;
        --mock-only)
            MOCK_ONLY=true
            shift
            ;;
        --real-only)
            REAL_ONLY=true
            shift
            ;;
        --help|-h)
            head -50 "$0" | grep -E "^#" | sed 's/^# //' | sed 's/^#//'
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Set environment variables for provider filtering
if [[ -n "$PROVIDERS" ]]; then
    export E2E_PROVIDERS="$PROVIDERS"
fi

if [[ -n "$SKIP_PROVIDERS" ]]; then
    export E2E_SKIP_PROVIDERS="$SKIP_PROVIDERS"
fi

if [[ "$MOCK_ONLY" == true ]]; then
    export E2E_PROVIDERS="mock"
fi

if [[ "$REAL_ONLY" == true ]]; then
    export E2E_SKIP_PROVIDERS="${E2E_SKIP_PROVIDERS:+$E2E_SKIP_PROVIDERS,}mock"
fi

# Print header
echo -e "${BOLD}${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}${BLUE}  PromptKit SDK E2E Tests${NC}"
echo -e "${BOLD}${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""

# Show configuration
echo -e "${CYAN}Configuration:${NC}"
echo -e "  Output directory: ${OUTPUT_DIR}"
echo -e "  Timeout: ${TIMEOUT}"
echo -e "  Parallel: ${PARALLEL}"
[[ -n "${E2E_PROVIDERS:-}" ]] && echo -e "  Providers: ${E2E_PROVIDERS}"
[[ -n "${E2E_SKIP_PROVIDERS:-}" ]] && echo -e "  Skip: ${E2E_SKIP_PROVIDERS}"
[[ -n "$SUITE" ]] && echo -e "  Suite: ${SUITE}"
echo ""

# Show available providers
echo -e "${CYAN}Provider Availability:${NC}"
[[ -n "${OPENAI_API_KEY:-}" ]] && echo -e "  ${GREEN}✓${NC} OpenAI" || echo -e "  ${YELLOW}○${NC} OpenAI (no API key)"
[[ -n "${ANTHROPIC_API_KEY:-}" ]] && echo -e "  ${GREEN}✓${NC} Anthropic" || echo -e "  ${YELLOW}○${NC} Anthropic (no API key)"
[[ -n "${GEMINI_API_KEY:-}" || -n "${GOOGLE_API_KEY:-}" ]] && echo -e "  ${GREEN}✓${NC} Gemini" || echo -e "  ${YELLOW}○${NC} Gemini (no API key)"
echo -e "  ${GREEN}✓${NC} Mock (always available)"
echo ""

# Build test command
# Always filter to TestE2E tests to avoid running regular unit tests
TEST_CMD="go test -tags=e2e"

# Add test pattern - default to TestE2E prefix, or specific suite
if [[ -n "$SUITE" ]]; then
    case "$SUITE" in
        text)    TEST_CMD="$TEST_CMD -run TestE2E_Text" ;;
        vision)  TEST_CMD="$TEST_CMD -run TestE2E_Vision" ;;
        tools)   TEST_CMD="$TEST_CMD -run TestE2E_Tools" ;;
        events)  TEST_CMD="$TEST_CMD -run TestE2E_Events" ;;
        *)       TEST_CMD="$TEST_CMD -run $SUITE" ;;
    esac
else
    # Default: run all e2e tests (those starting with TestE2E)
    TEST_CMD="$TEST_CMD -run TestE2E"
fi

# Add timeout
TEST_CMD="$TEST_CMD -timeout $TIMEOUT"

# Add parallel
TEST_CMD="$TEST_CMD -parallel $PARALLEL"

# Add verbose
if [[ "$VERBOSE" == true ]]; then
    TEST_CMD="$TEST_CMD -v"
fi

# Add coverage
if [[ "$COVERAGE" == true ]]; then
    TEST_CMD="$TEST_CMD -coverprofile=$OUTPUT_DIR/coverage.out -covermode=atomic"
fi

# Check for gotestsum (better output formatting)
HAS_GOTESTSUM=false
if command -v gotestsum &> /dev/null; then
    HAS_GOTESTSUM=true
fi

# Run tests
echo -e "${BOLD}${BLUE}Running E2E Tests...${NC}"
echo ""

START_TIME=$(date +%s)
EXIT_CODE=0

if [[ "$JSON_OUTPUT" == true ]] || [[ "$JUNIT_OUTPUT" == true ]]; then
    # Use gotestsum for formatted output if available
    if [[ "$HAS_GOTESTSUM" == true ]]; then
        GOTESTSUM_CMD="gotestsum"

        if [[ "$JSON_OUTPUT" == true ]]; then
            GOTESTSUM_CMD="$GOTESTSUM_CMD --jsonfile $OUTPUT_DIR/results.json"
        fi

        if [[ "$JUNIT_OUTPUT" == true ]]; then
            GOTESTSUM_CMD="$GOTESTSUM_CMD --junitfile $OUTPUT_DIR/junit.xml"
        fi

        if [[ "$VERBOSE" == true ]]; then
            GOTESTSUM_CMD="$GOTESTSUM_CMD --format standard-verbose"
        else
            GOTESTSUM_CMD="$GOTESTSUM_CMD --format testdox"
        fi

        $GOTESTSUM_CMD -- ${TEST_CMD#go test } ./sdk/... 2>&1 | tee "$OUTPUT_DIR/output.log" || EXIT_CODE=$?
    else
        # Fallback to go test -json
        if [[ "$JSON_OUTPUT" == true ]]; then
            $TEST_CMD -json ./sdk/... 2>&1 | tee "$OUTPUT_DIR/results.json" || EXIT_CODE=$?
        else
            $TEST_CMD ./sdk/... 2>&1 | tee "$OUTPUT_DIR/output.log" || EXIT_CODE=$?
        fi

        if [[ "$JUNIT_OUTPUT" == true ]] && [[ "$HAS_GOTESTSUM" == false ]]; then
            echo -e "${YELLOW}Warning: gotestsum not installed, JUnit output not available${NC}"
            echo -e "${YELLOW}Install with: go install gotest.tools/gotestsum@latest${NC}"
        fi
    fi
else
    # Simple output
    if [[ "$HAS_GOTESTSUM" == true ]]; then
        gotestsum --format testdox -- ${TEST_CMD#go test } ./sdk/... 2>&1 | tee "$OUTPUT_DIR/output.log" || EXIT_CODE=$?
    else
        $TEST_CMD ./sdk/... 2>&1 | tee "$OUTPUT_DIR/output.log" || EXIT_CODE=$?
    fi
fi

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo ""
echo -e "${BOLD}${BLUE}═══════════════════════════════════════════════════════${NC}"

# Generate HTML coverage report
if [[ "$HTML_REPORT" == true ]] && [[ -f "$OUTPUT_DIR/coverage.out" ]]; then
    echo -e "${CYAN}Generating HTML coverage report...${NC}"
    go tool cover -html="$OUTPUT_DIR/coverage.out" -o "$OUTPUT_DIR/coverage.html"
    echo -e "  ${GREEN}✓${NC} HTML report: $OUTPUT_DIR/coverage.html"
fi

# Show coverage summary
if [[ "$COVERAGE" == true ]] && [[ -f "$OUTPUT_DIR/coverage.out" ]]; then
    echo ""
    echo -e "${CYAN}Coverage Summary:${NC}"
    go tool cover -func="$OUTPUT_DIR/coverage.out" | tail -1
fi

# Print summary
echo ""
echo -e "${CYAN}Results:${NC}"
echo -e "  Duration: ${DURATION}s"
echo -e "  Output: ${OUTPUT_DIR}/"

if [[ -f "$OUTPUT_DIR/results.json" ]]; then
    echo -e "  ${GREEN}✓${NC} JSON: $OUTPUT_DIR/results.json"
fi

if [[ -f "$OUTPUT_DIR/junit.xml" ]]; then
    echo -e "  ${GREEN}✓${NC} JUnit: $OUTPUT_DIR/junit.xml"
fi

if [[ -f "$OUTPUT_DIR/coverage.out" ]]; then
    echo -e "  ${GREEN}✓${NC} Coverage: $OUTPUT_DIR/coverage.out"
fi

if [[ -f "$OUTPUT_DIR/coverage.html" ]]; then
    echo -e "  ${GREEN}✓${NC} HTML Report: $OUTPUT_DIR/coverage.html"
fi

echo ""

if [[ $EXIT_CODE -eq 0 ]]; then
    echo -e "${BOLD}${GREEN}✓ All E2E tests passed!${NC}"
else
    echo -e "${BOLD}${RED}✗ Some E2E tests failed${NC}"
    echo -e "  See $OUTPUT_DIR/output.log for details"
fi

echo -e "${BOLD}${BLUE}═══════════════════════════════════════════════════════${NC}"

exit $EXIT_CODE

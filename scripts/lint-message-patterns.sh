#!/bin/bash
# lint-message-patterns.sh - Check for problematic message content access patterns
#
# This script detects:
# 1. Direct tool message creation without using NewToolResultMessage constructor
# 2. Direct msg.Content access in provider code (should use GetContent() instead)
#
# Usage: ./scripts/lint-message-patterns.sh [--fix]
#
# Exit codes:
#   0 - No issues found
#   1 - Issues found

set -e

RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

ISSUES_FOUND=0

echo "Checking for problematic message patterns..."
echo

# Pattern 1: Direct tool message creation (should use NewToolResultMessage)
# Look for: types.Message{...Role: "tool"...ToolResult:...}
# Exclude test files and the constructor definition itself
echo "=== Checking for direct tool message creation ==="
PATTERN1_FILES=$(grep -rn 'types\.Message{' --include='*.go' \
    runtime/pipeline runtime/streaming tools/arena 2>/dev/null | \
    grep -v '_test\.go' | \
    grep -v 'message\.go' | \
    grep 'Role.*"tool"' | \
    grep 'ToolResult' || true)

if [ -n "$PATTERN1_FILES" ]; then
    echo -e "${YELLOW}WARNING: Found direct tool message creation. Use types.NewToolResultMessage() instead:${NC}"
    echo "$PATTERN1_FILES"
    echo
    ISSUES_FOUND=1
fi

# Pattern 2: Direct msg.Content access in provider tool handling code
# These files should use GetContent() or ToolResult.Content
echo "=== Checking for direct msg.Content access in providers ==="
PATTERN2_FILES=$(grep -rn 'msg\.Content' --include='*.go' \
    runtime/providers 2>/dev/null | \
    grep -v '_test\.go' | \
    grep -v 'GetContent\|ToolResult\.Content' | \
    grep -v '// Legacy\|// System\|// Handle legacy\|msg\.Content != "".*len(msg\.Parts)' | \
    grep -v 'streaming\.go' || true)  # Exclude replay provider which uses different type

if [ -n "$PATTERN2_FILES" ]; then
    echo -e "${YELLOW}WARNING: Found direct msg.Content access in providers.${NC}"
    echo "Consider using GetContent() for safer content extraction:"
    echo "$PATTERN2_FILES"
    echo
    # This is a warning, not an error - there may be valid uses
fi

# Summary
echo "=== Summary ==="
if [ $ISSUES_FOUND -eq 0 ]; then
    echo -e "${GREEN}✓ No problematic patterns found${NC}"
else
    echo -e "${RED}✗ Found patterns that should be reviewed${NC}"
    echo
    echo "Guidelines:"
    echo "  - Use types.NewToolResultMessage() to create tool result messages"
    echo "  - Use msg.GetContent() to extract text content from messages"
    echo "  - Only use msg.Content directly for legacy fallback checks"
fi

exit $ISSUES_FOUND

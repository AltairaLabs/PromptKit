#!/usr/bin/env bash
#
# Install git hooks for PromptKit development
# Run this script once after cloning the repository
#

set -e

# Color codes
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}════════════════════════════════════════════${NC}"
echo -e "${BLUE}  PromptKit - Git Hooks Installation${NC}"
echo -e "${BLUE}════════════════════════════════════════════${NC}"
echo ""

# Get script directory and repository root
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"

echo "Repository root: $REPO_ROOT"
echo ""

# Check if .git directory exists
if [ ! -d "$REPO_ROOT/.git" ]; then
    echo -e "${YELLOW}Warning: .git directory not found${NC}"
    echo "This script should be run from within a git repository"
    exit 1
fi

# Check if pre-commit hook already exists
HOOK_SOURCE="$SCRIPT_DIR/pre-commit.sh"
HOOK_DEST="$REPO_ROOT/.git/hooks/pre-commit"

if [ ! -f "$HOOK_SOURCE" ]; then
    echo -e "${YELLOW}Error: Pre-commit hook source not found${NC}"
    echo "Expected: $HOOK_SOURCE"
    exit 1
fi

# Copy and make executable
cp "$HOOK_SOURCE" "$HOOK_DEST"
chmod +x "$HOOK_DEST"
echo -e "${GREEN}✓${NC} Pre-commit hook installed"

echo ""
echo -e "${BLUE}Checking prerequisites...${NC}"
echo ""

# Check for golangci-lint
if command -v golangci-lint &> /dev/null; then
    GOLANGCI_VERSION=$(golangci-lint version --format short 2>/dev/null || echo "unknown")
    echo -e "${GREEN}✓${NC} golangci-lint is installed (version: $GOLANGCI_VERSION)"
else
    echo -e "${YELLOW}⚠${NC} golangci-lint is not installed"
    echo "  Install with: brew install golangci-lint"
    echo "  Or visit: https://golangci-lint.run/usage/install/"
fi

# Check for gosec (optional but recommended)
if command -v gosec &> /dev/null; then
    GOSEC_VERSION=$(gosec -version 2>/dev/null | head -1 || echo "unknown")
    echo -e "${GREEN}✓${NC} gosec is installed ($GOSEC_VERSION)"
else
    echo -e "${YELLOW}⚠${NC} gosec is not installed (optional, recommended)"
    echo "  Install with: brew install gosec"
    echo "  Or visit: https://github.com/securego/gosec"
fi

# Check for diff-cover
if command -v diff-cover &> /dev/null; then
    DIFFCOVER_VERSION=$(diff-cover --version 2>&1 | head -n1 || echo "unknown")
    echo -e "${GREEN}✓${NC} diff-cover is installed ($DIFFCOVER_VERSION)"
else
    echo -e "${YELLOW}⚠${NC} diff-cover is not installed"
    echo "  Install with: pip install diff-cover"
    echo "  Or: pip3 install diff-cover"
fi

# Check for Go
if command -v go &> /dev/null; then
    GO_VERSION=$(go version | awk '{print $3}')
    echo -e "${GREEN}✓${NC} Go is installed ($GO_VERSION)"
else
    echo -e "${YELLOW}⚠${NC} Go is not installed"
    echo "  Please install Go: https://golang.org/doc/install"
fi

echo ""
echo -e "${BLUE}════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ Git hooks installation complete!${NC}"
echo -e "${BLUE}════════════════════════════════════════════${NC}"
echo ""
echo "The pre-commit hook will now run automatically before each commit."
echo ""
echo "What it does:"
echo "  • Lints only changed Go files (fast!)"
echo "  • Runs tests on affected packages"
echo "  • Checks coverage on changed lines (≥80%)"
echo ""
echo "To skip the hook (not recommended):"
echo "  git commit -m \"your message [skip-pre-commit]\""
echo ""
echo "To run checks manually:"
echo "  make verify          # Run all checks"
echo "  make lint-diff       # Lint changed files only"
echo "  make test-coverage-diff  # Check coverage on changes"
echo ""

#!/usr/bin/env bash

# Branch Protection Setup Script for PromptKit
# This script configures branch protection rules using the GitHub CLI

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
REPO_OWNER="${GITHUB_REPO_OWNER:-AltairaLabs}"
REPO_NAME="${GITHUB_REPO_NAME:-PromptKit}"
REPO="${REPO_OWNER}/${REPO_NAME}"

echo "========================================="
echo "Branch Protection Setup for ${REPO}"
echo "========================================="
echo ""

# Check if GitHub CLI is installed
if ! command -v gh &> /dev/null; then
    echo -e "${RED}Error: GitHub CLI (gh) is not installed${NC}"
    echo "Install it from: https://cli.github.com/"
    exit 1
fi

# Check if authenticated
if ! gh auth status &> /dev/null; then
    echo -e "${RED}Error: Not authenticated with GitHub CLI${NC}"
    echo "Run: gh auth login"
    exit 1
fi

echo -e "${GREEN}✓ GitHub CLI is installed and authenticated${NC}"
echo ""

# Function to create branch protection rule
protect_branch() {
    local branch_pattern=$1
    local description=$2
    
    echo -e "${YELLOW}Setting up protection for: ${branch_pattern}${NC}"
    echo "  ${description}"
    
    # Note: The GitHub CLI doesn't have native branch protection commands yet
    # We need to use the API directly
    
    gh api \
        --method PUT \
        -H "Accept: application/vnd.github+json" \
        "/repos/${REPO}/branches/${branch_pattern}/protection" \
        -f required_status_checks[strict]=true \
        -f required_status_checks[contexts][]=test \
        -f required_status_checks[contexts][]=lint \
        -f required_status_checks[contexts][]=build \
        -f required_status_checks[contexts][]=coverage \
        -f required_pull_request_reviews[required_approving_review_count]=1 \
        -f required_pull_request_reviews[dismiss_stale_reviews]=true \
        -f required_pull_request_reviews[require_code_owner_reviews]=true \
        -f required_conversation_resolution=true \
        -f required_signatures=true \
        -f required_linear_history=true \
        -f allow_force_pushes[enabled]=false \
        -f allow_deletions[enabled]=false \
        -f enforce_admins=true \
        -f restrictions=null \
        > /dev/null 2>&1
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Protected: ${branch_pattern}${NC}"
    else
        echo -e "${RED}✗ Failed to protect: ${branch_pattern}${NC}"
        echo -e "${YELLOW}  You may need to configure this manually via GitHub UI${NC}"
    fi
    echo ""
}

# Function to create tag protection
protect_tags() {
    local pattern=$1
    local description=$2
    
    echo -e "${YELLOW}Setting up tag protection for: ${pattern}${NC}"
    echo "  ${description}"
    
    gh api \
        --method POST \
        -H "Accept: application/vnd.github+json" \
        "/repos/${REPO}/tags/protection" \
        -f pattern="${pattern}" \
        > /dev/null 2>&1
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Protected tags: ${pattern}${NC}"
    else
        echo -e "${YELLOW}⚠ Tag protection may already exist or requires organization plan${NC}"
    fi
    echo ""
}

# Main branch protection
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Configuring Branch Protection"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

protect_branch "main" "Production branch with full protection"
echo ""

# Tag protection
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Configuring Tag Protection"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

protect_tags "v*" "Version tags (v1.0.0, v1.1.0, etc.)"
protect_tags "runtime/v*" "Runtime module tags"
protect_tags "sdk/v*" "SDK module tags"
protect_tags "pkg/v*" "Package module tags"
protect_tags "tools/arena/v*" "Arena tool tags"
protect_tags "tools/packc/v*" "PackC tool tags"
protect_tags "tools/inspect-state/v*" "Inspect-state tool tags"

# Summary
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Setup Complete"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo -e "${GREEN}Branch protection has been configured!${NC}"
echo ""
echo "Next steps:"
echo "  1. Review settings at: https://github.com/${REPO}/settings/branches"
echo "  2. Verify CODEOWNERS file is in place"
echo "  3. Test with a sample PR"
echo "  4. Releases are managed via tags (created by release workflow)"
echo ""
echo "Documentation:"
echo "  - Branch Protection Guide: docs/devops/branch-protection.md"
echo "  - Quick Reference: docs/devops/branch-protection-quickref.md"
echo "  - CODEOWNERS: .github/CODEOWNERS"
echo ""

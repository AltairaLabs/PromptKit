#!/usr/bin/env bash
#
# Pre-commit hook for PromptKit
# Runs fast, developer-friendly checks on changed code only
#
# To skip this hook, include "[skip-pre-commit]" in your commit message
#

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_info() {
    echo -e "${BLUE}ℹ ${NC}$1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_header() {
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════${NC}"
}

# Check if commit message contains skip flag
COMMIT_MSG_FILE=".git/COMMIT_EDITMSG"
if [ -f "$COMMIT_MSG_FILE" ]; then
    if grep -q "\[skip-pre-commit\]" "$COMMIT_MSG_FILE"; then
        print_warning "Skipping pre-commit checks (found [skip-pre-commit] in commit message)"
        exit 0
    fi
fi

# Also check the commit message passed via -m flag
for arg in "$@"; do
    if [[ "$arg" == *"[skip-pre-commit]"* ]]; then
        print_warning "Skipping pre-commit checks (found [skip-pre-commit] in commit message)"
        exit 0
    fi
done

print_header "Pre-Commit Checks"
print_info "Running fast checks on changed code only..."

# Track overall status
CHECKS_FAILED=0

# Get the repository root
REPO_ROOT=$(git rev-parse --show-toplevel)
cd "$REPO_ROOT"

# Check for staged Go files
STAGED_GO_FILES=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)

if [ -z "$STAGED_GO_FILES" ]; then
    print_info "No Go files staged for commit. Skipping checks."
    exit 0
fi

print_info "Found $(echo "$STAGED_GO_FILES" | wc -l | tr -d ' ') Go file(s) to check"

#
# 1. Lint changed files only
#
print_header "Linting Changed Files"

# Check if golangci-lint is installed
if ! command -v golangci-lint &> /dev/null; then
    print_error "golangci-lint is not installed"
    echo "  Install with: brew install golangci-lint"
    echo "  Or visit: https://golangci-lint.run/usage/install/"
    CHECKS_FAILED=1
else
    # Run lint on each module with changes
    MODULES_TO_LINT=()
    
    # Determine which modules have changes
    for file in $STAGED_GO_FILES; do
        if [[ "$file" == runtime/* ]]; then
            if [[ ! " ${MODULES_TO_LINT[@]} " =~ " runtime " ]]; then
                MODULES_TO_LINT+=("runtime")
            fi
        elif [[ "$file" == sdk/* ]]; then
            if [[ ! " ${MODULES_TO_LINT[@]} " =~ " sdk " ]]; then
                MODULES_TO_LINT+=("sdk")
            fi
        elif [[ "$file" == pkg/* ]]; then
            if [[ ! " ${MODULES_TO_LINT[@]} " =~ " pkg " ]]; then
                MODULES_TO_LINT+=("pkg")
            fi
        elif [[ "$file" == tools/arena/* ]]; then
            if [[ ! " ${MODULES_TO_LINT[@]} " =~ " tools/arena " ]]; then
                MODULES_TO_LINT+=("tools/arena")
            fi
        elif [[ "$file" == tools/packc/* ]]; then
            if [[ ! " ${MODULES_TO_LINT[@]} " =~ " tools/packc " ]]; then
                MODULES_TO_LINT+=("tools/packc")
            fi
        elif [[ "$file" == tools/inspect-state/* ]]; then
            if [[ ! " ${MODULES_TO_LINT[@]} " =~ " tools/inspect-state " ]]; then
                MODULES_TO_LINT+=("tools/inspect-state")
            fi
        elif [[ "$file" == tools/schema-gen/* ]]; then
            if [[ ! " ${MODULES_TO_LINT[@]} " =~ " tools/schema-gen " ]]; then
                MODULES_TO_LINT+=("tools/schema-gen")
            fi
        fi
    done
    
    if [ ${#MODULES_TO_LINT[@]} -eq 0 ]; then
        print_info "No module changes detected"
    else
        LINT_FAILED=0
        for module in "${MODULES_TO_LINT[@]}"; do
            print_info "Linting $module..."
            
            # Use --new-from-rev=HEAD to only check changes
            # Use --new to only report issues in new/changed code
            if cd "$REPO_ROOT/$module" && golangci-lint run --new-from-rev=HEAD --timeout=3m ./... 2>&1; then
                print_success "$module passed linting"
            else
                print_error "$module has linting issues"
                LINT_FAILED=1
            fi
            cd "$REPO_ROOT"
        done
        
        if [ $LINT_FAILED -eq 1 ]; then
            echo ""
            print_error "Linting failed. Fix the issues above or use [skip-pre-commit] to bypass."
            CHECKS_FAILED=1
        fi
    fi
fi


#
# 2. Build changed modules
#
print_header "Building Changed Modules"

if [ ${#MODULES_TO_LINT[@]} -eq 0 ]; then
    print_info "No module changes detected"
else
    BUILD_FAILED=0
    for module in "${MODULES_TO_LINT[@]}"; do
        print_info "Building $module..."
        
        if cd "$REPO_ROOT/$module" && go build ./... 2>&1; then
            print_success "$module built successfully"
        else
            print_error "$module failed to build"
            BUILD_FAILED=1
        fi
        cd "$REPO_ROOT"
    done
    
    if [ $BUILD_FAILED -eq 1 ]; then
        echo ""
        print_error "Build failed. Fix the compilation errors above."
        CHECKS_FAILED=1
    fi
fi

echo ""

#
# 3. Run tests with coverage on changed packages
#
print_header "Testing Changed Packages"

# Determine which packages to test based on changed files
PACKAGES_TO_TEST=()
TEST_MODULES=()

for file in $STAGED_GO_FILES; do
    # Get the package path for the file
    if [[ "$file" == runtime/* ]]; then
        pkg_dir=$(dirname "$file")
        if [[ ! " ${PACKAGES_TO_TEST[@]} " =~ " ./$pkg_dir " ]]; then
            PACKAGES_TO_TEST+=("./$pkg_dir")
        fi
        if [[ ! " ${TEST_MODULES[@]} " =~ " runtime " ]]; then
            TEST_MODULES+=("runtime")
        fi
    elif [[ "$file" == sdk/* ]]; then
        pkg_dir=$(dirname "$file")
        if [[ ! " ${PACKAGES_TO_TEST[@]} " =~ " ./$pkg_dir " ]]; then
            PACKAGES_TO_TEST+=("./$pkg_dir")
        fi
        if [[ ! " ${TEST_MODULES[@]} " =~ " sdk " ]]; then
            TEST_MODULES+=("sdk")
        fi
    elif [[ "$file" == pkg/* ]]; then
        pkg_dir=$(dirname "$file")
        if [[ ! " ${PACKAGES_TO_TEST[@]} " =~ " ./$pkg_dir " ]]; then
            PACKAGES_TO_TEST+=("./$pkg_dir")
        fi
        if [[ ! " ${TEST_MODULES[@]} " =~ " pkg " ]]; then
            TEST_MODULES+=("pkg")
        fi
    elif [[ "$file" == tools/arena/* ]]; then
        pkg_dir=$(dirname "$file")
        if [[ ! " ${PACKAGES_TO_TEST[@]} " =~ " ./$pkg_dir " ]]; then
            PACKAGES_TO_TEST+=("./$pkg_dir")
        fi
        if [[ ! " ${TEST_MODULES[@]} " =~ " tools/arena " ]]; then
            TEST_MODULES+=("tools/arena")
        fi
    elif [[ "$file" == tools/packc/* ]]; then
        pkg_dir=$(dirname "$file")
        if [[ ! " ${PACKAGES_TO_TEST[@]} " =~ " ./$pkg_dir " ]]; then
            PACKAGES_TO_TEST+=("./$pkg_dir")
        fi
        if [[ ! " ${TEST_MODULES[@]} " =~ " tools/packc " ]]; then
            TEST_MODULES+=("tools/packc")
        fi
    elif [[ "$file" == tools/inspect-state/* ]]; then
        pkg_dir=$(dirname "$file")
        if [[ ! " ${PACKAGES_TO_TEST[@]} " =~ " ./$pkg_dir " ]]; then
            PACKAGES_TO_TEST+=("./$pkg_dir")
        fi
        if [[ ! " ${TEST_MODULES[@]} " =~ " tools/inspect-state " ]]; then
            TEST_MODULES+=("tools/inspect-state")
        fi
    fi
done

if [ ${#TEST_MODULES[@]} -eq 0 ]; then
    print_info "No test modules to run"
else
    print_info "Running tests for changed packages..."
    
    # Create temp directory for coverage files
    TEMP_COVERAGE_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_COVERAGE_DIR" EXIT
    
    TEST_FAILED=0
    for module in "${TEST_MODULES[@]}"; do
        print_info "Testing $module..."
        
        COVERAGE_FILE="$TEMP_COVERAGE_DIR/${module//\//_}-coverage.out"
        
        if cd "$REPO_ROOT/$module" && go test -coverprofile="$COVERAGE_FILE" -covermode=set ./... 2>&1 | grep -v "no test files"; then
            print_success "$module tests passed"
        else
            print_error "$module tests failed"
            TEST_FAILED=1
        fi
        cd "$REPO_ROOT"
    done
    
    if [ $TEST_FAILED -eq 1 ]; then
        echo ""
        print_error "Tests failed. Fix the issues above or use [skip-pre-commit] to bypass."
        CHECKS_FAILED=1
    else
        # Merge coverage files
        MERGED_COVERAGE="$TEMP_COVERAGE_DIR/coverage.out"
        echo "mode: set" > "$MERGED_COVERAGE"
        for cov_file in "$TEMP_COVERAGE_DIR"/*-coverage.out; do
            if [ -f "$cov_file" ]; then
                grep -h -v "^mode:" "$cov_file" >> "$MERGED_COVERAGE" 2>/dev/null || true
            fi
        done
        
        # Check coverage on changed files only (excluding *_test.go and *_interactive.go)
        echo ""
        print_info "Checking coverage on changed files..."
        
        COVERAGE_THRESHOLD=80.0
        COVERAGE_FAILED=0
        COVERAGE_RESULTS=()
        
        if [ -f "$MERGED_COVERAGE" ]; then
            # Check each staged Go file (excluding test, interactive, and integration files)
            for file in $STAGED_GO_FILES; do
                # Skip test files, interactive files, and integration files
                if [[ "$file" == *_test.go ]] || [[ "$file" == *_interactive.go ]] || [[ "$file" == *_integration.go ]]; then
                    continue
                fi
                
                # Skip example files (examples directory) and integration tests (tests directory)
                if [[ "$file" == examples/* ]] || [[ "$file" == */examples/* ]] || [[ "$file" == tests/* ]]; then
                    continue
                fi
                
                # Skip build-time tooling (schema generators, etc.)
                if [[ "$file" == tools/schema-gen/* ]]; then
                    COVERAGE_RESULTS+=("○ $file: N/A (build tooling)")
                    continue
                fi
                
                # Calculate ACTUAL statement coverage from raw coverage.out
                # Format: file:startLine.startCol,endLine.endCol numStatements count
                # count > 0 means covered, count = 0 means not covered
                FILE_COV_PERCENT=$(grep "/$file:" "$MERGED_COVERAGE" 2>/dev/null | awk -F'[ ]' '
                {
                    # Last two fields: numStatements count
                    # Split on space, get the numeric fields
                    n = split($0, parts, " ")
                    if (n >= 2) {
                        count = parts[n]      # coverage count (0 = not covered, 1+ = covered)
                        stmts = parts[n-1]    # number of statements in this block
                        if (stmts ~ /^[0-9]+$/ && count ~ /^[0-9]+$/) {
                            total_stmts += stmts
                            if (count > 0) {
                                covered_stmts += stmts
                            }
                        }
                    }
                }
                END {
                    if (total_stmts > 0) {
                        printf "%.1f", (covered_stmts / total_stmts) * 100
                    }
                }')
                
                if [ -n "$FILE_COV_PERCENT" ]; then
                    # Check if coverage meets threshold
                    COV_OK=$(echo "$FILE_COV_PERCENT $COVERAGE_THRESHOLD" | awk '{if ($1 >= $2) print "1"; else print "0"}')
                    
                    if [ "$COV_OK" = "1" ]; then
                        COVERAGE_RESULTS+=("✓ $file: ${FILE_COV_PERCENT}%")
                    else
                        COVERAGE_RESULTS+=("✗ $file: ${FILE_COV_PERCENT}% (below ${COVERAGE_THRESHOLD}%)")
                        COVERAGE_FAILED=1
                    fi
                else
                    # No coverage data for this file - check if it has any executable code
                    # Files with only type definitions won't have coverage data
                    HAS_FUNCS=$(grep -l "^func " "$REPO_ROOT/$file" 2>/dev/null || true)
                    if [ -n "$HAS_FUNCS" ]; then
                        COVERAGE_RESULTS+=("✗ $file: 0.0% (no coverage data)")
                        COVERAGE_FAILED=1
                    else
                        COVERAGE_RESULTS+=("○ $file: N/A (no executable code)")
                    fi
                fi
            done
            
            # Print results
            if [ ${#COVERAGE_RESULTS[@]} -eq 0 ]; then
                print_info "No non-test Go files to check (all changes in test files)"
            else
                for result in "${COVERAGE_RESULTS[@]}"; do
                    echo "  $result"
                done
                
                echo ""
                if [ $COVERAGE_FAILED -eq 1 ]; then
                    print_error "Some files have insufficient test coverage (threshold: ${COVERAGE_THRESHOLD}%)"
                    print_info "Add tests or move untestable code to *_interactive.go or *_integration.go files"
                    CHECKS_FAILED=1
                else
                    print_success "All changed files meet coverage threshold (≥${COVERAGE_THRESHOLD}%)"
                fi
            fi
            
            # Show overall summary
            echo ""
            TOTAL_COVERAGE=$(go tool cover -func="$MERGED_COVERAGE" | tail -1 | grep -oE '[0-9]+\.[0-9]+%')
            print_info "Overall coverage: $TOTAL_COVERAGE"
        fi
    fi
fi

#
# 4. Lint TypeScript files in GitHub Actions
#
print_header "Linting TypeScript Files"

# Check for staged TypeScript files in GitHub Actions
STAGED_TS_FILES=$(git diff --cached --name-only --diff-filter=ACM | grep '\.github/actions/.*\.ts$' || true)

if [ -z "$STAGED_TS_FILES" ]; then
    print_info "No TypeScript files staged in GitHub Actions"
else
    print_info "Found $(echo "$STAGED_TS_FILES" | wc -l | tr -d ' ') TypeScript file(s) to check"

    TS_LINT_FAILED=0

    # Check packc-action
    if echo "$STAGED_TS_FILES" | grep -q "packc-action"; then
        print_info "Linting packc-action TypeScript..."
        if cd "$REPO_ROOT/.github/actions/packc-action" && npm run lint 2>&1; then
            print_success "packc-action passed linting"
        else
            print_error "packc-action has linting issues"
            TS_LINT_FAILED=1
        fi
        cd "$REPO_ROOT"
    fi

    # Check promptarena-action
    if echo "$STAGED_TS_FILES" | grep -q "promptarena-action"; then
        print_info "Linting promptarena-action TypeScript..."
        if cd "$REPO_ROOT/.github/actions/promptarena-action" && npm run lint 2>&1; then
            print_success "promptarena-action passed linting"
        else
            print_error "promptarena-action has linting issues"
            TS_LINT_FAILED=1
        fi
        cd "$REPO_ROOT"
    fi

    if [ $TS_LINT_FAILED -eq 1 ]; then
        echo ""
        print_error "TypeScript linting failed. Fix the issues above or use [skip-pre-commit] to bypass."
        CHECKS_FAILED=1
    fi
fi

#
# Summary
#
print_header "Summary"

if [ $CHECKS_FAILED -eq 0 ]; then
    print_success "All pre-commit checks passed!"
    echo ""
    print_info "Proceeding with commit..."
    exit 0
else
    echo ""
    print_error "Pre-commit checks failed!"
    echo ""
    print_info "Options:"
    echo "  1. Fix the issues above and try again"
    echo "  2. Run 'make verify' to see detailed output"
    echo "  3. Use [skip-pre-commit] in commit message to bypass (not recommended)"
    echo ""
    exit 1
fi

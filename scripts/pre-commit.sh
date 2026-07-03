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

# --- Module resolution -------------------------------------------------------
# Resolve each staged Go file to its nearest enclosing module (the closest
# parent directory containing a go.mod), relative to REPO_ROOT. This correctly
# attributes files in nested modules — e.g. sdk/examples/voice-chat, which has
# its own go.mod and is NOT part of go.work — to that module rather than to the
# parent module they happen to sit under. Mapping by path prefix (sdk/* → sdk)
# made golangci-lint fail with "main module does not contain package".
find_module_dir() {
    local dir
    dir=$(dirname "$1")
    while [ "$dir" != "." ] && [ "$dir" != "/" ]; do
        if [ -f "$REPO_ROOT/$dir/go.mod" ]; then
            printf '%s\n' "$dir"
            return 0
        fi
        dir=$(dirname "$dir")
    done
    [ -f "$REPO_ROOT/go.mod" ] && printf '.\n'
}

# Print "off" when a module is NOT a member of the Go workspace, so it is
# linted/built/tested in isolation against its own go.mod (+ replace) directives
# via GOWORK=off. Workspace members print nothing (workspace mode is correct).
gowork_off_for() {
    if [ -f "$REPO_ROOT/go.work" ] && grep -qE "^[[:space:]]*\./$1[[:space:]]*$" "$REPO_ROOT/go.work"; then
        printf ''
    else
        printf 'off'
    fi
}

# run_in_module <module-dir> <command...> — run a command inside the module dir,
# isolating non-workspace modules with GOWORK=off. Callers add their own 2>&1.
run_in_module() {
    local module="$1"; shift
    if [ "$(gowork_off_for "$module")" = "off" ]; then
        ( cd "$REPO_ROOT/$module" && GOWORK=off "$@" )
    else
        ( cd "$REPO_ROOT/$module" && "$@" )
    fi
}

# A module whose packages are all excluded by build constraints in the default
# build (e.g. everything behind a //go:build portaudio tag) or that has no test
# files has nothing to analyze/build/test — that is not a failure. Match the Go
# toolchain / golangci-lint messages that signal "nothing to do here".
is_empty_module_output() {
    echo "$1" | grep -qE "no go files to analyze|matched no packages|build constraints exclude all Go files|no test files"
}

# Unique list of modules touched by this commit.
CHANGED_MODULES=()
for file in $STAGED_GO_FILES; do
    mod=$(find_module_dir "$file")
    [ -z "$mod" ] && continue
    if [[ ! " ${CHANGED_MODULES[*]} " =~ " ${mod} " ]]; then
        CHANGED_MODULES+=("$mod")
    fi
done

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
    if [ ${#CHANGED_MODULES[@]} -eq 0 ]; then
        print_info "No module changes detected"
    else
        LINT_FAILED=0
        for module in "${CHANGED_MODULES[@]}"; do
            print_info "Linting $module..."

            # --new-from-rev=HEAD reports issues only in new/changed code.
            # `env -u GIT_DIR GIT_INDEX_FILE GIT_WORK_TREE`: when this hook runs
            # from a linked git worktree, `git commit` exports GIT_DIR (an
            # absolute path under .git/worktrees/<name>) into the hook env.
            # golangci-lint's go-git-based revgrep then cannot resolve the work
            # tree, fails open, and floods the whole module's pre-existing debt
            # instead of just the diff. Unsetting those vars restores correct
            # new-from-rev scoping (main repo and worktree behave identically).
            set +e
            lint_out=$(run_in_module "$module" env -u GIT_DIR -u GIT_INDEX_FILE -u GIT_WORK_TREE \
                golangci-lint run --new-from-rev=HEAD --timeout=3m ./... 2>&1)
            lint_rc=$?
            set -e
            [ -n "$lint_out" ] && echo "$lint_out"

            if [ $lint_rc -eq 0 ] || is_empty_module_output "$lint_out"; then
                print_success "$module passed linting"
            else
                print_error "$module has linting issues"
                LINT_FAILED=1
            fi
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

if [ ${#CHANGED_MODULES[@]} -eq 0 ]; then
    print_info "No module changes detected"
else
    BUILD_FAILED=0
    for module in "${CHANGED_MODULES[@]}"; do
        print_info "Building $module..."

        set +e
        build_out=$(run_in_module "$module" go build ./... 2>&1)
        build_rc=$?
        set -e
        [ -n "$build_out" ] && echo "$build_out"

        if [ $build_rc -eq 0 ] || is_empty_module_output "$build_out"; then
            print_success "$module built successfully"
        else
            print_error "$module failed to build"
            BUILD_FAILED=1
        fi
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

# Test the same set of modules touched by this commit (CHANGED_MODULES),
# resolved to each file's nearest go.mod so nested modules are handled.
if [ ${#CHANGED_MODULES[@]} -eq 0 ]; then
    print_info "No test modules to run"
else
    print_info "Running tests for changed packages..."

    # Create temp directory for coverage files
    TEMP_COVERAGE_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_COVERAGE_DIR" EXIT

    TEST_FAILED=0
    for module in "${CHANGED_MODULES[@]}"; do
        print_info "Testing $module..."

        COVERAGE_FILE="$TEMP_COVERAGE_DIR/${module//\//_}-coverage.out"

        set +e
        test_out=$(run_in_module "$module" go test -coverprofile="$COVERAGE_FILE" -covermode=set ./... 2>&1)
        test_rc=$?
        set -e
        echo "$test_out" | grep -v "no test files" || true

        if [ $test_rc -eq 0 ] || is_empty_module_output "$test_out"; then
            print_success "$module tests passed"
        else
            print_error "$module tests failed"
            TEST_FAILED=1
        fi
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
        
        # Files exempt from the coverage gate — MUST stay in sync with
        # sonar.coverage.exclusions. Each is thin live-I/O glue that cannot run
        # in CI (real network/websocket to a provider, ffmpeg subprocess,
        # cloud-SDK credential fetch) or a cross-package test harness. The
        # *_integration.go / *_interactive.go suffix ALONE no longer grants an
        # exemption — only membership in this list does — so new code cannot
        # dodge the gate by adopting the suffix. Extract pure logic into a gated
        # sibling and test it instead of adding entries here.
        COVERAGE_EXEMPT_FILES="runtime/credentials/aws_integration.go
runtime/credentials/azure_integration.go
runtime/credentials/gcp_integration.go
runtime/media/audio_converter_integration.go
runtime/pipeline/stage/stages_video_frames_integration.go
runtime/providers/gemini/stream_session_integration.go
runtime/providers/gemini/stream_session_protocol_integration.go
runtime/providers/gemini/stream_session_tools_integration.go
runtime/providers/openai/openai_responses_integration.go
runtime/providers/openai/realtime_session_integration.go
runtime/providers/openai/realtime_tools_integration.go
runtime/providers/openai/realtime_websocket_integration.go
runtime/providers/openai/streaming_support_integration.go
runtime/providers/provider_contract_integration.go
runtime/skills/installer_integration.go
runtime/tts/cartesia_interactive.go"

        if [ -f "$MERGED_COVERAGE" ]; then
            # Check each staged Go file (test files and the explicit exempt
            # allowlist are skipped; the suffix alone is not an exemption).
            for file in $STAGED_GO_FILES; do
                # Always skip test files.
                if [[ "$file" == *_test.go ]]; then
                    continue
                fi
                # Skip only files on the explicit coverage-exemption allowlist.
                if printf '%s\n' "$COVERAGE_EXEMPT_FILES" | grep -qxF "$file"; then
                    continue
                fi

                # Skip example files (examples directory) and integration tests (tests directory)
                if [[ "$file" == examples/* ]] || [[ "$file" == */examples/* ]] || [[ "$file" == tests/* ]]; then
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
                    print_info "Add tests. If the code is genuinely un-runnable in CI (live network/device/subprocess), extract the pure logic into a gated sibling and test THAT — renaming to *_integration.go no longer exempts a file; only the COVERAGE_EXEMPT_FILES allowlist (mirrored in sonar.coverage.exclusions) does."
                    CHECKS_FAILED=1
                else
                    print_success "All changed files meet coverage threshold (≥${COVERAGE_THRESHOLD}%)"
                fi
            fi
            
            # Show overall summary (only when there is real coverage data beyond
            # the "mode:" header — test-less/tag-only modules produce none).
            if grep -qvE "^mode:" "$MERGED_COVERAGE" 2>/dev/null; then
                echo ""
                TOTAL_COVERAGE=$(go tool cover -func="$MERGED_COVERAGE" 2>/dev/null | tail -1 | grep -oE '[0-9]+\.[0-9]+%' || true)
                [ -n "$TOTAL_COVERAGE" ] && print_info "Overall coverage: $TOTAL_COVERAGE"
            fi
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

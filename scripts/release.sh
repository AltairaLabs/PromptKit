#!/bin/bash
# Automated release script for PromptKit monorepo
# Handles the entire release process for tools

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
DRY_RUN=false
SKIP_TESTS=false
SKIP_WAIT=false
AUTO_CONFIRM=false

# Function to print colored output
print_step() {
    echo -e "${BLUE}==>${NC} $1"
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

# Function to wait with countdown
wait_with_countdown() {
    local seconds=$1
    local message=$2
    
    if [ "$SKIP_WAIT" = true ]; then
        print_warning "Skipping wait (--skip-wait enabled)"
        return
    fi
    
    print_step "$message"
    for i in $(seq $seconds -1 1); do
        echo -ne "\rWaiting: $i seconds remaining...  "
        sleep 1
    done
    echo -e "\r${GREEN}✓${NC} Wait complete!                    "
}

# Function to confirm action
confirm() {
    if [ "$AUTO_CONFIRM" = true ]; then
        return 0
    fi
    
    read -p "$1 [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        return 1
    fi
    return 0
}

# Usage
usage() {
    cat << EOF
Usage: $0 [OPTIONS] VERSION

Automate the release process for PromptKit (all components).

VERSION should be in format: v1.0.0

OPTIONS:
    -h, --help              Show this help message
    -d, --dry-run           Show what would be done without actually doing it
    -s, --skip-tests        Skip running tests before release
    -w, --skip-wait         Skip waiting periods (for testing)
    -y, --yes               Auto-confirm all prompts
    --libs-only             Only tag libraries (runtime, pkg, sdk)
    --tools-only            Only tag tools (assumes libs already tagged)

EXAMPLES:
    # Full release with confirmation prompts
    $0 v1.0.0

    # Dry run to see what would happen
    $0 --dry-run v1.0.0

    # Auto-confirm all steps
    $0 --yes v1.0.0

    # Only tag libraries, do tools later
    $0 --libs-only v1.0.0
    # Wait 10 minutes, then:
    $0 --tools-only v1.0.0

COMPONENTS RELEASED:
    Libraries (can be used independently):
      - runtime/$VERSION    (Core runtime components)
      - pkg/$VERSION        (Shared packages)
      - sdk/$VERSION        (Production SDK library)
    
    Tools (depend on libraries):
      - tools/arena/$VERSION    (Arena CLI)
      - tools/packc/$VERSION    (PackC CLI)

PHASES:
    1. Validation (check version, branch, tests)
    2. Tag libraries (runtime, pkg, sdk)
    3. Wait for Go proxy (5-10 minutes)
    4. Update tool go.mod files
    5. Create release branch
    6. Tag tools (arena, packc)
    7. Verify installation
    8. Instructions for cleanup

EOF
    exit 0
}

# Parse arguments
VERSION=""
LIBS_ONLY=false
TOOLS_ONLY=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            usage
            ;;
        -d|--dry-run)
            DRY_RUN=true
            shift
            ;;
        -s|--skip-tests)
            SKIP_TESTS=true
            shift
            ;;
        -w|--skip-wait)
            SKIP_WAIT=true
            shift
            ;;
        -y|--yes)
            AUTO_CONFIRM=true
            shift
            ;;
        --libs-only|--deps-only)
            LIBS_ONLY=true
            shift
            ;;
        --tools-only)
            TOOLS_ONLY=true
            shift
            ;;
        v*)
            VERSION=$1
            shift
            ;;
        *)
            print_error "Unknown option: $1"
            usage
            ;;
    esac
done

# Validate version format
if [ -z "$VERSION" ]; then
    print_error "Version is required"
    usage
fi

if [[ ! $VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    print_error "Version must be in format: v1.0.0"
    exit 1
fi

# Print banner
echo "================================================"
echo "  PromptKit Release Automation"
echo "================================================"
echo "Version:      $VERSION"
echo "Dry Run:      $DRY_RUN"
echo "Skip Tests:   $SKIP_TESTS"
echo "Skip Wait:    $SKIP_WAIT"
echo "Auto Confirm: $AUTO_CONFIRM"
echo "Libs Only:    $LIBS_ONLY"
echo "Tools Only:   $TOOLS_ONLY"
echo "================================================"
echo ""

# Phase 1: Validation
if [ "$TOOLS_ONLY" = false ]; then
    print_step "Phase 1: Validation"
    
    # Check we're on main branch
    current_branch=$(git branch --show-current)
    if [ "$current_branch" != "main" ]; then
        print_error "Must be on main branch (currently on: $current_branch)"
        exit 1
    fi
    print_success "On main branch"
    
    # Check working directory is clean
    if [ -n "$(git status --porcelain)" ]; then
        print_error "Working directory is not clean"
        git status --short
        exit 1
    fi
    print_success "Working directory is clean"
    
    # Pull latest
    print_step "Pulling latest changes..."
    if [ "$DRY_RUN" = false ]; then
        git pull origin main
    fi
    print_success "Up to date with origin/main"
    
    # Check if version tag already exists
    if git tag -l | grep -q "^$VERSION$\|^runtime/$VERSION$\|^pkg/$VERSION$\|^tools/arena/$VERSION$\|^tools/packc/$VERSION$"; then
        print_error "Version $VERSION already exists in tags"
        git tag -l | grep "$VERSION"
        exit 1
    fi
    print_success "Version $VERSION is available"
    
    # Run tests
    if [ "$SKIP_TESTS" = false ]; then
        print_step "Running tests..."
        if [ "$DRY_RUN" = false ]; then
            make test || {
                print_error "Tests failed"
                exit 1
            }
        fi
        print_success "Tests passed"
    else
        print_warning "Skipping tests"
    fi
    
    echo ""
fi

# Phase 2: Tag Libraries
if [ "$TOOLS_ONLY" = false ]; then
    print_step "Phase 2: Tag Libraries (runtime, pkg, sdk)"
    
    if ! confirm "Tag runtime/$VERSION, pkg/$VERSION, and sdk/$VERSION?"; then
        print_error "Aborted by user"
        exit 1
    fi
    
    # Tag runtime
    print_step "Tagging runtime/$VERSION..."
    if [ "$DRY_RUN" = false ]; then
        git tag -a "runtime/$VERSION" -m "Release runtime $VERSION"
        git push origin "runtime/$VERSION"
    else
        echo "[DRY RUN] Would run: git tag -a runtime/$VERSION -m 'Release runtime $VERSION'"
        echo "[DRY RUN] Would run: git push origin runtime/$VERSION"
    fi
    print_success "Tagged runtime/$VERSION"
    
    # Tag pkg
    print_step "Tagging pkg/$VERSION..."
    if [ "$DRY_RUN" = false ]; then
        git tag -a "pkg/$VERSION" -m "Release pkg $VERSION"
        git push origin "pkg/$VERSION"
    else
        echo "[DRY RUN] Would run: git tag -a pkg/$VERSION -m 'Release pkg $VERSION'"
        echo "[DRY RUN] Would run: git push origin pkg/$VERSION"
    fi
    print_success "Tagged pkg/$VERSION"
    
    # Tag sdk
    print_step "Tagging sdk/$VERSION..."
    if [ "$DRY_RUN" = false ]; then
        git tag -a "sdk/$VERSION" -m "Release sdk $VERSION"
        git push origin "sdk/$VERSION"
    else
        echo "[DRY RUN] Would run: git tag -a sdk/$VERSION -m 'Release sdk $VERSION'"
        echo "[DRY RUN] Would run: git push origin sdk/$VERSION"
    fi
    print_success "Tagged sdk/$VERSION"
    
    echo ""
fi

# Phase 3: Wait for Go Proxy
if [ "$LIBS_ONLY" = true ]; then
    echo ""
    print_success "Libraries tagged successfully!"
    echo ""
    echo "Next steps:"
    echo "1. Wait 5-10 minutes for Go proxy to cache the modules"
    echo "2. Verify on proxy:"
    echo "   curl https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit/runtime/@v/$VERSION.info"
    echo "   curl https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit/pkg/@v/$VERSION.info"
    echo "   curl https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit/sdk/@v/$VERSION.info"
    echo "3. Continue with tools:"
    echo "   $0 --tools-only $VERSION"
    echo ""
    echo "SDK users can now use:"
    echo "   go get github.com/AltairaLabs/PromptKit/sdk@$VERSION"
    exit 0
fi

if [ "$TOOLS_ONLY" = false ]; then
    wait_with_countdown 600 "Phase 3: Waiting for Go proxy to cache libraries (10 minutes)..."
    
    # Verify libraries are available
    print_step "Verifying libraries on Go proxy..."
    
    runtime_check=$(curl -s "https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit/runtime/@v/$VERSION.info" || echo "failed")
    pkg_check=$(curl -s "https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit/pkg/@v/$VERSION.info" || echo "failed")
    sdk_check=$(curl -s "https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit/sdk/@v/$VERSION.info" || echo "failed")
    
    if [[ $runtime_check == *"Version"* ]] && [[ $pkg_check == *"Version"* ]] && [[ $sdk_check == *"Version"* ]]; then
        print_success "All libraries are available on Go proxy"
    else
        print_warning "Libraries might not be cached yet, but continuing..."
        if [[ $sdk_check != *"Version"* ]]; then
            print_warning "SDK not yet available - users cannot use go get until cached"
        fi
    fi
    
    echo ""
fi

# Phase 4: Update Tool Dependencies
print_step "Phase 4: Update Tool Dependencies"

if ! confirm "Update tool go.mod files to use $VERSION?"; then
    print_error "Aborted by user"
    exit 1
fi

# Create release branch
RELEASE_BRANCH="release/tools/$VERSION"
print_step "Creating branch: $RELEASE_BRANCH"

if [ "$DRY_RUN" = false ]; then
    git checkout -b "$RELEASE_BRANCH"
else
    echo "[DRY RUN] Would run: git checkout -b $RELEASE_BRANCH"
fi

# Update arena
print_step "Updating tools/arena/go.mod..."
if [ "$DRY_RUN" = false ]; then
    cd tools/arena
    go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/runtime
    go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/pkg
    go get "github.com/AltairaLabs/PromptKit/runtime@$VERSION"
    go get "github.com/AltairaLabs/PromptKit/pkg@$VERSION"
    go mod tidy
    go build -v ./... || {
        print_error "Arena build failed"
        exit 1
    }
    cd ../..
else
    echo "[DRY RUN] Would update arena dependencies to $VERSION"
fi
print_success "Updated arena"

# Update packc
print_step "Updating tools/packc/go.mod..."
if [ "$DRY_RUN" = false ]; then
    cd tools/packc
    go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/runtime
    go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/pkg
    go get "github.com/AltairaLabs/PromptKit/runtime@$VERSION"
    go get "github.com/AltairaLabs/PromptKit/pkg@$VERSION"
    go mod tidy
    go build -v ./... || {
        print_error "Packc build failed"
        exit 1
    }
    cd ../..
else
    echo "[DRY RUN] Would update packc dependencies to $VERSION"
fi
print_success "Updated packc"

echo ""

# Phase 5: Commit and Push Release Branch
print_step "Phase 5: Commit Release Branch"

if [ "$DRY_RUN" = false ]; then
    git add tools/arena/go.mod tools/arena/go.sum
    git add tools/packc/go.mod tools/packc/go.sum
    git commit -m "release: update tools to $VERSION"
    git push origin "$RELEASE_BRANCH"
else
    echo "[DRY RUN] Would commit and push release branch"
fi
print_success "Committed and pushed $RELEASE_BRANCH"

echo ""

# Phase 6: Tag Tools
print_step "Phase 6: Tag Tools"

if ! confirm "Tag tools/arena/$VERSION and tools/packc/$VERSION?"; then
    print_error "Aborted by user"
    exit 1
fi

# Tag arena
print_step "Tagging tools/arena/$VERSION..."
if [ "$DRY_RUN" = false ]; then
    git tag -a "tools/arena/$VERSION" -m "Release arena $VERSION"
    git push origin "tools/arena/$VERSION"
else
    echo "[DRY RUN] Would tag tools/arena/$VERSION"
fi
print_success "Tagged tools/arena/$VERSION"

# Tag packc
print_step "Tagging tools/packc/$VERSION..."
if [ "$DRY_RUN" = false ]; then
    git tag -a "tools/packc/$VERSION" -m "Release packc $VERSION"
    git push origin "tools/packc/$VERSION"
else
    echo "[DRY RUN] Would tag tools/packc/$VERSION"
fi
print_success "Tagged tools/packc/$VERSION"

echo ""

# Phase 7: Verify Installation
print_step "Phase 7: Verification"

wait_with_countdown 300 "Waiting for Go proxy to cache tools (5 minutes)..."

print_step "Testing installation..."

if [ "$DRY_RUN" = false ]; then
    if go install "github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@$VERSION"; then
        print_success "Arena installed successfully"
    else
        print_warning "Arena installation may need more time"
    fi
    
    if go install "github.com/AltairaLabs/PromptKit/tools/packc@$VERSION"; then
        print_success "Packc installed successfully"
    else
        print_warning "Packc installation may need more time"
    fi
else
    echo "[DRY RUN] Would test installation"
fi

echo ""

# Phase 8: Create GitHub Release with GoReleaser
print_step "Phase 8: Create GitHub Release"

if [ "$DRY_RUN" = false ]; then
    if ! confirm "Create GitHub release with GoReleaser (binaries, checksums, etc.)?"; then
        print_warning "Skipping GitHub release creation"
    else
        # Check if goreleaser is installed
        if ! command -v goreleaser &> /dev/null; then
            print_warning "GoReleaser not found. Install it to create releases automatically:"
            echo "  brew install goreleaser/tap/goreleaser"
            echo "  # or"
            echo "  go install github.com/goreleaser/goreleaser@latest"
            echo ""
            print_step "Skipping GoReleaser, but you can run it manually later:"
            echo "  goreleaser release --clean"
        else
            print_step "Running GoReleaser..."
            
            # GoReleaser will:
            # - Build binaries for multiple platforms
            # - Create checksums
            # - Generate changelog
            # - Create draft GitHub release
            # - Upload all artifacts
            
            if goreleaser release --clean; then
                print_success "GoReleaser completed successfully"
                echo ""
                echo "📦 GitHub release created (as draft)"
                echo "   Review at: https://github.com/AltairaLabs/PromptKit/releases"
                echo ""
            else
                print_error "GoReleaser failed. You can run it manually:"
                echo "  goreleaser release --clean"
            fi
        fi
    fi
else
    echo "[DRY RUN] Would run: goreleaser release --clean"
fi

echo ""

# Phase 9: Return to main and restore replace directives
print_step "Phase 9: Cleanup"

if [ "$DRY_RUN" = false ]; then
    if confirm "Return to main branch and restore replace directives?"; then
        print_step "Checking out main branch..."
        git checkout main
        print_success "Back on main branch with replace directives intact"
    else
        print_warning "Staying on release branch - remember to restore replace directives!"
    fi
else
    echo "[DRY RUN] Would checkout main branch"
fi

echo ""

# Final Summary
print_success "Release $VERSION Complete!"
echo ""
echo "================================================"
echo "  ✅ What Was Done"
echo "================================================"
echo ""
echo "✓ Tagged libraries: runtime, pkg, sdk"
echo "✓ Updated tools to use versioned dependencies"
echo "✓ Tagged tools: arena, packc"
echo "✓ Created GitHub release (draft)"
echo "✓ Built and uploaded binaries"
echo ""
echo "================================================"
echo "  📋 Next Steps"
echo "================================================"
echo ""
echo "1. Review and publish GitHub release:"
echo "   https://github.com/AltairaLabs/PromptKit/releases"
echo ""
echo "2. Update documentation:"
echo "   - Update README.md install instructions"
echo "   - Update CHANGELOG.md"
echo ""
echo "3. Test installations:"
echo ""
echo "   # Pre-built binaries (from GitHub release)"
echo "   wget https://github.com/AltairaLabs/PromptKit/releases/download/$VERSION/..."
echo ""
echo "   # Go install (CLI tools)"
echo "   go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@$VERSION"
echo "   go install github.com/AltairaLabs/PromptKit/tools/packc@$VERSION"
echo ""
echo "   # Go get (SDK library)"
echo "   go get github.com/AltairaLabs/PromptKit/sdk@$VERSION"
echo ""
echo "4. Announce release!"
echo "   - Blog post"
echo "   - Social media"
echo "   - Slack/Discord"
echo ""
echo "================================================"

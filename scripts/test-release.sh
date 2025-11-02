#!/bin/bash
# Test release preparation script - safe to run locally without polluting git history

set -e

TOOL=${1:-arena}
VERSION=${2:-v0.0.1-test}
DRY_RUN=${3:-true}

echo "================================================"
echo "🧪 Testing Release Preparation"
echo "================================================"
echo "Tool:     $TOOL"
echo "Version:  $VERSION"
echo "Dry Run:  $DRY_RUN"
echo "================================================"
echo ""

# Validate tool exists
if [ ! -d "tools/$TOOL" ]; then
    echo "❌ Error: Tool 'tools/$TOOL' not found"
    echo "Available tools:"
    ls -d tools/*/
    exit 1
fi

echo "📋 Step 1: Backing up current go.mod files..."
cp "tools/$TOOL/go.mod" "tools/$TOOL/go.mod.backup"
cp "tools/$TOOL/go.sum" "tools/$TOOL/go.sum.backup" 2>/dev/null || true

echo ""
echo "📝 Step 2: Current go.mod content:"
echo "-----------------------------------"
cat "tools/$TOOL/go.mod"
echo "-----------------------------------"
echo ""

echo "🔧 Step 3: Simulating replace directive removal..."
cd "tools/$TOOL"

# Remove replace directives
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/runtime
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/pkg

echo ""
echo "📝 Modified go.mod content:"
echo "-----------------------------------"
cat go.mod
echo "-----------------------------------"
echo ""

echo "🔍 Step 4: Checking if dependencies exist remotely..."
if go mod download 2>&1 | grep -q "no matching versions"; then
    echo "⚠️  Dependencies not found remotely (expected for test)"
    echo ""
    echo "💡 This means you need to:"
    echo "   1. First tag and push runtime/$VERSION and pkg/$VERSION"
    echo "   2. Wait 5-10 minutes for Go proxy to cache them"
    echo "   3. Then tag tools/$TOOL/$VERSION"
    echo ""
else
    echo "✅ Dependencies would resolve from remote"
    echo ""
fi

echo "🏗️  Step 5: Testing build with local dependencies..."
# Restore for building
cp go.mod.backup go.mod
cp go.sum.backup go.sum 2>/dev/null || true

if go build -v ./...; then
    echo "✅ Build successful with local dependencies"
else
    echo "❌ Build failed"
    exit 1
fi

cd ../..

echo ""
echo "📊 Step 6: What would change in git:"
echo "-----------------------------------"
cd "tools/$TOOL"
cp go.mod.backup go.mod.temp
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/runtime go.mod.temp
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/pkg go.mod.temp
diff go.mod.backup go.mod.temp || true
rm go.mod.temp
cd ../..
echo "-----------------------------------"
echo ""

echo "🧹 Step 7: Restoring original files..."
mv "tools/$TOOL/go.mod.backup" "tools/$TOOL/go.mod"
mv "tools/$TOOL/go.sum.backup" "tools/$TOOL/go.sum" 2>/dev/null || true

echo ""
echo "================================================"
echo "✅ Test Complete - No Files Were Modified"
echo "================================================"
echo ""
echo "📋 Next Steps:"
echo ""
echo "================================================"
echo "  FOR TESTING (safe, deletable)"
echo "================================================"
echo ""
echo "⚠️  Use 'test/' prefix for test tags - they can be deleted within 5 min!"
echo ""
echo "1️⃣  Tag libraries with test/ prefix:"
echo "    git tag test/runtime/v0.0.1"
echo "    git tag test/pkg/v0.0.1"
echo "    git tag test/sdk/v0.0.1"
echo "    git push origin test/runtime/v0.0.1 test/pkg/v0.0.1 test/sdk/v0.0.1"
echo ""
echo "2️⃣  Test SDK (wait 2-3 min, NOT 10!):"
echo "    go get github.com/AltairaLabs/PromptKit/sdk@test/sdk/v0.0.1"
echo ""
echo "3️⃣  If successful, continue with tools (or DELETE within 5 min!)"
echo "    git push origin --delete test/runtime/v0.0.1 test/pkg/v0.0.1 test/sdk/v0.0.1"
echo ""
echo "📚 For full testing guide: docs/devops/testing-releases.md"
echo ""
echo "================================================"
echo "  FOR PRODUCTION RELEASE (permanent!)"
echo "================================================"
echo ""
echo "💡 Use the automated script (RECOMMENDED):"
echo "    ./scripts/release.sh v1.0.0"
echo ""
echo "Or manually:"
echo ""
echo "1️⃣  Tag and push libraries:"
echo "    git tag runtime/v1.0.0"
echo "    git tag pkg/v1.0.0"
echo "    git tag sdk/v1.0.0"
echo "    git push origin runtime/v1.0.0 pkg/v1.0.0 sdk/v1.0.0"
echo ""
echo "2️⃣  Wait 10 minutes for Go proxy to cache"
echo ""
echo "3️⃣  Verify SDK is available:"
echo "    go get github.com/AltairaLabs/PromptKit/sdk@v1.0.0"
echo ""
echo "4️⃣  Update tools and tag them"
echo "    (See release.sh for full automation)"
echo ""
echo "📚 Full release guide: docs/devops/release-process.md"
echo "� Automation guide: docs/devops/release-automation.md"
echo ""

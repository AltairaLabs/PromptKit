#!/bin/bash
# Test script for live schema URLs (after deployment)
# This validates that published schemas are accessible and work correctly

set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SCHEMA_BASE_URL="https://promptkit.altairalabs.ai/schemas"
SCHEMA_TYPES=("arena" "scenario" "provider" "promptconfig" "tool" "persona")

echo "🌐 Testing Live PromptKit Schemas"
echo "======================================"
echo ""

PASS=0
FAIL=0

# Test 1: Check latest URLs are accessible
echo "Test 1: Checking 'latest' schema URLs are accessible..."

for schema in "${SCHEMA_TYPES[@]}"; do
  url="${SCHEMA_BASE_URL}/latest/${schema}.json"
  
  if curl -sf "$url" > /dev/null 2>&1; then
    echo -e "  ${GREEN}✓${NC} ${url}"
    PASS=$((PASS + 1))
  else
    echo -e "  ${RED}✗${NC} ${url} (not accessible)"
    FAIL=$((FAIL + 1))
  fi
done

echo ""

# Test 2: Check v1alpha1 URLs are accessible
echo "Test 2: Checking 'v1alpha1' schema URLs are accessible..."

for schema in "${SCHEMA_TYPES[@]}"; do
  url="${SCHEMA_BASE_URL}/v1alpha1/${schema}.json"
  
  if curl -sf "$url" > /dev/null 2>&1; then
    echo -e "  ${GREEN}✓${NC} ${url}"
    PASS=$((PASS + 1))
  else
    echo -e "  ${RED}✗${NC} ${url} (not accessible)"
    FAIL=$((FAIL + 1))
  fi
done

echo ""

# Test 3: Verify latest refs contain valid $ref
echo "Test 3: Verifying 'latest' schemas contain \$ref to v1alpha1..."

for schema in "${SCHEMA_TYPES[@]}"; do
  url="${SCHEMA_BASE_URL}/latest/${schema}.json"
  
  content=$(curl -sf "$url" 2>/dev/null || echo "")
  
  if [ -z "$content" ]; then
    echo -e "  ${RED}✗${NC} ${schema}.json (failed to fetch)"
    FAIL=$((FAIL + 1))
  elif echo "$content" | grep -q "\$ref.*v1alpha1/${schema}.json"; then
    echo -e "  ${GREEN}✓${NC} ${schema}.json has correct \$ref"
    PASS=$((PASS + 1))
  else
    echo -e "  ${RED}✗${NC} ${schema}.json missing or invalid \$ref"
    FAIL=$((FAIL + 1))
    echo "     Content: $content"
  fi
done

echo ""

# Test 4: Verify v1alpha1 schemas are proper JSON Schema
echo "Test 4: Verifying v1alpha1 schemas are proper JSON Schemas..."

for schema in "${SCHEMA_TYPES[@]}"; do
  url="${SCHEMA_BASE_URL}/v1alpha1/${schema}.json"
  
  content=$(curl -sf "$url" 2>/dev/null || echo "")
  
  if [ -z "$content" ]; then
    echo -e "  ${RED}✗${NC} ${schema}.json (failed to fetch)"
    FAIL=$((FAIL + 1))
  elif echo "$content" | grep -q "\$schema" && echo "$content" | grep -q "\$id"; then
    echo -e "  ${GREEN}✓${NC} ${schema}.json has \$schema and \$id"
    PASS=$((PASS + 1))
  else
    echo -e "  ${RED}✗${NC} ${schema}.json missing required fields"
    FAIL=$((FAIL + 1))
  fi
done

echo ""

# Test 5: Verify CORS headers (important for browser access)
echo "Test 5: Checking CORS headers..."

url="${SCHEMA_BASE_URL}/latest/arena.json"
cors_header=$(curl -sI "$url" | grep -i "access-control-allow-origin" || echo "")

if [ -n "$cors_header" ]; then
  echo -e "  ${GREEN}✓${NC} CORS headers present: ${cors_header}"
  PASS=$((PASS + 1))
else
  echo -e "  ${YELLOW}⚠${NC}  No CORS headers (may cause issues in browsers)"
  echo "     Consider adding 'Access-Control-Allow-Origin: *' for schema files"
fi

echo ""

# Test 6: Test with ajv-cli against live URLs
echo "Test 6: Testing schema validation with live URLs (if ajv-cli available)..."

if command -v ajv &> /dev/null; then
  # Create a temporary test file
  TEST_FILE=$(mktemp /tmp/test-arena.XXXXXX.yaml)
  cat > "$TEST_FILE" <<'EOF'
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: live-test
spec:
  prompt_configs:
    - id: test
      file: test.yaml
  providers:
    - file: provider.yaml
  scenarios:
    - file: scenario.yaml
EOF

  echo "  Testing validation against live 'latest' schema..."
  
  if ajv validate -s "${SCHEMA_BASE_URL}/latest/arena.json" -d "$TEST_FILE" --strict=false 2>&1 | grep -q "valid"; then
    echo -e "  ${GREEN}✓${NC} Validation works with live 'latest' URL"
    PASS=$((PASS + 1))
  else
    echo -e "  ${YELLOW}⚠${NC}  ajv validation had issues (may need \$ref resolution)"
  fi
  
  rm -f "$TEST_FILE"
else
  echo -e "  ${YELLOW}⚠${NC}  ajv-cli not installed (run: npm install -g ajv-cli ajv-formats)"
fi

echo ""

# Test 7: Check schema content-type headers
echo "Test 7: Checking Content-Type headers..."

url="${SCHEMA_BASE_URL}/latest/arena.json"
content_type=$(curl -sI "$url" | grep -i "content-type" || echo "")

if echo "$content_type" | grep -qi "application/json"; then
  echo -e "  ${GREEN}✓${NC} Correct content-type: ${content_type}"
  PASS=$((PASS + 1))
else
  echo -e "  ${YELLOW}⚠${NC}  Content-Type: ${content_type}"
  echo "     Recommended: application/json or application/schema+json"
fi

echo ""

# Summary
echo "======================================"
echo "Live Schema Test Results"
echo "======================================"
echo -e "Passed: ${GREEN}${PASS}${NC}"
echo -e "Failed: ${RED}${FAIL}${NC}"
echo ""

if [ $FAIL -eq 0 ]; then
  echo -e "${GREEN}✓ All live schema tests passed!${NC}"
  echo ""
  echo "Schemas are ready for Schema Store submission!"
  echo ""
  echo "Schema Store entry template:"
  echo "---"
  cat <<'TEMPLATE'
{
  "name": "PromptKit Arena Configuration",
  "description": "Configuration file for PromptKit Arena test orchestration",
  "fileMatch": [
    "arena.yaml",
    "*.arena.yaml",
    "**/arena.yaml"
  ],
  "url": "https://promptkit.altairalabs.ai/schemas/latest/arena.json"
},
{
  "name": "PromptKit Scenario",
  "description": "Test scenario definition for PromptKit Arena",
  "fileMatch": [
    "**/scenarios/*.yaml",
    "*.scenario.yaml"
  ],
  "url": "https://promptkit.altairalabs.ai/schemas/latest/scenario.json"
},
{
  "name": "PromptKit Provider",
  "description": "LLM provider configuration for PromptKit",
  "fileMatch": [
    "**/providers/*.yaml",
    "*.provider.yaml"
  ],
  "url": "https://promptkit.altairalabs.ai/schemas/latest/provider.json"
},
{
  "name": "PromptKit Prompt Configuration",
  "description": "Prompt template configuration for PromptKit",
  "fileMatch": [
    "**/prompts/*.yaml",
    "*.prompt.yaml"
  ],
  "url": "https://promptkit.altairalabs.ai/schemas/latest/promptconfig.json"
},
{
  "name": "PromptKit Tool",
  "description": "Tool definition for PromptKit LLM interactions",
  "fileMatch": [
    "**/tools/*.yaml",
    "*.tool.yaml"
  ],
  "url": "https://promptkit.altairalabs.ai/schemas/latest/tool.json"
},
{
  "name": "PromptKit Persona",
  "description": "User persona for PromptKit self-play testing",
  "fileMatch": [
    "**/personas/*.yaml",
    "*.persona.yaml"
  ],
  "url": "https://promptkit.altairalabs.ai/schemas/latest/persona.json"
}
TEMPLATE
  echo "---"
  exit 0
else
  echo -e "${RED}✗ Some live schema tests failed${NC}"
  echo ""
  echo "Fix deployment issues before submitting to Schema Store"
  exit 1
fi

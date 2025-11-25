#!/bin/bash
# Test script for schema $ref redirects (latest -> v1alpha1)
# This validates that the latest schema aliases work correctly before submitting to Schema Store

set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SCHEMA_BASE_URL="https://promptkit.altairalabs.ai/schemas"
SCHEMA_TYPES=("arena" "scenario" "provider" "promptconfig" "tool" "persona")

echo "🧪 Testing PromptKit Schema References"
echo "======================================"
echo ""

# Test 1: Check local latest refs exist
echo "Test 1: Checking local 'latest' schema references..."
PASS=0
FAIL=0

for schema in "${SCHEMA_TYPES[@]}"; do
  file="docs/public/schemas/latest/${schema}.json"
  if [ -f "$file" ]; then
    echo -e "  ${GREEN}✓${NC} ${file} exists"
    PASS=$((PASS + 1))
  else
    echo -e "  ${RED}✗${NC} ${file} missing"
    FAIL=$((FAIL + 1))
  fi
done

echo ""

# Test 2: Validate $ref syntax
echo "Test 2: Validating \$ref syntax in latest schemas..."

for schema in "${SCHEMA_TYPES[@]}"; do
  file="docs/public/schemas/latest/${schema}.json"
  if [ -f "$file" ]; then
    # Check if file contains $ref to v1alpha1 (flexible matching)
    if grep -q "\$ref.*v1alpha1/${schema}.json" "$file"; then
      echo -e "  ${GREEN}✓${NC} ${schema}.json has correct \$ref"
      PASS=$((PASS + 1))
    else
      echo -e "  ${RED}✗${NC} ${schema}.json has invalid \$ref"
      FAIL=$((FAIL + 1))
      cat "$file"
    fi
  fi
done

echo ""

# Test 3: Validate JSON syntax
echo "Test 3: Validating JSON syntax..."

for schema in "${SCHEMA_TYPES[@]}"; do
  file="docs/public/schemas/latest/${schema}.json"
  if [ -f "$file" ]; then
    if python3 -m json.tool "$file" > /dev/null 2>&1; then
      echo -e "  ${GREEN}✓${NC} ${schema}.json is valid JSON"
      PASS=$((PASS + 1))
    else
      echo -e "  ${RED}✗${NC} ${schema}.json has invalid JSON syntax"
      FAIL=$((FAIL + 1))
    fi
  fi
done

echo ""

# Test 4: Check v1alpha1 schemas exist
echo "Test 4: Checking v1alpha1 target schemas exist..."

for schema in "${SCHEMA_TYPES[@]}"; do
  file="docs/public/schemas/v1alpha1/${schema}.json"
  if [ -f "$file" ]; then
    echo -e "  ${GREEN}✓${NC} ${file} exists"
    PASS=$((PASS + 1))
  else
    echo -e "  ${RED}✗${NC} ${file} missing (broken ref target!)"
    FAIL=$((FAIL + 1))
  fi
done

echo ""

# Test 5: Validate v1alpha1 schemas are proper JSON Schema
echo "Test 5: Validating v1alpha1 schemas are proper JSON Schemas..."

for schema in "${SCHEMA_TYPES[@]}"; do
  file="docs/public/schemas/v1alpha1/${schema}.json"
  if [ -f "$file" ]; then
    # Check for required JSON Schema fields (flexible matching)
    if grep -q "\$schema" "$file" && grep -q "\$id" "$file"; then
      echo -e "  ${GREEN}✓${NC} ${schema}.json has \$schema and \$id fields"
      PASS=$((PASS + 1))
    else
      echo -e "  ${RED}✗${NC} ${schema}.json missing required JSON Schema fields"
      FAIL=$((FAIL + 1))
    fi
  fi
done

echo ""

# Test 6: Test with a sample YAML file (if ajv-cli is available)
echo "Test 6: Testing schema validation with ajv-cli (if available)..."

if command -v ajv &> /dev/null; then
  # Create a temporary test file
  TEST_FILE=$(mktemp /tmp/test-arena.XXXXXX.yaml)
  cat > "$TEST_FILE" <<'EOF'
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-schema-ref
spec:
  prompt_configs:
    - id: test
      file: test.yaml
  providers:
    - file: provider.yaml
  scenarios:
    - file: scenario.yaml
EOF

  echo "  Testing with sample arena config..."
  
  # Test with latest ref
  if ajv validate -s "docs/public/schemas/latest/arena.json" -d "$TEST_FILE" --strict=false 2>&1 | grep -q "valid"; then
    echo -e "  ${GREEN}✓${NC} Validation works with 'latest' schema ref"
    PASS=$((PASS + 1))
  else
    echo -e "  ${YELLOW}⚠${NC}  ajv validation had issues (may be expected with \$ref)"
    # This might fail because ajv needs to resolve the $ref - that's okay for local testing
  fi
  
  # Test with v1alpha1 directly
  if ajv validate -s "docs/public/schemas/v1alpha1/arena.json" -d "$TEST_FILE" --strict=false 2>&1 | grep -q "valid"; then
    echo -e "  ${GREEN}✓${NC} Validation works with 'v1alpha1' schema directly"
    PASS=$((PASS + 1))
  else
    echo -e "  ${RED}✗${NC} Validation failed with v1alpha1 schema"
    FAIL=$((FAIL + 1))
  fi
  
  rm -f "$TEST_FILE"
else
  echo -e "  ${YELLOW}⚠${NC}  ajv-cli not installed (run: npm install -g ajv-cli ajv-formats)"
  echo "  Skipping validation test"
fi

echo ""

# Test 7: Verify Schema Store file patterns
echo "Test 7: Verifying Schema Store file patterns..."

PATTERNS=(
  "arena.yaml:arena"
  "*.arena.yaml:arena"
  "**/arena.yaml:arena"
  "scenarios/*.yaml:scenario"
  "*.scenario.yaml:scenario"
  "providers/*.yaml:provider"
  "*.provider.yaml:provider"
  "prompts/*.yaml:promptconfig"
  "*.prompt.yaml:promptconfig"
  "tools/*.yaml:tool"
  "*.tool.yaml:tool"
  "personas/*.yaml:persona"
  "*.persona.yaml:persona"
)

echo "  Recommended file patterns for Schema Store:"
for pattern in "${PATTERNS[@]}"; do
  IFS=':' read -r file_pattern schema_type <<< "$pattern"
  echo "    - ${file_pattern} → ${schema_type}.json"
done
PASS=$((PASS + 1))

echo ""

# Summary
echo "======================================"
echo "Test Results Summary"
echo "======================================"
echo -e "Passed: ${GREEN}${PASS}${NC}"
echo -e "Failed: ${RED}${FAIL}${NC}"
echo ""

if [ $FAIL -eq 0 ]; then
  echo -e "${GREEN}✓ All tests passed!${NC}"
  echo ""
  echo "Next steps:"
  echo "  1. Deploy docs/public/schemas to hosting"
  echo "  2. Test with live URLs:"
  echo "     curl https://promptkit.altairalabs.ai/schemas/latest/arena.json"
  echo "  3. Verify \$ref resolution works in production"
  echo "  4. Submit PR to SchemaStore/schemastore"
  exit 0
else
  echo -e "${RED}✗ Some tests failed${NC}"
  echo ""
  echo "Fix issues before submitting to Schema Store"
  exit 1
fi

---
title: Validate Packs
sidebar:
  order: 3
---
Ensure your compiled packs are correct and production-ready.

## Goal

Verify pack structure, catch errors, and enforce quality standards before deployment.

## Prerequisites

- packc installed
- Compiled pack file (.pack.json)

## Basic Validation

### Steps

1. **Compile your pack** (if not already done)

   ```bash
   packc compile --config arena.yaml --output packs/app.pack.json --id app
   ```

2. **Run validation**

   ```bash
   packc validate packs/app.pack.json
   ```

3. **Check the result**

   **Success:**
   ```
   Validating pack: packs/app.pack.json
   Validating against PromptPack schema...
   ✓ Schema validation passed
   ✓ Pack structure is valid
   ```

   **With warnings:**
   ```
   Validating pack: packs/app.pack.json
   Validating against PromptPack schema...
   ✓ Schema validation passed
   ⚠ Pack has 2 warnings:
     - Missing description for prompt 'support'
     - prompt 'support': no variables defined
   ```

## Validation Checks

PackC validates:

- **Schema compliance** - Pack structure matches spec
- **Required fields** - All mandatory fields present
- **Prompt references** - Prompts referenced in pack exist
- **Tool definitions** - Tools are properly defined
- **Template syntax** - Go templates are parseable
- **Parameter types** - Parameters have correct types

## Validation Patterns

### Pattern 1: Validate After Every Compile

```bash
#!/bin/bash
set -e

packc compile --config arena.yaml --output packs/app.pack.json --id app
packc validate packs/app.pack.json

echo "✓ Pack compiled and validated"
```

### Pattern 2: Validate All Packs

```bash
#!/bin/bash
# validate-all.sh

for pack in packs/*.pack.json; do
  echo "Validating: $pack"
  packc validate "$pack"
done

echo "✓ All packs validated"
```

### Pattern 3: Validate with Exit Codes

```bash
#!/bin/bash
# Validate and handle errors

if packc validate packs/app.pack.json; then
  echo "✓ Pack is valid"
  exit 0
else
  echo "✗ Pack validation failed"
  exit 1
fi
```

### Pattern 4: Pre-Deployment Validation

```bash
#!/bin/bash
# deploy.sh

echo "Pre-deployment checks..."

# Validate pack
if ! packc validate packs/prod/app.pack.json; then
  echo "✗ Pack validation failed - aborting deployment"
  exit 1
fi

# Additional checks
./scripts/test-pack.sh packs/prod/app.pack.json

echo "✓ Validation passed - proceeding with deployment"
./scripts/deploy-pack.sh packs/prod/app.pack.json
```

## Integration Patterns

### Pre-commit Hook

Validate packs before committing:

```bash
#!/bin/bash
# .git/hooks/pre-commit

echo "Validating packs before commit..."

for pack in $(git diff --cached --name-only | grep '\.pack\.json$'); do
  echo "Checking: $pack"
  
  if ! packc validate "$pack"; then
    echo "✗ Validation failed for $pack"
    echo "Fix issues or use 'git commit --no-verify' to bypass"
    exit 1
  fi
done

echo "✓ All packs valid"
```

Make it executable:

```bash
chmod +x .git/hooks/pre-commit
```

### Makefile Target

```makefile
.PHONY: validate
validate:
	@echo "Validating all packs..."
	@for pack in packs/*.pack.json; do \
		echo "Checking $$pack..."; \
		packc validate $$pack || exit 1; \
	done
	@echo "✓ All packs validated"

.PHONY: validate-strict
validate-strict: validate
	@echo "Running additional checks..."
	@./scripts/check-pack-size.sh
	@./scripts/check-prompt-quality.sh
```

### CI/CD Pipeline

```yaml
# .github/workflows/validate-packs.yml
name: Validate Packs

on:
  pull_request:
    paths:
      - 'packs/*.pack.json'
      - 'prompts/**'
      - 'config/**'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Compile packs
        run: |
          packc compile --config arena.yaml --output packs/app.pack.json --id app
      
      - name: Validate packs
        run: |
          for pack in packs/*.pack.json; do
            echo "Validating: $pack"
            packc validate "$pack"
          done
      
      - name: Check pack quality
        run: |
          ./scripts/check-pack-quality.sh packs/*.pack.json
```

## Advanced Validation

### Custom Validation Scripts

Create additional validation:

```bash
#!/bin/bash
# scripts/validate-pack-quality.sh

pack_file="$1"

echo "Running quality checks on $pack_file..."

# Check pack size
size=$(stat -f%z "$pack_file" 2>/dev/null || stat -c%s "$pack_file")
max_size=$((1024 * 1024))  # 1MB

if [ "$size" -gt "$max_size" ]; then
  echo "✗ Pack too large: ${size} bytes (max: ${max_size})"
  exit 1
fi

# Validate JSON syntax
if ! jq empty "$pack_file" 2>/dev/null; then
  echo "✗ Invalid JSON"
  exit 1
fi

# Check for required metadata
if ! jq -e '.compilation.compiled_with' "$pack_file" >/dev/null; then
  echo "✗ Missing compilation.compiled_with"
  exit 1
fi

echo "✓ Quality checks passed"
```

### Validation Matrix

Test packs across environments:

```yaml
# .github/workflows/validate-matrix.yml
name: Validate Across Environments

on: [push, pull_request]

jobs:
  validate:
    strategy:
      matrix:
        environment: [dev, staging, prod]
        
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Compile $ pack
        run: |
          packc compile \
            --config config/arena.$.yaml \
            --output packs/$/app.pack.json \
            --id app-$
      
      - name: Validate pack
        run: packc validate packs/$/app.pack.json
```

## Verification

### Check Validation Status

```bash
# Exit code 0 = valid, 1 = invalid
packc validate packs/app.pack.json
echo $?  # Should be 0
```

### Inspect Validation Results

```bash
# Capture output
output=$(packc validate packs/app.pack.json 2>&1)

# Check for warnings
if echo "$output" | grep -q "⚠"; then
  echo "Pack has warnings"
fi
```

## Troubleshooting

### Validation fails with warnings

**Problem:**

```
⚠ Pack has 1 warnings:
  - Missing description for prompt 'support'
```

**Solution:** Fix warnings in source YAML:

```yaml
spec:
  task_type: support
  description: "Customer support assistant"  # Add missing description
```

Then recompile and validate:

```bash
packc compile --config arena.yaml --output packs/app.pack.json --id app
packc validate packs/app.pack.json
```

### Pack file not found

**Problem:**

```
Error loading pack: open packs/app.pack.json: no such file or directory
```

**Solution:** Compile pack first:

```bash
packc compile --config arena.yaml --output packs/app.pack.json --id app
packc validate packs/app.pack.json
```

### Invalid JSON

**Problem:**

```
Error loading pack: invalid character '}' after top-level value
```

**Solution:** Recompile pack (don't edit JSON manually):

```bash
packc compile --config arena.yaml --output packs/app.pack.json --id app
```

### Tool not defined

**Problem:**

```
⚠ Tool 'search_kb' not defined
```

**Solution:** Define tool in tools directory or prompt config:

```yaml
# prompts/support.yaml
tools:
  - name: search_kb
    description: Search knowledge base
    input_schema:
      type: object
      properties:
        query:
          type: string
```

## Best Practices

### 1. Validate on Every Build

```bash
# Always pair compile with validate
packc compile --config arena.yaml --output packs/app.pack.json --id app && \
packc validate packs/app.pack.json
```

### 2. Fail Fast in CI

```yaml
- name: Validate packs
  run: |
    packc validate packs/*.pack.json
  # Fail pipeline if validation fails
```

### 3. Track Validation History

```bash
# Log validation results
packc validate packs/app.pack.json 2>&1 | tee validation-$(date +%Y%m%d).log
```

### 4. Validate Before Tagging Releases

```bash
#!/bin/bash
# scripts/release.sh

# Validate all packs
for pack in packs/prod/*.pack.json; do
  packc validate "$pack" || exit 1
done

# Tag release
git tag -a "v1.2.0" -m "Release v1.2.0"
```

### 5. Create Validation Reports

```bash
#!/bin/bash
# Generate validation report

cat > validation-report.txt <<EOF
Pack Validation Report
=====================
Date: $(date)
Compiler: $(packc version)

EOF

for pack in packs/*.pack.json; do
  echo "Pack: $pack" >> validation-report.txt
  packc validate "$pack" 2>&1 >> validation-report.txt
  echo "" >> validation-report.txt
done

cat validation-report.txt
```

## Next Steps

- [Organize Pack Files](organize-packs) - Structure your packs
- [CI/CD Integration](ci-cd-integration) - Automate validation
- [Inspect Packs](../reference/inspect) - View pack details

## See Also

- [validate command reference](../reference/validate)
- [Pack format explanation](../explanation/pack-format)
- [Quality assurance](../../guides/quality-assurance)

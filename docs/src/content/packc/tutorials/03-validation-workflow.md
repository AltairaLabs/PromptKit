---
title: '03: Validation Workflows'
docType: tutorial
order: 3
---
# Tutorial 3: Validation Workflows

Build a workflow that ensures pack quality before deployment.

## Learning Objectives

- Create automated validation workflows
- Catch errors before deployment
- Integrate validation in development
- Set up pre-commit validation

## Time Required

**25 minutes**

## Prerequisites

- Completed [Tutorial 2: Multi-Prompt Packs](02-multi-prompt)
- Git installed

## Step 1: Setup Project

```bash
mkdir validation-workflow
cd validation-workflow
mkdir -p prompts config packs scripts .git/hooks
git init
```

## Step 2: Create Prompts

Create a sample prompt:

```bash
cat > prompts/assistant.yaml <<'EOF'
apiVersion: promptkit/v1
kind: PromptConfig
spec:
  task_type: assistant
  name: AI Assistant
  description: General purpose assistant
  system_prompt: You are a helpful AI assistant.
  user_template: "User: "
  template_engine: go
  parameters:
    temperature: 0.7
    max_tokens: 500
EOF
```

Create arena.yaml:

```bash
cat > config/arena.yaml <<'EOF'
prompts:
  - ../prompts/assistant.yaml
EOF
```

## Step 3: Create Validation Script

```bash
cat > scripts/validate-pack.sh <<'EOF'
#!/bin/bash
set -e

PACK_FILE="$1"

if [ -z "$PACK_FILE" ]; then
  echo "Usage: $0 <pack-file>"
  exit 1
fi

echo "Validating: $PACK_FILE"

# 1. Check file exists
if [ ! -f "$PACK_FILE" ]; then
  echo "❌ Pack file not found"
  exit 1
fi

# 2. Validate with packc
if ! packc validate "$PACK_FILE"; then
  echo "❌ Pack validation failed"
  exit 1
fi

# 3. Check pack size
size=$(stat -f%z "$PACK_FILE" 2>/dev/null || stat -c%s "$PACK_FILE")
max_size=$((1024 * 1024))  # 1MB

if [ "$size" -gt "$max_size" ]; then
  echo "⚠ Warning: Pack size ${size} bytes exceeds recommended ${max_size} bytes"
fi

# 4. Verify JSON structure
if ! jq empty "$PACK_FILE" 2>/dev/null; then
  echo "❌ Invalid JSON"
  exit 1
fi

# 5. Check required fields
if ! jq -e '.id' "$PACK_FILE" >/dev/null; then
  echo "❌ Missing pack ID"
  exit 1
fi

if ! jq -e '.prompts' "$PACK_FILE" >/dev/null; then
  echo "❌ Missing prompts"
  exit 1
fi

echo "✅ Pack validation passed"
EOF

chmod +x scripts/validate-pack.sh
```

## Step 4: Build and Validate Workflow

Create build script:

```bash
cat > scripts/build-and-validate.sh <<'EOF'
#!/bin/bash
set -e

echo "=== Building Packs ==="
mkdir -p packs

packc compile \
  --config config/arena.yaml \
  --output packs/assistant.pack.json \
  --id assistant

echo ""
echo "=== Validating Packs ==="
./scripts/validate-pack.sh packs/assistant.pack.json

echo ""
echo "=== Inspecting Pack ==="
packc inspect packs/assistant.pack.json

echo ""
echo "✅ Build and validation complete"
EOF

chmod +x scripts/build-and-validate.sh
```

Run it:

```bash
./scripts/build-and-validate.sh
```

## Step 5: Pre-Commit Hook

Set up automatic validation before commits:

```bash
cat > .git/hooks/pre-commit <<'EOF'
#!/bin/bash

echo "Running pre-commit validation..."

# Compile all packs
mkdir -p packs
packc compile --config config/arena.yaml --output packs/assistant.pack.json --id assistant

# Validate
if ! packc validate packs/assistant.pack.json; then
  echo ""
  echo "❌ Pack validation failed!"
  echo "Fix errors or use 'git commit --no-verify' to bypass"
  exit 1
fi

echo "✅ Pre-commit validation passed"
git add packs/assistant.pack.json
EOF

chmod +x .git/hooks/pre-commit
```

Test it:

```bash
git add prompts/ config/
git commit -m "Add assistant prompt"
```

## Step 6: Makefile Workflow

Create a Makefile for common workflows:

```bash
cat > Makefile <<'EOF'
.PHONY: build validate clean test all

all: build validate

build:
	@echo "Building packs..."
	@mkdir -p packs
	@packc compile --config config/arena.yaml --output packs/assistant.pack.json --id assistant

validate: build
	@echo "Validating packs..."
	@./scripts/validate-pack.sh packs/assistant.pack.json

test: validate
	@echo "Running pack tests..."
	@# Add SDK tests here

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf packs/

inspect: build
	@packc inspect packs/assistant.pack.json
EOF
```

Use it:

```bash
make build      # Build packs
make validate   # Build and validate
make all        # Build and validate
make inspect    # Build and inspect
make clean      # Clean up
```

## Step 7: Continuous Validation

Create a validation loop for development:

```bash
cat > scripts/watch-and-validate.sh <<'EOF'
#!/bin/bash

echo "Watching for changes..."

while true; do
  # Watch for changes to prompt files
  if find prompts/ -type f -mmin -0.1 | grep -q .; then
    echo ""
    echo "Changes detected, rebuilding..."
    make validate
  fi
  
  sleep 1
done
EOF

chmod +x scripts/watch-and-validate.sh
```

Run in a separate terminal:

```bash
./scripts/watch-and-validate.sh
```

## What You Learned

- ✅ Created validation scripts
- ✅ Built automated workflows
- ✅ Set up pre-commit hooks
- ✅ Used Makefiles for consistency
- ✅ Enabled continuous validation

## Best Practices

### 1. Always Validate

```bash
# Never deploy without validation
packc compile ... && packc validate ...
```

### 2. Fail Fast

```bash
# Stop on first error
set -e
```

### 3. Multiple Validation Levels

```bash
# Basic: packc validate
# Medium: + size checks
# Strict: + custom validation rules
```

### 4. Document Failures

```bash
# Log validation results
packc validate pack.json 2>&1 | tee validation.log
```

## Next Steps

- **[Tutorial 4: Pack Management](04-pack-management)** - Organize and version packs
- **[How-To: Validate Packs](../how-to/validate-packs)** - More validation strategies
- **[How-To: CI/CD Integration](../how-to/ci-cd-integration)** - Automate in CI/CD

## Summary

You now have:
- Automated validation workflows
- Pre-commit quality gates
- Development validation loop
- Production-ready validation

Great work! 🎉

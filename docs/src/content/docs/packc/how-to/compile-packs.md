---
title: Compile Packs
sidebar:
  order: 2
---
Compile YAML prompt configurations into production-ready pack files.

## Goal

Transform your arena.yaml configuration or individual prompt files into optimized `.pack.json` files ready for use with the PromptKit SDK.

## Prerequisites

- packc installed ([installation guide](install))
- Prompt configuration files (arena.yaml or individual .yaml files)
- Basic understanding of pack structure

## Method 1: Compile from arena.yaml (Recommended)

This is the standard approach for production packs.

### Steps

1. **Prepare your arena.yaml**

   ```yaml
   # config/arena.yaml
   prompts:
     - prompts/support.yaml
     - prompts/sales.yaml
     - prompts/marketing.yaml
   
   tools_directory: ./tools
   ```

2. **Run the compile command**

   ```bash
   packc compile \
     --config config/arena.yaml \
     --output packs/customer-service.pack.json \
     --id customer-service
   ```

3. **Verify the output**

   ```bash
   ls -lh packs/customer-service.pack.json
   packc inspect packs/customer-service.pack.json
   ```

### Expected Output

```
Loaded 3 prompt configs from memory repository
Compiling 3 prompts into pack 'customer-service'...
✓ Pack compiled successfully: packs/customer-service.pack.json
  Contains 3 prompts: [support, sales, marketing]
```

## Method 2: Compile Single Prompt

For individual prompt development or testing.

### Steps

1. **Create a prompt file**

   ```yaml
   # prompts/support.yaml
   apiVersion: promptkit.altairalabs.ai/v1alpha1
   kind: PromptConfig
   spec:
     task_type: customer-support
     name: Customer Support Agent
     system_prompt: |
       You are a helpful customer support agent.
     user_template: |
       Customer: 
   ```

2. **Compile the prompt**

   ```bash
   packc compile-prompt \
     --prompt prompts/support.yaml \
     --output packs/support.pack.json
   ```

3. **Verify compilation**

   ```bash
   packc validate packs/support.pack.json
   ```

## Common Compilation Patterns

### Pattern 1: Environment-Specific Packs

```bash
# Development
packc compile \
  --config config/arena.dev.yaml \
  --output packs/app-dev.pack.json \
  --id app-dev

# Staging
packc compile \
  --config config/arena.staging.yaml \
  --output packs/app-staging.pack.json \
  --id app-staging

# Production
packc compile \
  --config config/arena.prod.yaml \
  --output packs/app-prod.pack.json \
  --id app-prod
```

### Pattern 2: Versioned Packs

```bash
VERSION="1.2.0"

packc compile \
  --config arena.yaml \
  --output "packs/app-v${VERSION}.pack.json" \
  --id app

# Also create latest symlink
ln -sf "app-v${VERSION}.pack.json" packs/app-latest.pack.json
```

### Pattern 3: Multi-Pack Build

```bash
#!/bin/bash
# build-all-packs.sh

for config in config/*.yaml; do
  name=$(basename "$config" .yaml)
  echo "Building pack: $name"
  
  packc compile \
    --config "$config" \
    --output "packs/${name}.pack.json" \
    --id "$name"
done
```

### Pattern 4: Batch Compile with Validation

```bash
#!/bin/bash
set -e

echo "Compiling packs..."
packc compile --config arena.yaml --output packs/app.pack.json --id app

echo "Validating pack..."
packc validate packs/app.pack.json

echo "Inspecting pack..."
packc inspect packs/app.pack.json

echo "✓ Build complete"
```

## Advanced Compilation

### Organize Output by Environment

```bash
mkdir -p packs/{dev,staging,prod}

# Compile to environment directories
packc compile \
  --config config/arena.prod.yaml \
  --output packs/prod/app.pack.json \
  --id app
```

### Add Metadata to Build

```bash
# Create build info
cat > packs/build-info.txt <<EOF
Build: $(date -u +"%Y-%m-%d %H:%M:%S UTC")
Compiler: $(packc version)
Git Commit: $(git rev-parse --short HEAD)
Branch: $(git branch --show-current)
EOF

# Compile pack
packc compile --config arena.yaml --output packs/app.pack.json --id app
```

### Compile with Pre/Post Scripts

```bash
#!/bin/bash
# build-with-hooks.sh

# Pre-compilation checks
echo "Running pre-compilation checks..."
./scripts/validate-prompts.sh

# Compile
echo "Compiling pack..."
packc compile --config arena.yaml --output packs/app.pack.json --id app

# Post-compilation tasks
echo "Running post-compilation tasks..."
packc validate packs/app.pack.json
./scripts/test-pack.sh packs/app.pack.json
```

## Makefile Integration

```makefile
.PHONY: compile
compile:
	@mkdir -p packs
	packc compile --config arena.yaml --output packs/app.pack.json --id app

.PHONY: compile-all
compile-all:
	@mkdir -p packs
	@for config in config/*.yaml; do \
		name=$$(basename $$config .yaml); \
		echo "Compiling $$name..."; \
		packc compile --config $$config --output packs/$$name.pack.json --id $$name; \
	done

.PHONY: compile-and-validate
compile-and-validate: compile
	packc validate packs/app.pack.json

.PHONY: clean
clean:
	rm -rf packs/*.pack.json
```

Usage:

```bash
make compile                 # Compile main pack
make compile-all            # Compile all configs
make compile-and-validate   # Compile and validate
make clean                  # Remove compiled packs
```

## Verification

After compilation, verify your pack:

### Check File Exists

```bash
test -f packs/app.pack.json && echo "✓ Pack file created"
```

### Check File Size

```bash
# Pack should be < 1MB for most use cases
ls -lh packs/app.pack.json
```

### Validate Structure

```bash
packc validate packs/app.pack.json
```

### Inspect Contents

```bash
packc inspect packs/app.pack.json
```

### Test with SDK

```go
pack, err := manager.LoadPack("./packs/app.pack.json")
if err != nil {
    log.Fatal("Pack load failed:", err)
}
fmt.Println("✓ Pack loaded successfully")
```

## Troubleshooting

### arena.yaml not found

**Problem:**

```
Error loading arena config: open arena.yaml: no such file or directory
```

**Solution:** Specify correct path:

```bash
packc compile --config ./config/arena.yaml --output packs/app.pack.json --id app
```

### Missing required fields

**Problem:**

```
Error: --output and --id are required
```

**Solution:** Provide all required flags:

```bash
packc compile --config arena.yaml --output packs/app.pack.json --id my-app
```

### Prompt file not found

**Problem:**

```
Error loading prompt: open prompts/support.yaml: no such file or directory
```

**Solution:** Check paths in arena.yaml are relative to config file:

```yaml
# arena.yaml should reference prompts correctly
prompts:
  - ../prompts/support.yaml  # Adjust path as needed
```

### Invalid YAML syntax

**Problem:**

```
Error parsing prompt config: yaml: line 5: mapping values are not allowed
```

**Solution:** Validate YAML syntax:

```bash
# Check YAML with yamllint
yamllint prompts/support.yaml
```

### Media file warnings

**Problem:**

```
⚠ Media validation warnings for customer-support:
  - Image file not found: images/logo.png
```

**Solution:** Ensure media files exist or update paths:

```bash
mkdir -p images
cp assets/logo.png images/
```

### Output directory missing

**Problem:**

```
Failed to write pack file: no such file or directory
```

**Solution:** Create output directory:

```bash
mkdir -p packs
packc compile --config arena.yaml --output packs/app.pack.json --id app
```

## Best Practices

### 1. Always Validate After Compiling

```bash
packc compile --config arena.yaml --output packs/app.pack.json --id app
packc validate packs/app.pack.json
```

### 2. Use Descriptive Pack IDs

```bash
# Good
--id customer-support-prod-v1

# Avoid
--id pack1
```

### 3. Organize Packs by Purpose

```
packs/
├── dev/
│   └── app.pack.json
├── staging/
│   └── app.pack.json
└── prod/
    └── app.pack.json
```

### 4. Version Your Packs

```bash
packc compile \
  --config arena.yaml \
  --output "packs/app-v1.2.0.pack.json" \
  --id app-v1.2.0
```

### 5. Keep Packs Small

- Compile separate packs for different features
- Don't bundle unrelated prompts together
- Consider pack size for deployment

## Next Steps

- [Validate Packs](validate-packs) - Ensure pack quality
- [Organize Pack Files](organize-packs) - Structure your packs
- [CI/CD Integration](ci-cd-integration) - Automate builds

## See Also

- [compile command reference](../reference/compile)
- [compile-prompt command reference](../reference/compile-prompt)
- [Pack format explanation](../explanation/pack-format)

---
layout: default
title: For DevOps Engineers
parent: Getting Started
nav_order: 3
---

# Getting Started as a DevOps Engineer

**Goal**: Compile, package, and version prompts for production deployment.

**Tool**: PackC (PromptPack Compiler)

**Time to Success**: 5-10 minutes

---

## What You'll Accomplish

By the end of this guide, you'll have:

- ✅ PackC installed and configured
- ✅ Compiled your first PromptPack
- ✅ Validated pack structure
- ✅ Integrated into a build pipeline

---

## Prerequisites

- Go 1.22 or later installed
- Basic understanding of build systems
- Prompt source files (YAML or JSON)

---

## Step 1: Install PackC

```bash
# Clone the repository
git clone https://github.com/AltairaLabs/PromptKit.git
cd PromptKit

# Build PackC
cd tools/packc
go build -o packc .

# Verify installation
./packc --version
```

Or install from the root using Make:

```bash
make build-packc
./bin/packc --version
```

---

## Step 2: Create a Prompt Source File

Create `prompts/greeting.yaml`:

```yaml
name: "greeting-bot"
version: "1.0.0"
description: "Friendly greeting prompts"

prompts:
  - id: "simple-greeting"
    system: "You are a friendly assistant."
    template: "Greet the user warmly and offer assistance."
    
  - id: "professional-greeting"
    system: "You are a professional business assistant."
    template: "Provide a formal greeting and offer business support."

metadata:
  author: "Your Team"
  tags: ["greeting", "onboarding"]
```

---

## Step 3: Compile Your First Pack

```bash
./packc compile prompts/greeting.yaml --output greeting.pack.json
```

This generates a compiled `.pack.json` file optimized for production use.

---

## Step 4: Validate the Pack

```bash
./packc validate greeting.pack.json
```

PackC checks:

- ✅ Schema compliance
- ✅ Prompt completeness
- ✅ Template syntax
- ✅ Metadata consistency

---

## Step 5: Integrate into CI/CD

### GitHub Actions Example

Create `.github/workflows/build-packs.yml`:

```yaml
name: Build PromptPacks

on:
  push:
    paths:
      - 'prompts/**'
  pull_request:
    paths:
      - 'prompts/**'

jobs:
  build:
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      
      - name: Install PackC
        run: |
          go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Compile Packs
        run: |
          packc compile prompts/*.yaml --output-dir dist/packs/
      
      - name: Validate Packs
        run: |
          packc validate dist/packs/*.pack.json
      
      - name: Upload Artifacts
        uses: actions/upload-artifact@v3
        with:
          name: prompt-packs
          path: dist/packs/
```

---

## What's Next?

Now that you've compiled your first pack, explore more capabilities:

### 📚 **Tutorials** (Hands-on Learning)

- [First Compilation](/packc/tutorials/01-first-compilation/) - Detailed compilation walkthrough
- [CI/CD Pipeline](/packc/tutorials/02-ci-pipeline/) - Automated pack builds

### 🔧 **How-To Guides** (Specific Tasks)

- [Compile Packs](/packc/how-to/compile-packs/) - Compilation options
- [Validate Packs](/packc/how-to/validate-packs/) - Validation strategies
- [Optimize Packs](/packc/how-to/optimize-packs/) - Size and performance
- [Automate Builds](/packc/how-to/automate-builds/) - CI/CD integration patterns

### 💡 **Concepts** (Understanding)

- [Why Compile?](/packc/explanation/why-compile/) - Benefits of compilation
- [Pack Format](/packc/explanation/pack-format/) - Understanding .pack.json
- [Optimization](/packc/explanation/optimization/) - How optimization works

### 📖 **Reference** (Look Up Details)

- [CLI Commands](/packc/reference/cli-commands/) - Complete command reference
- [Pack Specification](/packc/reference/pack-specification/) - Schema definition
- [Compiler Options](/packc/reference/compiler-options/) - Configuration flags

---

## Common Use Cases for DevOps

### Version Management

```yaml
# prompts/greeting-v2.yaml
name: "greeting-bot"
version: "2.0.0"  # Semantic versioning
description: "Enhanced greeting prompts"
```

Compile with version tagging:

```bash
packc compile prompts/greeting-v2.yaml --version 2.0.0 --tag production
```

### Multi-Environment Builds

```bash
# Development
packc compile prompts/*.yaml --env dev --output dist/dev/

# Staging
packc compile prompts/*.yaml --env staging --output dist/staging/

# Production
packc compile prompts/*.yaml --env production --optimize --output dist/prod/
```

### Pack Distribution

```bash
# Generate checksum
sha256sum greeting.pack.json > greeting.pack.json.sha256

# Upload to artifact registry
aws s3 cp greeting.pack.json s3://my-packs/v1.0.0/
aws s3 cp greeting.pack.json.sha256 s3://my-packs/v1.0.0/
```

---

## Integration Patterns

### Makefile Integration

```makefile
.PHONY: build-packs
build-packs:
    packc compile prompts/*.yaml --output-dir dist/packs/

.PHONY: validate-packs
validate-packs:
    packc validate dist/packs/*.pack.json

.PHONY: clean-packs
clean-packs:
    rm -rf dist/packs/

.PHONY: release-packs
release-packs: build-packs validate-packs
    ./scripts/upload-packs.sh dist/packs/
```

### GitLab CI Example

```yaml
build-packs:
  stage: build
  image: golang:1.22
  script:
    - go install github.com/AltairaLabs/PromptKit/tools/packc@latest
    - packc compile prompts/*.yaml --output-dir packs/
    - packc validate packs/*.pack.json
  artifacts:
    paths:
      - packs/
    expire_in: 30 days
```

### Docker Multi-Stage Build

```dockerfile
FROM golang:1.22 AS builder
WORKDIR /build
RUN go install github.com/AltairaLabs/PromptKit/tools/packc@latest
COPY prompts/ ./prompts/
RUN packc compile prompts/*.yaml --output-dir /packs/

FROM alpine:latest
COPY --from=builder /packs/ /app/packs/
CMD ["ls", "/app/packs/"]
```

---

## Troubleshooting

### Compilation Errors

```bash
# Check syntax
packc validate --check-syntax prompts/greeting.yaml

# Verbose output
packc compile prompts/greeting.yaml --verbose
```

### Schema Validation Failures

```bash
# View detailed errors
packc validate greeting.pack.json --verbose

# Check against specific schema version
packc validate greeting.pack.json --schema-version 2.0
```

### CI/CD Build Failures

- Verify Go version matches requirements (1.22+)
- Check file paths are correct
- Ensure write permissions for output directory
- Validate YAML syntax before compilation

---

## Best Practices

### Versioning Strategy

- Use semantic versioning (MAJOR.MINOR.PATCH)
- Tag releases in Git matching pack versions
- Keep changelog of prompt changes
- Archive old pack versions

### Build Optimization

- Cache PackC binary in CI/CD
- Compile only changed prompts
- Use parallel compilation for large projects
- Enable optimization for production builds

### Quality Gates

```yaml
# CI quality checks
- packc compile --strict
- packc validate --fail-on-warning
- packc lint prompts/
- packc test prompts/ --coverage
```

---

## Production Checklist

Before deploying packs to production:

- ✅ All packs compiled successfully
- ✅ Validation passes with no errors
- ✅ Version numbers are correct
- ✅ Metadata is complete and accurate
- ✅ Packs are tested in staging environment
- ✅ Checksums generated and verified
- ✅ Artifact storage configured
- ✅ Rollback plan in place

---

## Join the Community

- **Questions**: [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- **Examples**: [PackC Examples](/packc/examples/)
- **Issues**: [Report a Bug](https://github.com/AltairaLabs/PromptKit/issues)

---

## Related Guides

- **For Prompt Engineers**: [Arena Getting Started](/getting-started/prompt-engineer/) - Test prompts before compiling
- **For Developers**: [SDK Getting Started](/getting-started/app-developer/) - Use compiled packs in applications
- **Complete Workflow**: [End-to-End Guide](/getting-started/complete-workflow/) - See all tools together

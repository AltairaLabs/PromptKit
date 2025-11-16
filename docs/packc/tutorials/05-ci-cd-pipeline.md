---
layout: default
title: "05: CI/CD Pipeline"
parent: Tutorials
grand_parent: PackC
nav_order: 5
---

# Tutorial 5: CI/CD Pipeline

Automate pack compilation, validation, and deployment in CI/CD.

## Learning Objectives

- Set up GitHub Actions for pack builds
- Automate validation in CI
- Deploy packs automatically
- Handle multi-environment deployments

## Time Required

**45 minutes**

## Prerequisites

- Completed [Tutorial 4: Pack Management](04-pack-management.md)
- GitHub account
- Git repository

## Step 1: Initialize Repository

```bash
mkdir cicd-packs
cd cicd-packs
git init
mkdir -p prompts config packs .github/workflows
```

## Step 2: Create Prompt and Config

```bash
cat > prompts/assistant.yaml <<'EOF'
apiVersion: promptkit/v1
kind: PromptConfig
spec:
  task_type: assistant
  name: AI Assistant
  system_prompt: You are a helpful AI assistant.
  user_template: "{% raw %}{{.message}}{% endraw %}"
  template_engine: go
  parameters:
    temperature: 0.7
    max_tokens: 500
EOF

cat > config/arena.yaml <<'EOF'
prompts:
  - ../prompts/assistant.yaml
EOF
```

## Step 3: Basic CI Workflow

Create GitHub Actions workflow:

```bash
cat > .github/workflows/build-packs.yml <<'EOF'
name: Build and Validate Packs

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    
    steps:
      - name: Checkout code
        uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Compile packs
        run: |
          mkdir -p packs
          packc compile \
            --config config/arena.yaml \
            --output packs/assistant.pack.json \
            --id assistant
      
      - name: Validate packs
        run: packc validate packs/assistant.pack.json
      
      - name: Upload packs
        uses: actions/upload-artifact@v3
        with:
          name: compiled-packs
          path: packs/*.pack.json
          retention-days: 30
EOF
```

## Step 4: Multi-Environment Workflow

```bash
cat > .github/workflows/multi-env.yml <<'EOF'
name: Multi-Environment Build

on:
  push:
    branches:
      - develop  # Dev environment
      - staging  # Staging environment
      - main     # Production environment

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v3
      
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Determine environment
        id: env
        run: |
          if [[ "${{ github.ref }}" == "refs/heads/main" ]]; then
            echo "environment=prod" >> $GITHUB_OUTPUT
          elif [[ "${{ github.ref }}" == "refs/heads/staging" ]]; then
            echo "environment=staging" >> $GITHUB_OUTPUT
          else
            echo "environment=dev" >> $GITHUB_OUTPUT
          fi
      
      - name: Compile pack
        run: |
          mkdir -p packs/${{ steps.env.outputs.environment }}
          packc compile \
            --config config/arena.yaml \
            --output packs/${{ steps.env.outputs.environment }}/assistant.pack.json \
            --id assistant-${{ steps.env.outputs.environment }}
      
      - name: Validate pack
        run: packc validate packs/${{ steps.env.outputs.environment }}/assistant.pack.json
      
      - name: Deploy to ${{ steps.env.outputs.environment }}
        run: |
          echo "Deploying to ${{ steps.env.outputs.environment }}"
          # Add your deployment command here
          # aws s3 cp packs/${{ steps.env.outputs.environment }}/assistant.pack.json s3://bucket/
EOF
```

## Step 5: Release Workflow

```bash
cat > .github/workflows/release.yml <<'EOF'
name: Release Packs

on:
  push:
    tags:
      - 'v*.*.*'

jobs:
  release:
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v3
      
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Get version
        id: version
        run: echo "VERSION=${GITHUB_REF#refs/tags/v}" >> $GITHUB_OUTPUT
      
      - name: Compile production pack
        run: |
          mkdir -p packs/prod
          packc compile \
            --config config/arena.yaml \
            --output packs/prod/assistant-v${{ steps.version.outputs.VERSION }}.pack.json \
            --id assistant
      
      - name: Validate pack
        run: packc validate packs/prod/assistant-v${{ steps.version.outputs.VERSION }}.pack.json
      
      - name: Create GitHub Release
        uses: softprops/action-gh-release@v1
        with:
          files: packs/prod/*.pack.json
          body: |
            ## Pack Release v${{ steps.version.outputs.VERSION }}
            
            Compiled production packs.
            
            ### Installation
            ```bash
            # Download pack
            curl -L https://github.com/${{ github.repository }}/releases/download/v${{ steps.version.outputs.VERSION }}/assistant-v${{ steps.version.outputs.VERSION }}.pack.json -o assistant.pack.json
            ```
EOF
```

## Step 6: Test Workflow

Commit and push:

```bash
git add .
git commit -m "Add CI/CD workflows"
git remote add origin https://github.com/yourusername/cicd-packs.git
git push -u origin main
```

Watch the workflow run on GitHub Actions.

## Step 7: Add Status Badges

Update README.md:

```bash
cat > README.md <<'EOF'
# CI/CD Packs

![Build Status](https://github.com/yourusername/cicd-packs/workflows/Build%20and%20Validate%20Packs/badge.svg)

Automated pack compilation and deployment.

## Workflows

- **Build and Validate** - Runs on every push/PR
- **Multi-Environment** - Deploys based on branch
- **Release** - Creates versioned releases on tags

## Usage

```bash
# Development
git push origin develop

# Staging
git push origin staging

# Production
git tag v1.0.0
git push origin v1.0.0
```
EOF
```

## Step 8: Local Testing

Test workflows locally with act:

```bash
# Install act
brew install act  # macOS
# or
curl https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash

# Test build workflow
act -j build

# Test with specific event
act push -j build
```

## What You Learned

- ✅ Set up GitHub Actions workflows
- ✅ Automated pack compilation
- ✅ Multi-environment deployments
- ✅ Release automation
- ✅ Status badges

## Best Practices

### 1. Cache Dependencies

```yaml
- name: Cache Go modules
  uses: actions/cache@v3
  with:
    path: ~/go/pkg/mod
    key: {% raw %}${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}{% endraw %}
```

### 2. Matrix Builds

```yaml
strategy:
  matrix:
    environment: [dev, staging, prod]
```

### 3. Required Status Checks

Configure branch protection to require passing builds before merge.

### 4. Automated Deployments

Only deploy from specific branches:

```yaml
if: github.ref == 'refs/heads/main'
```

## Advanced Patterns

### Parallel Builds

```yaml
jobs:
  build-dev:
    # ...
  build-staging:
    # ...
  build-prod:
    needs: [build-dev, build-staging]  # Sequential
```

### Conditional Steps

```yaml
- name: Deploy
  if: github.ref == 'refs/heads/main'
  run: ./deploy.sh
```

### Manual Approvals

```yaml
environment:
  name: production
  # Requires manual approval in GitHub
```

## Troubleshooting

### packc not found

```yaml
- name: Add to PATH
  run: echo "$(go env GOPATH)/bin" >> $GITHUB_PATH
```

### Permission denied

```yaml
- name: Set permissions
  run: chmod +x scripts/*.sh
```

### Validation fails

```yaml
- name: Show validation errors
  if: failure()
  run: packc validate packs/*.pack.json || true
```

## Next Steps

Congratulations! You've completed all PackC tutorials. Continue with:

- **[PackC Explanation](../explanation/)** - Deep dive into pack concepts
- **[SDK Integration](../../sdk/)** - Use packs in applications
- **[Arena Testing](../../arena/)** - Test prompts interactively

## Summary

You now have:
- Fully automated pack builds
- Multi-environment deployments
- Versioned releases
- Production-ready CI/CD

Excellent work completing all tutorials! 🎉🚀

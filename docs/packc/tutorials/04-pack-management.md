---
layout: default
title: "04: Pack Management"
parent: Tutorials
grand_parent: PackC
nav_order: 4
---

# Tutorial 4: Pack Management

Learn to organize, version, and manage packs effectively.

## Learning Objectives

- Organize packs by environment
- Version packs properly
- Manage pack lifecycle
- Deploy packs safely

## Time Required

**30 minutes**

## Prerequisites

- Completed [Tutorial 3: Validation Workflows](03-validation-workflow.md)

## Step 1: Create Project Structure

```bash
mkdir pack-management
cd pack-management
mkdir -p prompts config/{dev,staging,prod} packs/{dev,staging,prod}
```

## Step 2: Environment-Specific Configs

### Development Config

```bash
cat > config/dev/arena.yaml <<'EOF'
prompts:
  - ../../prompts/assistant.yaml

# Development settings
environment: dev
debug: true
EOF
```

### Staging Config

```bash
cat > config/staging/arena.yaml <<'EOF'
prompts:
  - ../../prompts/assistant.yaml

environment: staging
debug: false
EOF
```

### Production Config

```bash
cat > config/prod/arena.yaml <<'EOF'
prompts:
  - ../../prompts/assistant.yaml

environment: prod
debug: false
EOF
```

## Step 3: Create Prompt

```bash
cat > prompts/assistant.yaml <<'EOF'
apiVersion: promptkit/v1
kind: PromptConfig
spec:
  task_type: assistant
  name: AI Assistant
  description: Production assistant
  system_prompt: You are a helpful AI assistant.
  user_template: "{% raw %}{{.message}}{% endraw %}"
  template_engine: go
  parameters:
    temperature: 0.7
    max_tokens: 500
EOF
```

## Step 4: Build Script

```bash
cat > scripts/build-all.sh <<'EOF'
#!/bin/bash
set -e

VERSION="${1:-1.0.0}"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

echo "Building version: $VERSION"

for env in dev staging prod; do
  echo ""
  echo "=== Building $env ==="
  
  packc compile \
    --config "config/$env/arena.yaml" \
    --output "packs/$env/assistant-v${VERSION}.pack.json" \
    --id "assistant-$env"
  
  # Create latest symlink
  ln -sf "assistant-v${VERSION}.pack.json" "packs/$env/assistant-latest.pack.json"
  
  # Validate
  packc validate "packs/$env/assistant-v${VERSION}.pack.json"
  
  echo "✅ Built: packs/$env/assistant-v${VERSION}.pack.json"
done

echo ""
echo "✅ All environments built: v${VERSION}"
EOF

chmod +x scripts/build-all.sh
```

Run it:

```bash
./scripts/build-all.sh 1.0.0
```

## Step 5: Version Management

Create version tracking:

```bash
cat > VERSION <<'EOF'
1.0.0
EOF

cat > scripts/bump-version.sh <<'EOF'
#!/bin/bash

CURRENT=$(cat VERSION)
echo "Current version: $CURRENT"

# Parse version
IFS='.' read -r major minor patch <<< "$CURRENT"

# Determine bump type
case "${1:-patch}" in
  major)
    major=$((major + 1))
    minor=0
    patch=0
    ;;
  minor)
    minor=$((minor + 1))
    patch=0
    ;;
  patch)
    patch=$((patch + 1))
    ;;
esac

NEW_VERSION="$major.$minor.$patch"
echo "New version: $NEW_VERSION"

# Update VERSION file
echo "$NEW_VERSION" > VERSION

# Build with new version
./scripts/build-all.sh "$NEW_VERSION"

echo ""
echo "✅ Version bumped to $NEW_VERSION"
EOF

chmod +x scripts/bump-version.sh
```

Bump versions:

```bash
./scripts/bump-version.sh patch   # 1.0.0 -> 1.0.1
./scripts/bump-version.sh minor   # 1.0.1 -> 1.1.0
./scripts/bump-version.sh major   # 1.1.0 -> 2.0.0
```

## Step 6: Deployment Script

```bash
cat > scripts/deploy.sh <<'EOF'
#!/bin/bash
set -e

ENVIRONMENT="$1"
VERSION="$2"

if [ -z "$ENVIRONMENT" ] || [ -z "$VERSION" ]; then
  echo "Usage: $0 <environment> <version>"
  echo "Example: $0 prod 1.0.0"
  exit 1
fi

PACK_FILE="packs/$ENVIRONMENT/assistant-v${VERSION}.pack.json"

echo "=== Deploying to $ENVIRONMENT ==="

# 1. Verify pack exists
if [ ! -f "$PACK_FILE" ]; then
  echo "❌ Pack not found: $PACK_FILE"
  exit 1
fi

# 2. Validate pack
echo "Validating pack..."
packc validate "$PACK_FILE"

# 3. Backup current pack
if [ -f "/deployment/$ENVIRONMENT/assistant.pack.json" ]; then
  cp "/deployment/$ENVIRONMENT/assistant.pack.json" \
     "/deployment/$ENVIRONMENT/backups/assistant-$(date +%Y%m%d-%H%M%S).pack.json"
  echo "✅ Backed up current pack"
fi

# 4. Deploy new pack
mkdir -p "/deployment/$ENVIRONMENT"
cp "$PACK_FILE" "/deployment/$ENVIRONMENT/assistant.pack.json"

echo "✅ Deployed: $PACK_FILE to $ENVIRONMENT"
EOF

chmod +x scripts/deploy.sh
```

Deploy to staging:

```bash
./scripts/deploy.sh staging 1.0.0
```

Deploy to production:

```bash
./scripts/deploy.sh prod 1.0.0
```

## Step 7: Cleanup Old Versions

```bash
cat > scripts/cleanup-old-versions.sh <<'EOF'
#!/bin/bash

ENVIRONMENT="$1"
KEEP_VERSIONS="${2:-3}"

if [ -z "$ENVIRONMENT" ]; then
  echo "Usage: $0 <environment> [keep_versions]"
  exit 1
fi

cd "packs/$ENVIRONMENT"

echo "Keeping latest $KEEP_VERSIONS versions in $ENVIRONMENT"

# List versions, sort, keep latest N
ls -t assistant-v*.pack.json | tail -n +$((KEEP_VERSIONS + 1)) | while read file; do
  echo "Removing: $file"
  rm "$file"
done

echo "✅ Cleanup complete"
EOF

chmod +x scripts/cleanup-old-versions.sh
```

Clean up:

```bash
./scripts/cleanup-old-versions.sh dev 3
./scripts/cleanup-old-versions.sh staging 5
./scripts/cleanup-old-versions.sh prod 10
```

## Step 8: Pack Inventory

Track what's deployed:

```bash
cat > scripts/inventory.sh <<'EOF'
#!/bin/bash

echo "=== Pack Inventory ==="
echo ""

for env in dev staging prod; do
  echo "Environment: $env"
  echo "---"
  
  if [ -d "packs/$env" ]; then
    ls -lh "packs/$env/"*.pack.json | awk '{print "  " $9, "(" $5 ")"}'
    
    latest="packs/$env/assistant-latest.pack.json"
    if [ -L "$latest" ]; then
      target=$(readlink "$latest")
      echo "  Latest -> $target"
    fi
  fi
  
  echo ""
done
EOF

chmod +x scripts/inventory.sh
```

View inventory:

```bash
./scripts/inventory.sh
```

## What You Learned

- ✅ Organized packs by environment
- ✅ Implemented versioning strategy
- ✅ Created deployment workflows
- ✅ Managed pack lifecycle
- ✅ Automated cleanup

## Best Practices

### 1. Semantic Versioning

```
MAJOR.MINOR.PATCH
2.1.3

MAJOR: Breaking changes
MINOR: New features
PATCH: Bug fixes
```

### 2. Environment Isolation

```
✅ Separate configs per environment
✅ Test in dev → staging → prod
✅ Never skip staging
```

### 3. Always Backup

```bash
# Before deployment
cp current.pack.json backups/
cp new.pack.json current.pack.json
```

### 4. Track Deployments

```bash
# Log deployments
echo "$(date): Deployed v1.0.0 to prod" >> deployments.log
```

## Next Steps

- **[Tutorial 5: CI/CD Pipeline](05-ci-cd-pipeline.md)** - Automate everything
- **[How-To: Organize Packs](../how-to/organize-packs.md)** - More organization strategies
- **[Explanation: Pack Format](../explanation/pack-format.md)** - Understand pack structure

## Summary

You now can:
- Manage packs across environments
- Version packs properly
- Deploy safely with backups
- Track pack inventory
- Clean up old versions

Excellent progress! 🎉

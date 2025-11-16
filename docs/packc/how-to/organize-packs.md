---
layout: default
title: Organize Pack Files
parent: How-To Guides
grand_parent: PackC
nav_order: 4
---

# How to Organize Pack Files

Structure and manage your pack files for maintainability and scalability.

## Goal

Create a clear, maintainable organization system for your pack files that scales with your project.

## Prerequisites

- packc installed
- Understanding of your project structure
- Multiple pack files or environments

## Organization Strategies

### Strategy 1: By Environment

Separate packs by deployment environment:

```
packs/
├── dev/
│   ├── app.pack.json
│   └── admin.pack.json
├── staging/
│   ├── app.pack.json
│   └── admin.pack.json
└── prod/
    ├── app.pack.json
    └── admin.pack.json
```

**Setup:**

```bash
# Create structure
mkdir -p packs/{dev,staging,prod}

# Compile for each environment
packc compile --config config/arena.dev.yaml \
  --output packs/dev/app.pack.json --id app-dev

packc compile --config config/arena.staging.yaml \
  --output packs/staging/app.pack.json --id app-staging

packc compile --config config/arena.prod.yaml \
  --output packs/prod/app.pack.json --id app-prod
```

### Strategy 2: By Feature

Organize by application feature or module:

```
packs/
├── customer-support/
│   ├── chat.pack.json
│   └── email.pack.json
├── sales/
│   ├── leads.pack.json
│   └── opportunities.pack.json
└── marketing/
    ├── campaigns.pack.json
    └── analytics.pack.json
```

**Setup:**

```bash
# Create feature directories
mkdir -p packs/{customer-support,sales,marketing}

# Compile feature packs
packc compile --config config/customer-support/arena.yaml \
  --output packs/customer-support/chat.pack.json --id cs-chat

packc compile --config config/sales/arena.yaml \
  --output packs/sales/leads.pack.json --id sales-leads
```

### Strategy 3: By Version

Version control your packs:

```
packs/
├── v1.0.0/
│   └── app.pack.json
├── v1.1.0/
│   └── app.pack.json
├── v1.2.0/
│   └── app.pack.json
└── latest -> v1.2.0/
```

**Setup:**

```bash
VERSION="1.2.0"

# Create version directory
mkdir -p "packs/v${VERSION}"

# Compile to versioned location
packc compile --config arena.yaml \
  --output "packs/v${VERSION}/app.pack.json" --id "app-v${VERSION}"

# Update latest symlink
ln -sf "v${VERSION}" packs/latest
```

### Strategy 4: Hybrid Organization

Combine strategies for complex projects:

```
packs/
├── dev/
│   ├── customer-support/
│   │   └── v1.0.0/
│   │       └── app.pack.json
│   └── sales/
│       └── v1.0.0/
│           └── app.pack.json
├── staging/
│   ├── customer-support/
│   │   └── v1.0.0/
│   │       └── app.pack.json
│   └── sales/
│       └── v1.0.0/
│           └── app.pack.json
└── prod/
    ├── customer-support/
    │   └── v1.0.0/
    │       └── app.pack.json
    └── sales/
        └── v1.0.0/
            └── app.pack.json
```

## Naming Conventions

### Pack File Names

Use descriptive, consistent names:

```bash
# Good naming
customer-support.pack.json
sales-leads-v1.pack.json
marketing-campaigns-prod.pack.json

# Avoid
pack1.json
temp.pack.json
test.json
```

### Pack IDs

Match IDs to purpose and environment:

```bash
# Development
--id customer-support-dev

# Staging
--id customer-support-staging

# Production
--id customer-support-prod

# Versioned
--id customer-support-v1.2.0
```

## Build Scripts

### Environment-Based Build

```bash
#!/bin/bash
# scripts/build-packs.sh

set -e

ENVIRONMENTS=("dev" "staging" "prod")

for env in "${ENVIRONMENTS[@]}"; do
  echo "Building packs for $env..."
  
  mkdir -p "packs/$env"
  
  packc compile \
    --config "config/arena.$env.yaml" \
    --output "packs/$env/app.pack.json" \
    --id "app-$env"
  
  packc validate "packs/$env/app.pack.json"
done

echo "✓ All packs built successfully"
```

### Feature-Based Build

```bash
#!/bin/bash
# scripts/build-features.sh

set -e

FEATURES=("customer-support" "sales" "marketing")

for feature in "${FEATURES[@]}"; do
  echo "Building $feature packs..."
  
  mkdir -p "packs/$feature"
  
  for config in "config/$feature"/*.yaml; do
    name=$(basename "$config" .yaml)
    
    packc compile \
      --config "$config" \
      --output "packs/$feature/$name.pack.json" \
      --id "$feature-$name"
  done
done

echo "✓ All feature packs built"
```

### Versioned Build

```bash
#!/bin/bash
# scripts/build-versioned.sh

set -e

VERSION="${1:-$(date +%Y%m%d)}"

echo "Building version: $VERSION"

mkdir -p "packs/v$VERSION"

packc compile \
  --config arena.yaml \
  --output "packs/v$VERSION/app.pack.json" \
  --id "app-v$VERSION"

# Update latest
ln -sf "v$VERSION" packs/latest

echo "✓ Version $VERSION built"
echo "Latest points to: $(readlink packs/latest)"
```

## Makefile Organization

```makefile
# Variables
VERSION ?= $(shell date +%Y%m%d)
ENVIRONMENTS := dev staging prod
FEATURES := customer-support sales marketing

.PHONY: build-all
build-all: build-by-env build-by-feature build-versioned

# Build by environment
.PHONY: build-by-env
build-by-env:
	@for env in $(ENVIRONMENTS); do \
		mkdir -p packs/$$env; \
		packc compile --config config/arena.$$env.yaml \
		  --output packs/$$env/app.pack.json --id app-$$env; \
	done

# Build by feature
.PHONY: build-by-feature
build-by-feature:
	@for feature in $(FEATURES); do \
		mkdir -p packs/$$feature; \
		packc compile --config config/$$feature/arena.yaml \
		  --output packs/$$feature/app.pack.json --id $$feature-app; \
	done

# Build versioned
.PHONY: build-versioned
build-versioned:
	@mkdir -p packs/v$(VERSION)
	@packc compile --config arena.yaml \
	  --output packs/v$(VERSION)/app.pack.json --id app-v$(VERSION)
	@ln -sf v$(VERSION) packs/latest

# Clean
.PHONY: clean
clean:
	rm -rf packs/

.PHONY: clean-env
clean-env:
	rm -rf packs/$(ENV)/

# Validate
.PHONY: validate-all
validate-all:
	@find packs -name '*.pack.json' -exec packc validate {} \;
```

## Configuration Organization

Mirror your pack organization in configs:

```
config/
├── dev/
│   └── arena.yaml
├── staging/
│   └── arena.yaml
├── prod/
│   └── arena.yaml
└── features/
    ├── customer-support/
    │   └── arena.yaml
    ├── sales/
    │   └── arena.yaml
    └── marketing/
        └── arena.yaml
```

## Metadata Files

Track pack information:

```bash
# Create pack inventory
cat > packs/inventory.json <<EOF
{
  "environments": {
    "dev": {
      "packs": ["app.pack.json"],
      "last_build": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    },
    "prod": {
      "packs": ["app.pack.json"],
      "last_build": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    }
  }
}
EOF
```

## Git Configuration

### .gitignore

```gitignore
# Ignore compiled packs in development
packs/dev/*.pack.json
packs/staging/*.pack.json

# Keep production packs in version control
# (Comment out the line below to track prod packs)
packs/prod/*.pack.json

# Ignore temporary builds
packs/tmp/
packs/*.tmp.json

# Keep directory structure
!packs/dev/.gitkeep
!packs/staging/.gitkeep
!packs/prod/.gitkeep
```

### Track Pack Metadata

```bash
# Create .gitkeep files
touch packs/{dev,staging,prod}/.gitkeep

# Create README
cat > packs/README.md <<EOF
# Prompt Packs

This directory contains compiled prompt packs organized by environment.

## Structure

- \`dev/\` - Development packs
- \`staging/\` - Staging packs  
- \`prod/\` - Production packs

## Building

Run \`make build-all\` to compile all packs.

See [build instructions](../docs/packc/how-to/compile-packs.md) for details.
EOF

git add packs/.gitkeep packs/README.md
```

## Best Practices

### 1. Consistent Directory Structure

```
project/
├── config/          # Source configurations
│   ├── dev/
│   ├── staging/
│   └── prod/
├── packs/           # Compiled packs
│   ├── dev/
│   ├── staging/
│   └── prod/
└── prompts/         # Source prompts
    ├── customer-support/
    ├── sales/
    └── marketing/
```

### 2. Document Organization

Create a pack organization guide:

```markdown
# Pack Organization Guide

## Directory Structure

- `packs/dev/` - Development builds
- `packs/staging/` - QA/testing builds
- `packs/prod/` - Production releases

## Naming Conventions

- Use kebab-case: `customer-support.pack.json`
- Include version: `app-v1.2.0.pack.json`
- Add environment suffix: `app-prod.pack.json`

## Build Process

1. Update prompt configurations
2. Run `make build-by-env`
3. Validate with `make validate-all`
4. Commit to version control
```

### 3. Automate Organization

```bash
#!/bin/bash
# scripts/organize-packs.sh

# Create structure
mkdir -p packs/{dev,staging,prod}/{customer-support,sales,marketing}

# Build into organized structure
for env in dev staging prod; do
  for feature in customer-support sales marketing; do
    packc compile \
      --config "config/$env/$feature/arena.yaml" \
      --output "packs/$env/$feature/app.pack.json" \
      --id "$feature-$env"
  done
done
```

### 4. Version Control Strategy

Choose what to track:

**Option A: Track all packs**
```gitignore
# Track everything
!packs/**/*.pack.json
```

**Option B: Track only production**
```gitignore
# Ignore dev/staging
packs/dev/
packs/staging/

# Keep production
!packs/prod/**/*.pack.json
```

**Option C: Track none (build in CI)**
```gitignore
# Ignore all packs
packs/**/*.pack.json

# Keep structure
!packs/**/.gitkeep
```

### 5. Cleanup Old Versions

```bash
#!/bin/bash
# scripts/cleanup-old-packs.sh

KEEP_VERSIONS=3

cd packs

# Remove old versioned packs, keep latest N
ls -dt v* | tail -n +$((KEEP_VERSIONS + 1)) | xargs rm -rf

echo "Kept latest $KEEP_VERSIONS versions"
```

## Next Steps

- [CI/CD Integration](ci-cd-integration.md) - Automate pack builds
- [Validate Packs](validate-packs.md) - Quality assurance
- [Pack Management Tutorial](../tutorials/04-pack-management.md)

## See Also

- [Project structure guide](../../guides/project-structure.md)
- [Version control best practices](../../guides/version-control.md)
- [Pack format explanation](../explanation/pack-format.md)

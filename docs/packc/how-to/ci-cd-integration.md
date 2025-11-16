---
layout: default
title: CI/CD Integration
parent: How-To Guides
grand_parent: PackC
nav_order: 5
---

# How to Integrate PackC with CI/CD

Automate pack compilation, validation, and deployment in your CI/CD pipeline.

## Goal

Set up automated pack builds that compile, validate, and deploy packs on every code change.

## Prerequisites

- packc installed locally
- CI/CD platform (GitHub Actions, GitLab CI, Jenkins, etc.)
- Version control repository
- Basic CI/CD knowledge

## GitHub Actions

### Basic Pack Build

```yaml
# .github/workflows/build-packs.yml
name: Build Packs

on:
  push:
    branches: [main, develop]
    paths:
      - 'prompts/**'
      - 'config/**'
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
          packc compile --config arena.yaml --output packs/app.pack.json --id app
      
      - name: Validate packs
        run: |
          packc validate packs/app.pack.json
      
      - name: Upload packs
        uses: actions/upload-artifact@v3
        with:
          name: compiled-packs
          path: packs/*.pack.json
```

### Multi-Environment Build

```yaml
# .github/workflows/build-multi-env.yml
name: Build Multi-Environment Packs

on: [push, pull_request]

jobs:
  build:
    strategy:
      matrix:
        environment: [dev, staging, prod]
    
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v3
      
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Compile ${{ matrix.environment }} pack
        run: |
          mkdir -p packs/${{ matrix.environment }}
          packc compile \
            --config config/arena.${{ matrix.environment }}.yaml \
            --output packs/${{ matrix.environment }}/app.pack.json \
            --id app-${{ matrix.environment }}
      
      - name: Validate pack
        run: packc validate packs/${{ matrix.environment }}/app.pack.json
      
      - name: Upload pack
        uses: actions/upload-artifact@v3
        with:
          name: pack-${{ matrix.environment }}
          path: packs/${{ matrix.environment }}/*.pack.json
```

### Production Release

```yaml
# .github/workflows/release-packs.yml
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
            --config config/arena.prod.yaml \
            --output packs/prod/app-v${{ steps.version.outputs.VERSION }}.pack.json \
            --id app-v${{ steps.version.outputs.VERSION }}
      
      - name: Validate pack
        run: packc validate packs/prod/app-v${{ steps.version.outputs.VERSION }}.pack.json
      
      - name: Create release
        uses: softprops/action-gh-release@v1
        with:
          files: packs/prod/*.pack.json
          body: |
            ## Prompt Pack Release v${{ steps.version.outputs.VERSION }}
            
            Compiled packs for production deployment.
            
            Compiler: $(packc version)
```

## GitLab CI

### Basic Pipeline

```yaml
# .gitlab-ci.yml
stages:
  - build
  - validate
  - deploy

variables:
  PACK_ID: app
  GO_VERSION: "1.22"

before_script:
  - apt-get update && apt-get install -y golang-$GO_VERSION
  - go install github.com/AltairaLabs/PromptKit/tools/packc@latest
  - export PATH=$PATH:$(go env GOPATH)/bin

build:packs:
  stage: build
  script:
    - mkdir -p packs
    - packc compile --config arena.yaml --output packs/app.pack.json --id $PACK_ID
  artifacts:
    paths:
      - packs/*.pack.json
    expire_in: 1 day

validate:packs:
  stage: validate
  dependencies:
    - build:packs
  script:
    - packc validate packs/app.pack.json

deploy:production:
  stage: deploy
  dependencies:
    - build:packs
  only:
    - main
  script:
    - ./scripts/deploy-pack.sh packs/app.pack.json
  environment:
    name: production
```

### Multi-Environment Pipeline

```yaml
# .gitlab-ci.yml
.build_template:
  stage: build
  script:
    - mkdir -p packs/$ENVIRONMENT
    - packc compile --config config/arena.$ENVIRONMENT.yaml --output packs/$ENVIRONMENT/app.pack.json --id app-$ENVIRONMENT
    - packc validate packs/$ENVIRONMENT/app.pack.json
  artifacts:
    paths:
      - packs/$ENVIRONMENT/*.pack.json

build:dev:
  extends: .build_template
  variables:
    ENVIRONMENT: dev
  only:
    - develop

build:staging:
  extends: .build_template
  variables:
    ENVIRONMENT: staging
  only:
    - staging

build:prod:
  extends: .build_template
  variables:
    ENVIRONMENT: prod
  only:
    - main
```

## Jenkins

### Declarative Pipeline

```groovy
// Jenkinsfile
pipeline {
    agent any
    
    environment {
        PACK_ID = 'app'
        GO_VERSION = '1.22'
    }
    
    stages {
        stage('Setup') {
            steps {
                sh 'go version'
                sh 'go install github.com/AltairaLabs/PromptKit/tools/packc@latest'
                sh 'packc version'
            }
        }
        
        stage('Compile') {
            steps {
                sh 'mkdir -p packs'
                sh "packc compile --config arena.yaml --output packs/app.pack.json --id ${PACK_ID}"
            }
        }
        
        stage('Validate') {
            steps {
                sh 'packc validate packs/app.pack.json'
            }
        }
        
        stage('Archive') {
            steps {
                archiveArtifacts artifacts: 'packs/*.pack.json', fingerprint: true
            }
        }
        
        stage('Deploy') {
            when {
                branch 'main'
            }
            steps {
                sh './scripts/deploy-pack.sh packs/app.pack.json'
            }
        }
    }
    
    post {
        success {
            echo 'Pack build successful!'
        }
        failure {
            echo 'Pack build failed!'
        }
    }
}
```

### Multi-Branch Pipeline

```groovy
// Jenkinsfile
pipeline {
    agent any
    
    stages {
        stage('Compile') {
            steps {
                script {
                    def environment = env.BRANCH_NAME == 'main' ? 'prod' : env.BRANCH_NAME
                    
                    sh "mkdir -p packs/${environment}"
                    sh "packc compile --config config/arena.${environment}.yaml --output packs/${environment}/app.pack.json --id app-${environment}"
                    sh "packc validate packs/${environment}/app.pack.json"
                }
            }
        }
    }
}
```

## CircleCI

```yaml
# .circleci/config.yml
version: 2.1

orbs:
  go: circleci/go@1.9

jobs:
  build-packs:
    docker:
      - image: cimg/go:1.22
    steps:
      - checkout
      
      - run:
          name: Install packc
          command: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - run:
          name: Compile packs
          command: |
            mkdir -p packs
            packc compile --config arena.yaml --output packs/app.pack.json --id app
      
      - run:
          name: Validate packs
          command: packc validate packs/app.pack.json
      
      - store_artifacts:
          path: packs
          destination: compiled-packs
      
      - persist_to_workspace:
          root: .
          paths:
            - packs/*.pack.json

  deploy-packs:
    docker:
      - image: cimg/base:stable
    steps:
      - attach_workspace:
          at: .
      
      - run:
          name: Deploy packs
          command: ./scripts/deploy-pack.sh packs/app.pack.json

workflows:
  build-and-deploy:
    jobs:
      - build-packs
      - deploy-packs:
          requires:
            - build-packs
          filters:
            branches:
              only: main
```

## Docker Integration

### Build in Docker

```dockerfile
# Dockerfile.packc-build
FROM golang:1.22 AS builder

# Install packc
RUN go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Copy source files
WORKDIR /workspace
COPY . .

# Compile packs
RUN mkdir -p packs && \
    packc compile --config arena.yaml --output packs/app.pack.json --id app && \
    packc validate packs/app.pack.json

# Runtime image
FROM alpine:latest
COPY --from=builder /workspace/packs /packs

CMD ["ls", "-la", "/packs"]
```

Build and extract packs:

```bash
# Build image
docker build -f Dockerfile.packc-build -t packc-builder .

# Extract packs
docker run --rm -v $(pwd)/output:/output packc-builder cp -r /packs/. /output/
```

### Use in CI

```yaml
# .github/workflows/docker-build.yml
- name: Build packs with Docker
  run: |
    docker build -f Dockerfile.packc-build -t packc-builder .
    docker run --rm -v $(pwd)/packs:/output packc-builder cp -r /packs/. /output/
```

## Makefile CI Integration

```makefile
# Makefile for CI/CD

.PHONY: ci-build
ci-build: clean install compile validate

.PHONY: install
install:
	@echo "Installing packc..."
	@go install github.com/AltairaLabs/PromptKit/tools/packc@latest
	@packc version

.PHONY: compile
compile:
	@echo "Compiling packs..."
	@mkdir -p packs
	@packc compile --config arena.yaml --output packs/app.pack.json --id app

.PHONY: validate
validate:
	@echo "Validating packs..."
	@packc validate packs/app.pack.json

.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf packs/

.PHONY: deploy
deploy: ci-build
	@echo "Deploying packs..."
	@./scripts/deploy-pack.sh packs/app.pack.json
```

CI usage:

```yaml
# GitHub Actions
- name: Build and deploy
  run: make deploy

# GitLab CI
script:
  - make ci-build
```

## Best Practices

### 1. Cache Dependencies

**GitHub Actions:**

```yaml
- name: Cache Go modules
  uses: actions/cache@v3
  with:
    path: ~/go/pkg/mod
    key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
```

**GitLab CI:**

```yaml
cache:
  paths:
    - .go/pkg/mod
```

### 2. Version Pinning

Pin packc version for reproducible builds:

```yaml
- name: Install packc
  run: go install github.com/AltairaLabs/PromptKit/tools/packc@v0.1.0
```

### 3. Fail Fast

Stop pipeline on validation failure:

```yaml
- name: Validate packs
  run: |
    for pack in packs/*.pack.json; do
      packc validate "$pack" || exit 1
    done
```

### 4. Artifact Management

Store and version pack artifacts:

```yaml
- name: Upload with version
  uses: actions/upload-artifact@v3
  with:
    name: packs-${{ github.sha }}
    path: packs/*.pack.json
    retention-days: 30
```

### 5. Notifications

Send build notifications:

```yaml
- name: Notify on success
  if: success()
  run: ./scripts/notify-success.sh

- name: Notify on failure
  if: failure()
  run: ./scripts/notify-failure.sh
```

## Troubleshooting

### packc not found in CI

**Problem:** Command not found after install

**Solution:** Add to PATH:

```yaml
- name: Add to PATH
  run: echo "$(go env GOPATH)/bin" >> $GITHUB_PATH
```

### Compilation fails in CI

**Problem:** Files not found

**Solution:** Check working directory:

```yaml
- name: List files
  run: |
    pwd
    ls -la
    ls -la config/
```

### Permission denied

**Problem:** Can't create directories

**Solution:** Ensure write permissions:

```yaml
- name: Create directories
  run: mkdir -p packs && chmod -R 755 packs
```

## Next Steps

- [Organize Pack Files](organize-packs.md) - Structure your packs
- [Deployment Guide](../../guides/deployment.md) - Deploy packs to production
- [Monitor Builds](../../guides/monitoring.md) - Track build health

## See Also

- [CI/CD best practices](../../guides/ci-cd-best-practices.md)
- [Deployment strategies](../../guides/deployment-strategies.md)
- [Pack versioning](../explanation/versioning.md)

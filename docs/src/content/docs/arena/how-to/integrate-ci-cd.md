---
title: Integrate with CI/CD
sidebar:
  order: 6
---
Learn how to integrate PromptArena testing into continuous integration and deployment pipelines.

## Overview

Automate LLM testing in CI/CD pipelines to catch regressions, validate quality gates, and ensure consistent performance across deployments.

## GitHub Actions

### Basic Workflow

```yaml
# .github/workflows/arena-tests.yml
name: LLM Tests

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    
    steps:
      - name: Checkout code
        uses: actions/checkout@v4
      
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      
      - name: Install PromptArena
        run: |
          go install github.com/altairalabs/promptkit/tools/arena@latest
      
      - name: Run LLM tests
        env:
          OPENAI_API_KEY: $
          ANTHROPIC_API_KEY: $
        run: |
          promptarena run --ci --format junit,json,html
      
      - name: Publish Test Results
        uses: dorny/test-reporter@v1
        if: always()
        with:
          name: LLM Test Results
          path: out/junit.xml
          reporter: java-junit
      
      - name: Upload Test Reports
        uses: actions/upload-artifact@v4
        if: always()
        with:
          name: test-reports
          path: |
            out/junit.xml
            out/*.json
            out/*.html
```

### Multi-stage Testing

```yaml
# .github/workflows/arena-tests.yml
name: LLM Tests

on: [push, pull_request]

jobs:
  # Fast validation with mocks
  mock-validation:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      
      - name: Install Arena
        run: go install github.com/altairalabs/promptkit/tools/arena@latest
      
      - name: Validate with Mocks
        run: |
          promptarena run --mock-provider --ci --format junit
      
      - name: Publish Mock Results
        uses: dorny/test-reporter@v1
        if: always()
        with:
          name: Mock Validation
          path: out/junit.xml
          reporter: java-junit
  
  # Comprehensive tests with real providers
  provider-tests:
    runs-on: ubuntu-latest
    needs: mock-validation
    if: github.event_name == 'push'
    
    strategy:
      matrix:
        provider: [openai, claude, gemini]
    
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      
      - name: Install Arena
        run: go install github.com/altairalabs/promptkit/tools/arena@latest
      
      - name: Test $
        env:
          OPENAI_API_KEY: $
          ANTHROPIC_API_KEY: $
          GOOGLE_API_KEY: $
        run: |
          promptarena run \
            --provider $ \
            --ci \
            --format junit,json \
            --out out/$
      
      - name: Publish Results
        uses: dorny/test-reporter@v1
        if: always()
        with:
          name: $ Tests
          path: out/$/junit.xml
          reporter: java-junit
```

### Quality Gates

```yaml
# .github/workflows/arena-tests.yml
jobs:
  test-with-gates:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      
      - name: Install Arena
        run: go install github.com/altairalabs/promptkit/tools/arena@latest
      
      - name: Run tests
        id: arena-test
        env:
          OPENAI_API_KEY: $
        run: |
          promptarena run --ci --format json,junit
          
          # Check results
          PASS_RATE=$(jq '.summary.pass_rate' out/results.json)
          echo "pass_rate=$PASS_RATE" >> $GITHUB_OUTPUT
      
      - name: Quality Gate
        if: steps.arena-test.outputs.pass_rate < 0.95
        run: |
          echo "::error::Quality gate failed: Pass rate $ < 95%"
          exit 1
      
      - name: Comment PR
        if: github.event_name == 'pull_request'
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const results = JSON.parse(fs.readFileSync('out/results.json', 'utf8'));
            
            const comment = `
            ## LLM Test Results
            
            - **Pass Rate**: ${results.summary.pass_rate * 100}%
            - **Total Tests**: ${results.summary.total}
            - **Passed**: ${results.summary.passed}
            - **Failed**: ${results.summary.failed}
            
            ${results.summary.pass_rate >= 0.95 ? '✅ Quality gate passed' : '❌ Quality gate failed'}
            `;
            
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: comment
            });
```

## GitLab CI

### Basic Pipeline

```yaml
# .gitlab-ci.yml
stages:
  - test
  - report

variables:
  GO_VERSION: "1.23"

arena-tests:
  stage: test
  image: golang:${GO_VERSION}
  
  before_script:
    - go install github.com/altairalabs/promptkit/tools/arena@latest
  
  script:
    - promptarena run --ci --format junit,json
  
  artifacts:
    when: always
    reports:
      junit: out/junit.xml
    paths:
      - out/
  
  variables:
    OPENAI_API_KEY: $OPENAI_API_KEY
    ANTHROPIC_API_KEY: $ANTHROPIC_API_KEY
```

### Multi-environment Testing

```yaml
# .gitlab-ci.yml
.arena-test-template: &arena-test
  stage: test
  image: golang:1.23
  before_script:
    - go install github.com/altairalabs/promptkit/tools/arena@latest
  script:
    - promptarena run --ci --format junit,json --out out/${CI_JOB_NAME}
  artifacts:
    when: always
    reports:
      junit: out/${CI_JOB_NAME}/junit.xml
    paths:
      - out/

test-dev:
  <<: *arena-test
  variables:
    OPENAI_API_KEY: $DEV_OPENAI_API_KEY
  only:
    - develop

test-staging:
  <<: *arena-test
  variables:
    OPENAI_API_KEY: $STAGING_OPENAI_API_KEY
  only:
    - staging

test-prod:
  <<: *arena-test
  variables:
    OPENAI_API_KEY: $PROD_OPENAI_API_KEY
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
        OPENAI_API_KEY = credentials('openai-api-key')
        ANTHROPIC_API_KEY = credentials('anthropic-api-key')
    }
    
    stages {
        stage('Setup') {
            steps {
                sh 'go version'
                sh 'go install github.com/altairalabs/promptkit/tools/arena@latest'
            }
        }
        
        stage('Mock Validation') {
            steps {
                sh 'promptarena run --mock-provider --ci --format junit'
            }
        }
        
        stage('Provider Tests') {
            parallel {
                stage('OpenAI') {
                    steps {
                        sh '''
                            promptarena run \
                              --provider openai \
                              --ci \
                              --format junit \
                              --out out/openai
                        '''
                    }
                }
                
                stage('Claude') {
                    steps {
                        sh '''
                            promptarena run \
                              --provider claude \
                              --ci \
                              --format junit \
                              --out out/claude
                        '''
                    }
                }
            }
        }
    }
    
    post {
        always {
            junit 'out/**/junit.xml'
            archiveArtifacts artifacts: 'out/**/*', allowEmptyArchive: true
        }
        
        failure {
            emailext(
                subject: "LLM Tests Failed: ${env.JOB_NAME} - ${env.BUILD_NUMBER}",
                body: "Check console output at ${env.BUILD_URL}",
                to: "team@example.com"
            )
        }
    }
}
```

## CircleCI

```yaml
# .circleci/config.yml
version: 2.1

jobs:
  arena-test:
    docker:
      - image: cimg/go:1.23
    
    steps:
      - checkout
      
      - run:
          name: Install PromptArena
          command: go install github.com/altairalabs/promptkit/tools/arena@latest
      
      - run:
          name: Run tests
          command: |
            promptarena run --ci --format junit,json
      
      - store_test_results:
          path: out/junit.xml
      
      - store_artifacts:
          path: out/

workflows:
  version: 2
  test:
    jobs:
      - arena-test:
          context: llm-api-keys
```

## Best Practices

### 1. Use CI Mode

```bash
# Optimized for CI: headless, minimal output, fast failure
promptarena run --ci --format junit,json
```

### 2. Manage API Keys Securely

```yaml
# GitHub Actions
env:
  OPENAI_API_KEY: $

# GitLab CI (masked variables)
variables:
  OPENAI_API_KEY: $OPENAI_API_KEY

# Jenkins (credentials plugin)
environment {
  OPENAI_API_KEY = credentials('openai-api-key')
}
```

### 3. Fast Feedback with Mocks

```yaml
jobs:
  quick-check:
    # Fast validation (< 30s)
    steps:
      - run: promptarena run --mock-provider --ci
  
  full-test:
    needs: quick-check  # Only run if quick check passes
    steps:
      - run: promptarena run --ci
```

### 4. Control Concurrency

```bash
# Respect rate limits in CI
promptarena run --concurrency 2 --ci
```

### 5. Selective Testing

```bash
# Run critical tests on every PR
promptarena run --scenario critical --ci

# Full suite on main branch only
if [ "$BRANCH" = "main" ]; then
  promptarena run --ci
fi
```

### 6. Archive Results

```yaml
# GitHub Actions
- uses: actions/upload-artifact@v4
  if: always()
  with:
    name: arena-results-$
    path: out/
    retention-days: 30
```

### 7. Trend Analysis

Track metrics over time:

```bash
# Store results with metadata
promptarena run --ci --format json

# Parse and track
jq '.summary' out/results.json > metrics/run-${BUILD_ID}.json
```

### 8. Failure Notifications

```yaml
# Slack notification on failure
- name: Notify Slack
  if: failure()
  uses: slackapi/slack-github-action@v1
  with:
    payload: |
      {
        "text": "LLM tests failed in $",
        "blocks": [
          {
            "type": "section",
            "text": {
              "type": "mrkdwn",
              "text": "*LLM Test Failure*\nBranch: $\nRun: $/$/actions/runs/$"
            }
          }
        ]
      }
  env:
    SLACK_WEBHOOK_URL: $
```

## Troubleshooting

### Timeout Issues

```bash
# Increase timeout for CI environments
promptarena run --ci --timeout 600  # 10 minutes
```

### Rate Limiting

```bash
# Reduce concurrency
promptarena run --ci --concurrency 1

# Or use mock providers for structure validation
promptarena run --mock-provider --ci
```

### Flaky Tests

```yaml
# Retry failed tests
- name: Run tests with retry
  uses: nick-fields/retry@v2
  with:
    timeout_minutes: 10
    max_attempts: 3
    command: promptarena run --ci --format junit
```

## Next Steps

- **[Output Formats](../reference/output-formats)** - CI-friendly output formats
- **[CLI Reference](../reference/cli-commands)** - Complete command options
- **[Tutorial: CI Integration](../tutorials/05-ci-integration)** - Step-by-step CI setup

## Examples

See CI configurations:
- `.github/workflows/` - GitHub Actions examples
- `examples/ci-integration/` - Multi-platform CI configs

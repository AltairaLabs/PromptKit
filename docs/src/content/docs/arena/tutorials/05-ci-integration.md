---
title: 'Tutorial 5: CI/CD Integration'
sidebar:
  order: 5
---
Learn how to integrate PromptArena testing into your CI/CD pipeline for automated quality gates.

## What You'll Learn

- Set up Arena in GitHub Actions
- Configure quality gates
- Generate CI-friendly reports
- Handle API keys securely
- Optimize test execution for CI
- Create deployment gates

## Prerequisites

- Completed [Tutorials 1-4](01-first-test)
- GitHub repository
- Basic CI/CD knowledge

## Why Automate LLM Testing?

Manual testing doesn't scale. Automated testing in CI/CD:
- **Catches regressions** before deployment
- **Enforces quality standards** automatically
- **Validates changes** on every commit
- **Provides confidence** for releases
- **Documents behavior** over time

## Step 1: Prepare Your Tests

Organize tests for CI execution:

```bash
my-project/
├── .github/
│   └── workflows/
│       └── llm-tests.yml
├── tests/
│   ├── arena.yaml
│   ├── prompts/
│   ├── providers/
│   └── scenarios/
│       ├── critical/       # Must-pass tests
│       │   └── core.yaml
│       ├── integration/    # Full suite
│       │   └── full.yaml
│       └── smoke/          # Quick validation
│           └── basic.yaml
```

## Step 2: Choose Installation Method

For CI/CD, we recommend using npm for PromptArena installation:

**Why npm for CI?**
- ✅ No Go toolchain required (faster setup)
- ✅ Works on all platforms (Linux, macOS, Windows)
- ✅ Simpler workflow (one command)
- ✅ Node.js typically already in CI environments
- ✅ Faster installation time

**Alternative: Go Install** (if you prefer):
```yaml
- name: Set up Go
  uses: actions/setup-go@v5
  with:
    go-version: '1.23'

- name: Install PromptArena
  run: go install github.com/altairalabs/promptkit/tools/arena@latest
```

For this tutorial, we'll use npm as it's simpler and faster.

## Step 3: Create GitHub Actions Workflow

Create `.github/workflows/llm-tests.yml`:

```yaml
name: LLM Quality Tests

on:
  # Run on every push to main and PRs
  push:
    branches: [main]
  pull_request:
    branches: [main]
  
  # Allow manual trigger
  workflow_dispatch:

jobs:
  # Fast smoke tests (< 1 minute)
  smoke-tests:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    
    steps:
      - name: Checkout code
        uses: actions/checkout@v4
      
      - name: Install PromptArena
        run: npm install -g @altairalabs/promptarena
      
      - name: Run smoke tests with mock provider
        working-directory: tests
        run: |
          promptarena run \
            --scenario smoke \
            --mock-provider \
            --ci \
            --format junit,json
      
      - name: Upload smoke test results
        uses: actions/upload-artifact@v4
        if: always()
        with:
          name: smoke-test-results
          path: tests/out/
  
  # Comprehensive tests (with real LLMs)
  integration-tests:
    runs-on: ubuntu-latest
    needs: smoke-tests  # Only run if smoke tests pass
    timeout-minutes: 15
    
    steps:
      - name: Checkout code
        uses: actions/checkout@v4
      
      - name: Install PromptArena
        run: npm install -g @altairalabs/promptarena
      
      - name: Run integration tests
        working-directory: tests
        env:
          OPENAI_API_KEY: $
          ANTHROPIC_API_KEY: $
        run: |
          promptarena run \
            --scenario integration \
            --ci \
            --concurrency 2 \
            --format junit,json,html
      
      - name: Publish test results
        uses: dorny/test-reporter@v1
        if: always()
        with:
          name: LLM Integration Tests
          path: tests/out/junit.xml
          reporter: java-junit
      
      - name: Upload test artifacts
        uses: actions/upload-artifact@v4
        if: always()
        with:
          name: integration-test-results
          path: tests/out/
      
      - name: Check quality gate
        run: |
          PASS_RATE=$(jq '.summary.pass_rate' tests/out/results.json)
          echo "Pass rate: $PASS_RATE"
          
          if (( $(echo "$PASS_RATE < 0.95" | bc -l) )); then
            echo "❌ Quality gate failed: Pass rate $PASS_RATE < 95%"
            exit 1
          fi
          
          echo "✅ Quality gate passed: $PASS_RATE >= 95%"
```

## Step 4: Configure API Keys as Secrets

In your GitHub repository:

1. Go to **Settings** → **Secrets and variables** → **Actions**
2. Click **New repository secret**
3. Add your API keys:
   - `OPENAI_API_KEY`
   - `ANTHROPIC_API_KEY`
   - `GOOGLE_API_KEY`

## Step 5: Create Test Suites for CI

### Smoke Tests (Fast Validation)

`scenarios/smoke/basic.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-response-test
  labels:
    category: smoke
    speed: fast

spec:
  task_type: support
  
  turns:
    - role: user
      content: "Hello"
      assertions:
        - type: content_matches
          params:
            pattern: "^.{1,200}$"
            message: "Response should be brief"
```

### Critical Path Tests

`scenarios/critical/core.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: core-functionality
  labels:
    category: critical
    priority: must-pass

spec:
  task_type: support
  
  turns:
    - role: user
      content: "What are your business hours?"
      assertions:
        - type: content_includes
          params:
            patterns: ["Monday"]
            message: "Should mention business days"
    
    - role: user
      content: "How do I contact support?"
      assertions:
        - type: content_includes
          params:
            patterns: ["email"]
            message: "Should provide contact methods"
```

## Step 6: Add Quality Gates

Create `tests/quality-gates.sh`:

```bash
#!/bin/bash
set -e

RESULTS_FILE="out/results.json"

# Extract metrics
PASS_RATE=$(jq '.summary.pass_rate' $RESULTS_FILE)
TOTAL=$(jq '.summary.total' $RESULTS_FILE)
FAILED=$(jq '.summary.failed' $RESULTS_FILE)

echo "📊 Test Results:"
echo "  Total: $TOTAL"
echo "  Pass Rate: $PASS_RATE"
echo "  Failed: $FAILED"

# Quality gates
MIN_PASS_RATE=0.95
MAX_FAILURES=5

echo ""
echo "🚦 Quality Gates:"

# Check pass rate
if (( $(echo "$PASS_RATE < $MIN_PASS_RATE" | bc -l) )); then
  echo "❌ Pass rate $PASS_RATE < $MIN_PASS_RATE"
  exit 1
fi
echo "✅ Pass rate: $PASS_RATE >= $MIN_PASS_RATE"

# Check failure count
if [ "$FAILED" -gt "$MAX_FAILURES" ]; then
  echo "❌ Too many failures: $FAILED > $MAX_FAILURES"
  exit 1
fi
echo "✅ Failure count: $FAILED <= $MAX_FAILURES"

echo ""
echo "✅ All quality gates passed!"
```

Use in workflow:

```yaml
- name: Run tests
  run: promptarena run --ci --format json

- name: Check quality gates
  run: bash tests/quality-gates.sh
```

## Step 7: Optimize for CI Performance

### Use Concurrency Control

```bash
# Respect rate limits
promptarena run --concurrency 2 --ci
```

### Cache Test Results

```yaml
- name: Cache test dependencies
  uses: actions/cache@v3
  with:
    path: |
      ~/.cache/go-build
      ~/go/pkg/mod
    key: $-go-$
```

### Selective Testing

```bash
# Only run changed scenarios
if [ "$GITHUB_EVENT_NAME" = "pull_request" ]; then
  promptarena run --scenario critical --ci
else
  promptarena run --ci  # Full suite on main
fi
```

## Step 8: PR Comments with Results

Add test results to PR comments:

```yaml
- name: Comment PR with results
  if: github.event_name == 'pull_request'
  uses: actions/github-script@v7
  with:
    script: |
      const fs = require('fs');
      const results = JSON.parse(fs.readFileSync('tests/out/results.json', 'utf8'));
      
      const summary = results.summary;
      const passRate = (summary.pass_rate * 100).toFixed(1);
      
      const comment = `
      ## 🤖 LLM Test Results
      
      | Metric | Value |
      |--------|-------|
      | Total Tests | ${summary.total} |
      | Passed | ${summary.passed} ✅ |
      | Failed | ${summary.failed} ❌ |
      | Pass Rate | ${passRate}% |
      
      ${passRate >= 95 ? '✅ Quality gate: PASSED' : '❌ Quality gate: FAILED'}
      
      [View detailed report](https://github.com/$/actions/runs/$)
      `;
      
      github.rest.issues.createComment({
        issue_number: context.issue.number,
        owner: context.repo.owner,
        repo: context.repo.repo,
        body: comment
      });
```

## Step 9: Multi-Environment Testing

Test across dev, staging, production:

```yaml
strategy:
  matrix:
    environment: [dev, staging, prod]

steps:
  - name: Run tests in $
    env:
      OPENAI_API_KEY: ${{ secrets[format('{0}_OPENAI_API_KEY', matrix.environment)] }}
    run: |
      promptarena run \
        --config arena-$.yaml \
        --ci \
        --out out/$
```

## Step 10: Scheduled Testing

Run tests on a schedule:

```yaml
on:
  schedule:
    # Every 6 hours
    - cron: '0 */6 * * *'

jobs:
  scheduled-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Install PromptArena
        run: npm install -g @altairalabs/promptarena
      
      - name: Run full test suite
        env:
          OPENAI_API_KEY: $
        run: |
          promptarena run --ci --format json,html
      
      - name: Notify on failure
        if: failure()
        uses: slackapi/slack-github-action@v1
        with:
          payload: |
            {
              "text": "🚨 Scheduled LLM tests failed",
              "blocks": [
                {
                  "type": "section",
                  "text": {
                    "type": "mrkdwn",
                    "text": "*Scheduled LLM Tests Failed*\n<https://github.com/$/actions/runs/$|View Details>"
                  }
                }
              ]
            }
        env:
          SLACK_WEBHOOK_URL: $
```

## Step 11: Deployment Gates

Block deployments on test failures:

```yaml
# .github/workflows/deploy.yml
name: Deploy

on:
  push:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Install PromptArena
        run: npm install -g @altairalabs/promptarena
      
      - name: Run LLM tests
        env:
          OPENAI_API_KEY: $
        run: |
          promptarena run --scenario critical --ci
  
  deploy:
    needs: test  # Only deploy if tests pass
    runs-on: ubuntu-latest
    steps:
      - name: Deploy to production
        run: |
          echo "Deploying..."
          # Your deployment commands
```

## Complete Example Workflow

Here's a production-ready workflow:

```yaml
name: LLM Quality Pipeline

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]
  schedule:
    - cron: '0 */6 * * *'

env:
  GO_VERSION: '1.23'

jobs:
  smoke:
    name: Smoke Tests (Mock)
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - uses: actions/checkout@v4
      
      - name: Install Arena
        run: npm install -g @altairalabs/promptarena
      
      - name: Smoke tests
        working-directory: tests
        run: promptarena run --scenario smoke --mock-provider --ci --format junit
      
      - name: Publish results
        uses: dorny/test-reporter@v1
        if: always()
        with:
          name: Smoke Tests
          path: tests/out/junit.xml
          reporter: java-junit
  
  critical:
    name: Critical Path Tests
    needs: smoke
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v4
      
      - name: Install Arena
        run: npm install -g @altairalabs/promptarena
      
      - name: Critical tests
        working-directory: tests
        env:
          OPENAI_API_KEY: $
        run: |
          promptarena run \
            --scenario critical \
            --ci \
            --concurrency 2 \
            --format junit,json,html
      
      - name: Quality gate
        working-directory: tests
        run: |
          PASS_RATE=$(jq '.summary.pass_rate' out/results.json)
          if (( $(echo "$PASS_RATE < 0.95" | bc -l) )); then
            echo "❌ Quality gate failed: $PASS_RATE < 95%"
            exit 1
          fi
      
      - name: Upload results
        uses: actions/upload-artifact@v4
        if: always()
        with:
          name: critical-test-results
          path: tests/out/
  
  integration:
    name: Integration Tests
    needs: critical
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v4
      
      - name: Install Arena
        run: npm install -g @altairalabs/promptarena
      
      - name: Full test suite
        working-directory: tests
        env:
          OPENAI_API_KEY: $
          ANTHROPIC_API_KEY: $
          GOOGLE_API_KEY: $
        run: |
          promptarena run \
            --ci \
            --concurrency 3 \
            --format junit,json,html
      
      - name: Upload results
        uses: actions/upload-artifact@v4
        if: always()
        with:
          name: integration-test-results
          path: tests/out/
```

## Best Practices

### 1. Fast Feedback Loop

```yaml
# Stage 1: Mock tests (< 1 min)
# Stage 2: Critical tests (< 5 min)
# Stage 3: Full suite (< 20 min)
```

### 2. Fail Fast

```yaml
timeout-minutes: 10  # Kill hung tests
--concurrency 2      # Respect rate limits
--ci                 # Optimized output
```

### 3. Secure Secrets

```yaml
# ✅ Use GitHub Secrets
env:
  OPENAI_API_KEY: $

# ❌ Never commit keys
env:
  OPENAI_API_KEY: "sk-..."  # WRONG!
```

### 4. Informative Reports

```bash
# Generate multiple formats
--format junit,json,html

# Upload for review
uses: actions/upload-artifact@v4
```

## Troubleshooting

### Tests Timeout

```yaml
# Increase timeout
timeout-minutes: 20

# Reduce concurrency
--concurrency 1
```

### Rate Limiting

```bash
# Lower concurrency
--concurrency 1

# Use mock providers for structure validation
--mock-provider
```

### Secrets Not Working

```bash
# Check secret is set
echo "Key length: ${#OPENAI_API_KEY}"
# Should output: Key length: 51 (not 0)
```

## Next Steps

Congratulations! You've completed all Arena tutorials.

**What's next:**
- **[Arena How-To Guides](../how-to/)** - Practical recipes
- **[Arena Reference](../reference/)** - Complete API docs
- **[SDK Tutorials](../../sdk/tutorials/)** - Integrate Arena with your app

**Advanced topics:**
- Set up trend analysis
- Create custom quality metrics
- Build deployment pipelines
- Implement A/B testing

You're now ready to build production-grade LLM testing pipelines!

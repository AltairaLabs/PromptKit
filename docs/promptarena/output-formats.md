---
layout: page
title: "Output Formats"
permalink: /docs/promptarena/output-formats/
parent: PromptArena
nav_order: 6
---


PromptArena supports multiple output formats for test results, each optimized for different use cases. You can generate multiple formats simultaneously from a single test run.

## Supported Formats

| Format | Use Case | File Extension | CI/CD Integration |
|--------|----------|----------------|-------------------|
| **JSON** | Programmatic access, APIs | `.json` | ✅ Excellent |
| **HTML** | Human review, reports | `.html` | ⚠️ Manual review |
| **Markdown** | Documentation, GitHub | `.md` | ✅ Good |
| **JUnit XML** | CI/CD systems | `.xml` | ✅ Excellent |

## Configuration

Configure output in `arena.yaml`:

```yaml
defaults:
  output:
    dir: out                          # Output directory
    formats: ["json", "html", "markdown", "junit"]

    # Format-specific options
    json:
      file: results.json
      pretty: true
      include_raw: false

    html:
      file: report.html
      include_metadata: true
      theme: light

    markdown:
      file: report.md
      include_details: true

    junit:
      file: junit.xml
      include_system_out: true
```

## JSON Format

Machine-readable format for programmatic access and integrations.

### Structure

```json
{
  "arena_config": {
    "name": "customer-support-arena",
    "timestamp": "2024-01-15T10:30:00Z",
    "version": "v1.0.0"
  },
  "summary": {
    "total_tests": 15,
    "passed": 12,
    "failed": 3,
    "errors": 0,
    "skipped": 0,
    "total_duration": "45.2s",
    "total_cost": 0.0234,
    "total_tokens": 4521
  },
  "results": [
    {
      "scenario": "basic-qa",
      "provider": "openai-gpt4o-mini",
      "status": "passed",
      "duration": "3.2s",
      "turns": [
        {
          "turn_number": 1,
          "role": "user",
          "content": "What is the capital of France?",
          "response": {
            "role": "assistant",
            "content": "The capital of France is Paris.",
            "cost_info": {
              "input_tokens": 25,
              "output_tokens": 12,
              "cost": 0.00001
            }
          },
          "assertions": [
            {
              "type": "content_includes",
              "passed": true,
              "message": "Should mention Paris",
              "details": null
            }
          ]
        }
      ],
      "cost_info": {
        "total_input_tokens": 75,
        "total_output_tokens": 45,
        "total_cost": 0.00003
      }
    }
  ]
}
```

### Configuration Options

```yaml
json:
  file: results.json          # Output filename
  pretty: true                # Pretty-print JSON
  include_raw: false          # Include raw API responses
```

### Use Cases

**1. API Integration**
```bash
# Parse results in script
jq '.summary.passed' out/results.json
```

**2. Custom Reporting**
```python
import json

with open('out/results.json') as f:
    results = json.load(f)

passed = results['summary']['passed']
total = results['summary']['total_tests']
print(f"Pass rate: {passed/total*100:.1f}%")
```

**3. Data Analysis**
```python
# Analyze costs per provider
for result in results['results']:
    provider = result['provider']
    cost = result['cost_info']['total_cost']
    print(f"{provider}: ${cost:.4f}")
```

### Schema

Complete schema definition:

```typescript
interface TestResults {
  arena_config: {
    name: string
    timestamp: string
    version: string
  }
  summary: {
    total_tests: number
    passed: number
    failed: number
    errors: number
    skipped: number
    total_duration: string
    total_cost: number
    total_tokens: number
    average_cost: number
  }
  results: TestResult[]
}

interface TestResult {
  scenario: string
  provider: string
  status: "passed" | "failed" | "error" | "skipped"
  duration: string
  error?: string
  turns: Turn[]
  cost_info: CostInfo
  metadata?: Record<string, any>
}

interface Turn {
  turn_number: number
  role: "user" | "assistant"
  content: string
  response?: {
    role: string
    content: string
    tool_calls?: ToolCall[]
    cost_info: CostInfo
  }
  assertions?: Assertion[]
}

interface Assertion {
  type: string
  passed: boolean
  message: string
  details?: any
}

interface CostInfo {
  input_tokens: number
  output_tokens: number
  cached_tokens?: number
  cost: number
}
```

## HTML Format

Interactive HTML report for human review.

### Features

- **Summary Dashboard**: Overview with metrics
- **Provider Comparison**: Side-by-side results
- **Conversation View**: Full conversation transcripts
- **Assertion Details**: Pass/fail status with messages
- **Cost Breakdown**: Token usage and costs
- **Filtering**: Filter by status, provider, scenario
- **Theming**: Light and dark modes

### Example Report

```html
<!DOCTYPE html>
<html>
<head>
  <title>PromptArena Test Report</title>
  <style>/* Embedded CSS */</style>
</head>
<body>
  <div class="summary-card">
    <h2>Test Summary</h2>
    <div class="metrics">
      <div class="metric">
        <span class="label">Total</span>
        <span class="value">15</span>
      </div>
      <div class="metric success">
        <span class="label">Passed</span>
        <span class="value">12</span>
      </div>
      <div class="metric failure">
        <span class="label">Failed</span>
        <span class="value">3</span>
      </div>
    </div>
  </div>

  <!-- Detailed results -->
  <div class="results">
    <!-- ... -->
  </div>
</body>
</html>
```

### Configuration Options

```yaml
html:
  file: report.html           # Output filename
  include_metadata: true      # Include test metadata
  theme: light                # Theme: light | dark
```

### Viewing

```bash
# Open in browser
open out/report.html

# Or serve via HTTP
python -m http.server 8000
# Navigate to http://localhost:8000/out/report.html
```

### Sections

#### 1. Summary Dashboard

```
┌─────────────────────────────────────────┐
│ PromptArena Test Report                 │
│                                          │
│ Total: 15  Passed: 12  Failed: 3        │
│ Duration: 45.2s  Cost: $0.0234          │
│ Tokens: 4521 (input: 3200, output: 1321)│
└─────────────────────────────────────────┘
```

#### 2. Provider Comparison

```
┌────────────────┬──────────┬─────────┬────────┐
│ Provider       │ Tests    │ Pass %  │ Cost   │
├────────────────┼──────────┼─────────┼────────┤
│ GPT-4o-mini    │ 5        │ 100%    │ $0.008 │
│ Claude Sonnet  │ 5        │ 80%     │ $0.015 │
│ Gemini Flash   │ 5        │ 80%     │ $0.001 │
└────────────────┴──────────┴─────────┴────────┘
```

#### 3. Detailed Results

Each test shows:
- Scenario name and description
- Provider and model
- Pass/fail status
- Full conversation transcript
- Assertion results
- Token usage and cost
- Execution time

### Customization

The HTML report uses embedded CSS. To customize:

1. Generate report
2. Save HTML file
3. Edit `<style>` section
4. Reload in browser

## Markdown Format

GitHub-friendly markdown format for documentation.

### Structure

```markdown
# PromptArena Test Report

**Generated**: 2024-01-15 10:30:00
**Arena**: customer-support-arena

## Summary

- **Total Tests**: 15
- **Passed**: 12 ✅
- **Failed**: 3 ❌
- **Duration**: 45.2s
- **Cost**: $0.0234
- **Tokens**: 4,521

## Results by Provider

### OpenAI GPT-4o-mini

#### Scenario: basic-qa

**Status**: ✅ PASSED
**Duration**: 3.2s
**Cost**: $0.00003

##### Turn 1

**User**: What is the capital of France?

**Assistant**: The capital of France is Paris.

**Assertions**:
- ✅ content_includes: Should mention Paris

**Tokens**: 37 (input: 25, output: 12)
**Cost**: $0.00001

---

### Claude 3.5 Sonnet

...
```

### Configuration Options

```yaml
markdown:
  file: report.md             # Output filename
  include_details: true       # Include full conversation details
```

### Use Cases

**1. GitHub Actions Summary**
```yaml
- name: Generate Report
  run: promptarena run arena.yaml
- name: Comment on PR
  uses: actions/github-script@v6
  with:
    script: |
      const fs = require('fs');
      const report = fs.readFileSync('out/report.md', 'utf8');
      github.rest.issues.createComment({
        issue_number: context.issue.number,
        owner: context.repo.owner,
        repo: context.repo.repo,
        body: report
      });
```

**2. Documentation**

Include test results in docs:

```markdown
# API Testing Results

[include file="test-results/report.md"]
```

**3. Slack/Teams Notifications**

Send markdown to collaboration tools:

```bash
# Convert to Slack format
cat out/report.md | slack-markdown-converter | \
  slack-cli chat-post-message --channel #testing
```

## JUnit XML Format

Standard format for CI/CD systems (Jenkins, GitLab CI, GitHub Actions, etc.).

### Structure

```xml
<?xml version="1.0" encoding="UTF-8"?>
<testsuites name="PromptArena Tests" tests="15" failures="3" errors="0" time="45.2">
  <testsuite name="basic-qa" tests="3" failures="0" errors="0" time="9.6">
    <testcase name="basic-qa.openai-gpt4o-mini" classname="basic-qa" time="3.2">
      <system-out>
        Turn 1:
        User: What is the capital of France?
        Assistant: The capital of France is Paris.
        Assertions: ✅ content_includes
      </system-out>
    </testcase>

    <testcase name="basic-qa.claude-sonnet" classname="basic-qa" time="3.1">
      <failure message="Assertion failed: content_includes" type="AssertionFailure">
        Expected: Paris
        Actual: The capital city of France is Paris.
        Assertion: Should mention Paris
      </failure>
    </testcase>
  </testsuite>
</testsuites>
```

### Configuration Options

```yaml
junit:
  file: junit.xml             # Output filename
  include_system_out: true    # Include conversation in <system-out>
```

### CI/CD Integration

#### GitHub Actions

```yaml
- name: Run Tests
  run: promptarena run arena.yaml

- name: Publish Test Results
  uses: EnricoMi/publish-unit-test-result-action@v2
  if: always()
  with:
    files: out/junit.xml
```

#### GitLab CI

```yaml
test:
  script:
    - promptarena run arena.yaml
  artifacts:
    reports:
      junit: out/junit.xml
```

#### Jenkins

```groovy
pipeline {
  stages {
    stage('Test') {
      steps {
        sh 'promptarena run arena.yaml'
      }
      post {
        always {
          junit 'out/junit.xml'
        }
      }
    }
  }
}
```

#### CircleCI

```yaml
- run:
    name: Run Tests
    command: promptarena run arena.yaml
- store_test_results:
    path: out/junit.xml
```

## Multiple Formats

Generate all formats in one run:

```yaml
defaults:
  output:
    formats: ["json", "html", "markdown", "junit"]
```

Output structure:
```
out/
├── results.json
├── report.html
├── report.md
└── junit.xml
```

## Custom Output Directory

```yaml
defaults:
  output:
    dir: test-results-2024-01-15
```

```bash
# Or override via CLI
promptarena run arena.yaml --output custom-dir
```

## Programmatic Access

### Python

```python
import json

# Load JSON results
with open('out/results.json') as f:
    results = json.load(f)

# Calculate pass rate
summary = results['summary']
pass_rate = summary['passed'] / summary['total_tests'] * 100

# Find expensive scenarios
for result in results['results']:
    if result['cost_info']['total_cost'] > 0.01:
        print(f"Expensive: {result['scenario']} - ${result['cost_info']['total_cost']:.4f}")

# Find failing assertions
for result in results['results']:
    if result['status'] == 'failed':
        for turn in result['turns']:
            for assertion in turn.get('assertions', []):
                if not assertion['passed']:
                    print(f"Failed: {result['scenario']} - {assertion['message']}")
```

### Node.js

```javascript
const fs = require('fs');

// Load results
const results = JSON.parse(fs.readFileSync('out/results.json'));

// Generate custom report
const report = results.results.map(r => ({
  scenario: r.scenario,
  provider: r.provider,
  passed: r.status === 'passed',
  cost: r.cost_info.total_cost
}));

console.table(report);
```

### Go

```go
package main

import (
    "encoding/json"
    "os"
)

type Results struct {
    Summary struct {
        TotalTests int     `json:"total_tests"`
        Passed     int     `json:"passed"`
        TotalCost  float64 `json:"total_cost"`
    } `json:"summary"`
}

func main() {
    data, _ := os.ReadFile("out/results.json")
    var results Results
    json.Unmarshal(data, &results)

    passRate := float64(results.Summary.Passed) / float64(results.Summary.TotalTests) * 100
    fmt.Printf("Pass Rate: %.1f%%\n", passRate)
    fmt.Printf("Total Cost: $%.4f\n", results.Summary.TotalCost)
}
```

## Performance Considerations

### File Sizes

Typical sizes for 100 tests:

| Format | Approx. Size | Notes |
|--------|--------------|-------|
| JSON | 500 KB | Can be large with raw responses |
| HTML | 800 KB | Embedded CSS/JS |
| Markdown | 300 KB | Most compact |
| JUnit XML | 200 KB | Minimal data |

### Optimization

**Reduce JSON size**:
```yaml
json:
  include_raw: false          # Omit raw API responses
  pretty: false               # No formatting
```

**Faster HTML generation**:
```yaml
html:
  include_metadata: false     # Skip detailed metadata
```

## Best Practices

### 1. Use Right Format for Context

```yaml
# Development
formats: ["html"]              # Quick visual review

# CI/CD
formats: ["junit", "json"]     # Integration + data

# Documentation
formats: ["markdown"]          # Human-readable

# Production
formats: ["json", "junit"]     # Programmatic + CI
```

### 2. Version Control

```gitignore
# .gitignore
out/
test-results/
*.html
```

Commit configuration, not results.

### 3. Archive Historical Results

```bash
# Archive with timestamp
DATE=$(date +%Y%m%d-%H%M%S)
mv out test-results-$DATE
tar -czf test-results-$DATE.tar.gz test-results-$DATE
```

### 4. Parse for Metrics

```bash
# Extract pass rate
jq '.summary | {total: .total_tests, passed: .passed, rate: ((.passed / .total_tests) * 100)}' out/results.json

# Extract cost by provider
jq '.results | group_by(.provider) | map({provider: .[0].provider, cost: map(.cost_info.total_cost) | add})' out/results.json
```

## Next Steps

- **[CI/CD Integration](./ci-cd-integration.md)** - Running in pipelines
- **[Configuration Reference](./config-reference.md)** - Output configuration
- **[Best Practices](./best-practices.md)** - Production tips

---

**Examples**: See `examples/` for output configuration patterns.

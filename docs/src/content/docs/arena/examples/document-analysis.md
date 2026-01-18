---
title: Document Analysis
description: Test PDF document analysis with multiple providers
sidebar:
  order: 60
---

This example demonstrates document attachment support (PDFs) using PromptArena configuration and testing framework.

## Overview

Test document analysis workflows with multiple providers using declarative YAML configuration. This example shows how to:

- Attach PDF documents to prompts using media configuration
- Test document analysis across different LLM providers (Claude, Gemini)
- Run deterministic tests with mock providers for CI/CD
- Compare multiple documents or mix documents with images

## Quick Start

### Run with Mock Provider

Test without API calls:

```bash
cd examples/document-analysis
promptarena run config.arena.yaml --provider mock-docs
```

### Run with Real Providers

```bash
# Claude
export ANTHROPIC_API_KEY=your-key
promptarena run config.arena.yaml --provider claude-docs

# Gemini
export GEMINI_API_KEY=your-key
promptarena run config.arena.yaml --provider gemini-docs
```

## File Structure

```
document-analysis/
├── config.arena.yaml           # Main Arena configuration
├── prompts/
│   └── document-analyzer.yaml  # Prompt with document media support
├── providers/
│   ├── claude-docs.provider.yaml   # Claude configuration
│   ├── gemini-docs.provider.yaml   # Gemini configuration
│   └── mock-docs.provider.yaml     # Mock provider for testing
├── scenarios/
│   ├── pdf-summary.scenario.yaml       # Single document analysis
│   ├── multi-doc-compare.scenario.yaml # Multiple document comparison
│   └── mixed-media.scenario.yaml       # Document + image analysis
├── mock-responses.yaml         # Deterministic test responses
└── test-docs/                  # Sample test documents
    ├── sample.pdf
    ├── version1.pdf
    ├── version2.pdf
    ├── specs.pdf
    └── diagram.png
```

## Configuration

### Main Configuration

```yaml
# config.arena.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: ArenaConfig
metadata:
  name: document-analysis-arena
spec:
  version: "1.0"
  prompts:
    - prompts/document-analyzer.yaml
  providers:
    - providers/claude-docs.provider.yaml
    - providers/gemini-docs.provider.yaml
    - providers/mock-docs.provider.yaml
  scenarios:
    - scenarios/pdf-summary.scenario.yaml
    - scenarios/multi-doc-compare.scenario.yaml
  output:
    formats: [html, json, markdown]
    directory: out
```

### Document Analyzer Prompt

```yaml
# prompts/document-analyzer.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: document-analyzer
spec:
  system_template: |
    You are a document analysis expert. When presented with PDF documents:
    1. Read and understand the document content thoroughly
    2. Respond to the user's question accurately based on the document
    3. Cite specific sections when relevant
    4. Be concise but comprehensive

  user_template: "{{ user_question }}"

  media:
    enabled: true
    supported_types: [document, image]
```

## Scenarios

### 1. Single PDF Summary

Analyze a single PDF document:

```yaml
# scenarios/pdf-summary.scenario.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: pdf-summary
spec:
  description: "Analyze a single PDF document"
  variables:
    user_question: "Please provide a brief summary of this document"
  
  media:
    - type: document
      source: test-docs/sample.pdf
  
  assertions:
    - type: contains
      value: "Test PDF"
```

**Run it:**

```bash
promptarena run config.arena.yaml --scenario pdf-summary --provider claude-docs
```

### 2. Multi-Document Comparison

Compare multiple PDF documents:

```yaml
# scenarios/multi-doc-compare.scenario.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: multi-doc-compare
spec:
  description: "Compare multiple PDF documents"
  variables:
    user_question: "I've attached multiple versions. Can you confirm you received them all?"
  
  media:
    - type: document
      source: test-docs/version1.pdf
    - type: document
      source: test-docs/version2.pdf
    - type: document
      source: test-docs/specs.pdf
  
  assertions:
    - type: contains
      value: "received"
```

**Run it:**

```bash
promptarena run config.arena.yaml --scenario multi-doc-compare --provider claude-docs
```

### 3. Mixed Media Analysis

Analyze documents together with images:

```yaml
# scenarios/mixed-media.scenario.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: mixed-media
spec:
  description: "Analyze PDF and image together"
  variables:
    user_question: "I'm sending a PDF and an image. Please confirm you received both."
  
  media:
    - type: document
      source: test-docs/sample.pdf
    - type: image
      source: test-docs/diagram.png
  
  assertions:
    - type: contains
      value: "PDF"
    - type: contains
      value: "image"
```

**Run it:**

```bash
promptarena run config.arena.yaml --scenario mixed-media --provider gemini-docs
```

## Provider Configuration

### Claude Provider

```yaml
# providers/claude-docs.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: claude-docs
spec:
  type: anthropic
  model: claude-3-5-haiku-20241022
  parameters:
    temperature: 0.3
    max_tokens: 2048
```

**Capabilities:**
- Max PDF size: 32MB
- Fast processing with Haiku
- Excellent document understanding

### Gemini Provider

```yaml
# providers/gemini-docs.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: gemini-docs
spec:
  type: gemini
  model: gemini-2.0-flash
  parameters:
    temperature: 0.3
    max_tokens: 2048
```

**Capabilities:**
- Max PDF size: 20MB
- Very fast processing
- Good for quick analysis

### Mock Provider

```yaml
# providers/mock-docs.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: mock-docs
spec:
  type: mock
  model: mock-model
```

Configure deterministic responses:

```yaml
# mock-responses.yaml
pdf-summary:
  - role: assistant
    content: "This PDF contains the text 'Test PDF'."

multi-doc-compare:
  - role: assistant
    content: "I received 3 PDFs: version1.pdf, version2.pdf, and specs.pdf."
```

## Running Tests

### All Scenarios, All Providers

```bash
promptarena run config.arena.yaml
```

### Specific Scenario

```bash
promptarena run config.arena.yaml --scenario pdf-summary
```

### Specific Provider

```bash
promptarena run config.arena.yaml --provider claude-docs
```

### CI Mode (Exit on Failure)

```bash
promptarena run config.arena.yaml --ci
```

### Generate HTML Report

```bash
promptarena run config.arena.yaml --format html
open out/document-report.html
```

## Expected Results

When all tests pass, you'll see:

```
✓ pdf-summary [claude-docs] passed (2.5s)
✓ pdf-summary [gemini-docs] passed (1.8s)
✓ pdf-summary [mock-docs] passed (0.1s)
✓ multi-doc-compare [claude-docs] passed (4.2s)
✓ multi-doc-compare [gemini-docs] passed (3.1s)
✓ multi-doc-compare [mock-docs] passed (0.1s)

6/6 tests passed
```

## HTML Report Features

The generated HTML report shows:

- **Document Badges**: 📄 icon with file paths
- **Media Summary**: Count of documents attached
- **Provider Comparison**: Side-by-side results
- **Assertion Results**: Pass/fail for each test
- **Response Content**: Full LLM responses

Example report section:

```
Scenario: pdf-summary
Media: 📄 1 document (test-docs/sample.pdf)
Provider: claude-docs
Response: "This PDF contains the text 'Test PDF'."
Assertions: ✓ contains "Test PDF"
```

## Test Documents

### sample.pdf
Simple test PDF containing the text "Test PDF" - used for basic validation.

### version1.pdf, version2.pdf
Two versions of the same document with minor differences - used for comparison testing.

### specs.pdf
Technical specification document - used in multi-document scenarios.

### diagram.png
Simple diagram image - used for mixed media testing.

## Common Issues

### File Not Found

```
Error: document file not found: test-docs/sample.pdf
```

**Solution:** Run from the `examples/document-analysis` directory or check file paths.

### API Key Not Set

```
Error: ANTHROPIC_API_KEY not set
```

**Solution:**
```bash
export ANTHROPIC_API_KEY=your-key
```

Or use the mock provider for testing without API keys.

### File Too Large

```
Error: document size exceeds provider limit
```

**Solution:**
- Claude: Max 32MB
- Gemini: Max 20MB
- Compress PDF or split into smaller files

## Advanced Usage

### Custom Assertions

Add more specific assertions:

```yaml
assertions:
  - type: contains
    value: "summary"
  - type: not_contains
    value: "error"
  - type: regex
    pattern: "\\d+ pages?"
  - type: length_range
    min: 50
    max: 500
```

### Variable Substitution

Use variables in questions:

```yaml
variables:
  document_type: "contract"
  analysis_focus: "financial terms"
  user_question: "Analyze this {{ document_type }} focusing on {{ analysis_focus }}"
```

### Multi-Turn Conversations

Create follow-up questions:

```yaml
turns:
  - role: user
    content:
      - type: text
        patterns: ["Summarize this document"]
      - type: document
        document_url:
          url: "test-docs/sample.pdf"
    
  - role: assistant
    content:
      - type: text
        patterns: ["Here's the summary:"]
  
  - role: user
    content:
      - type: text
        patterns: ["What's on page 2?"]
```

## Integration with SDK

Use the same documents in SDK code:

```go
import "github.com/AltairaLabs/PromptKit/sdk"

conv, _ := sdk.Open("./app.pack.json", "document-analyzer")

resp, err := conv.Send(ctx, "Summarize this document",
    sdk.WithDocumentFile("test-docs/sample.pdf"),
)
```

## See Also

- [Arena Scenario Format](../reference/scenario-format) - Full scenario specification
- [Analyze Documents (SDK)](../../sdk/how-to/analyze-documents) - SDK implementation
- [Media Testing Example](./arena-media-test) - Images, audio, video testing
- [Output Formats](../reference/output-formats) - HTML, JSON, Markdown reports

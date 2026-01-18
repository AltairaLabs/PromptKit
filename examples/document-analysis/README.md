# Document Analysis Example

This example demonstrates document attachment support (PDFs, Word docs, etc.) using PromptArena configuration and testing framework.

## Overview

PromptArena allows you to test document analysis workflows with multiple providers using declarative YAML configuration. This example shows how to:

- Attach PDF documents to prompts using media configuration
- Test document analysis across different LLM providers (Claude, Gemini)
- Run deterministic tests with mock providers for CI/CD
- Compare multiple documents or mix documents with images

## Quick Start

Run all scenarios with the mock provider (no API calls):

```bash
promptarena run config.arena.yaml --provider mock-docs
```

Run with a real provider:

```bash
export ANTHROPIC_API_KEY=your-key
promptarena run config.arena.yaml --provider claude-docs
```

## Configuration Structure

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

## Scenarios Explained

### 1. PDF Summary (`pdf-summary.scenario.yaml`)

Analyzes a single PDF document:

```yaml
variables:
  user_question: "Please provide a brief summary of this document"

media:
  - type: document
    source: test-docs/sample.pdf
```

Run it:
```bash
promptarena run config.arena.yaml --scenario pdf-summary --provider claude-docs
```

### 2. Multi-Document Compare (`multi-doc-compare.scenario.yaml`)

Compares multiple PDF documents:

```yaml
variables:
  user_question: "Compare these documents and identify key differences"

media:
  - type: document
    source: test-docs/version1.pdf
  - type: document
    source: test-docs/version2.pdf
  - type: document
    source: test-docs/specs.pdf
```

Run it:
```bash
promptarena run config.arena.yaml --scenario multi-doc-compare --provider claude-docs
```

### 3. Mixed Media (`mixed-media.scenario.yaml`)

Analyzes documents together with images:

```yaml
variables:
  user_question: "Analyze the technical specifications and compare with the diagram"

media:
  - type: image
    source: test-docs/diagram.png
  - type: document
    source: test-docs/specs.pdf
```

Run it:
```bash
promptarena run config.arena.yaml --scenario mixed-media --provider gemini-docs
```

## Prompt Configuration

The prompt template ([prompts/document-analyzer.yaml](prompts/document-analyzer.yaml)) defines how media attachments are handled:

```yaml
id: document-analyzer
name: Document Analyzer
system_template: |
  You are a document analysis assistant. Analyze documents carefully and provide
  clear, structured responses.

media:
  enabled: true
  types:
    - document
    - image
```

This configuration:
- Enables media attachments
- Accepts both documents and images
- Works with multiple attachments in a single message

## Provider Configuration

### Mock Provider (CI/CD Testing)

The mock provider ([providers/mock-docs.provider.yaml](providers/mock-docs.provider.yaml)) returns predefined responses without API calls:

```yaml
id: mock-docs
name: Mock Provider for Document Tests
provider: mock

mock_responses:
  file: mock-responses.yaml
```

Perfect for:
- Automated testing in CI/CD pipelines
- Development without API costs
- Deterministic test outcomes

### Claude Provider

Claude ([providers/claude-docs.provider.yaml](providers/claude-docs.provider.yaml)) supports PDFs up to 32MB:

```yaml
id: claude-docs
name: Claude with Document Support
provider: claude
model: claude-3-5-sonnet-20241022
api_key: ${ANTHROPIC_API_KEY}
```

Usage:
```bash
export ANTHROPIC_API_KEY=your-key-here
promptarena run config.arena.yaml --provider claude-docs
```

### Gemini Provider

Gemini ([providers/gemini-docs.provider.yaml](providers/gemini-docs.provider.yaml)) supports PDFs up to 20MB:

```yaml
id: gemini-docs
name: Gemini with Document Support
provider: gemini
model: gemini-1.5-pro
api_key: ${GOOGLE_API_KEY}
```

Usage:
```bash
export GOOGLE_API_KEY=your-key-here
promptarena run config.arena.yaml --provider gemini-docs
```

## Provider Capabilities

| Provider | Max Size | Formats | Notes |
|----------|----------|---------|-------|
| Claude   | 32 MB    | PDF     | Native document support |
| Gemini   | 20 MB    | PDF     | Base64 inline data |
| Mock     | N/A      | All     | Deterministic responses |

## Using Documents in Go Code

If you need to use document attachments programmatically (not via PromptArena):

```go
import "github.com/altairalabs/promptkit/sdk"

// From file
resp, err := conv.Send(ctx, "Analyze this document",
    sdk.WithDocumentFile("path/to/document.pdf"))

// From memory
pdfData, _ := os.ReadFile("document.pdf")
resp, err := conv.Send(ctx, "Review this contract",
    sdk.WithDocumentData(pdfData, "application/pdf"))

// Multiple documents
resp, err := conv.Send(ctx, "Compare these documents",
    sdk.WithDocumentFile("v1.pdf"),
    sdk.WithDocumentFile("v2.pdf"))
```

## Testing in CI/CD

Run with the mock provider for fast, deterministic tests:

```bash
promptarena run config.arena.yaml --provider mock-docs --ci
```

The `--ci` flag:
- Exits with non-zero status on failures
- Provides structured output
- Perfect for GitHub Actions, GitLab CI, etc.

## Advanced: Custom Test Documents

Replace files in `test-docs/` with your own documents:

```bash
cp my-contract.pdf test-docs/sample.pdf
promptarena run config.arena.yaml --scenario pdf-summary --provider claude-docs
```

Or create new scenarios in `scenarios/` directory following the existing patterns.

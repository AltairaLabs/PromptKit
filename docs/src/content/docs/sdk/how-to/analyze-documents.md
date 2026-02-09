---
title: Analyze Documents
description: How to send PDF documents to LLMs for analysis
sidebar:
  order: 50
---

Send PDF documents to LLMs for analysis, summarization, comparison, and information extraction.

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    ctx := context.Background()

    conv, err := sdk.Open("./app.pack.json", "document-analyzer")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Analyze a PDF document
    resp, err := conv.Send(ctx, "Summarize this document",
        sdk.WithDocumentFile("./report.pdf"),
    )
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

## Supported Formats

Currently, the SDK supports **PDF documents only**.

| Format | MIME Type | Status |
|--------|-----------|--------|
| PDF | `application/pdf` | ✅ Supported |
| Word (.docx) | `application/vnd.openxmlformats-officedocument.wordprocessingml.document` | ❌ Not yet |
| Text (.txt) | `text/plain` | ❌ Not yet |

## Provider Support

Document analysis support varies by provider:

| Provider | Max Size | Models | Notes |
|----------|----------|--------|-------|
| **Claude** | 32MB | Haiku, Sonnet, Opus | Best for complex documents |
| **Gemini** | 20MB | Flash, Pro | Good for quick analysis |
| **OpenAI** | Varies | GPT-4V | Check model documentation |

### Recommended Models

```go
// Claude Haiku - fast, cost-effective
conv, _ := sdk.Open("./app.pack.json", "doc-analyzer",
    sdk.WithModel("claude-3-5-haiku-20241022"),
)

// Gemini Flash - very fast
conv, _ := sdk.Open("./app.pack.json", "doc-analyzer",
    sdk.WithModel("gemini-2.0-flash"),
)
```

## Input Methods

### From File Path

Most common approach - load PDF from disk:

```go
resp, err := conv.Send(ctx, "What are the key findings?",
    sdk.WithDocumentFile("./research-paper.pdf"),
)
```

### From Raw Bytes

Load PDF data from memory (e.g., from database, API response):

```go
pdfBytes, err := os.ReadFile("./document.pdf")
if err != nil {
    log.Fatal(err)
}

resp, err := conv.Send(ctx, "Summarize this",
    sdk.WithDocumentData(pdfBytes, "application/pdf"),
)
```

### Multiple Documents

Attach multiple PDFs for comparison or analysis:

```go
resp, err := conv.Send(ctx, "Compare these two contracts",
    sdk.WithDocumentFile("./contract_v1.pdf"),
    sdk.WithDocumentFile("./contract_v2.pdf"),
)
```

### Mixed Media

Combine documents with images for comprehensive analysis:

```go
resp, err := conv.Send(ctx, "Analyze the document and diagram",
    sdk.WithDocumentFile("./spec.pdf"),
    sdk.WithImageFile("./architecture.png"),
)
```

## Common Use Cases

### Document Summarization

```go
resp, err := conv.Send(ctx, 
    "Provide a concise 3-paragraph summary of the key points",
    sdk.WithDocumentFile("./report.pdf"),
)
```

### Information Extraction

```go
resp, err := conv.Send(ctx,
    "Extract all dates, names, and financial figures into a structured format",
    sdk.WithDocumentFile("./invoice.pdf"),
)
```

### Document Comparison

```go
resp, err := conv.Send(ctx,
    "List all changes between version 1 and version 2",
    sdk.WithDocumentFile("./v1.pdf"),
    sdk.WithDocumentFile("./v2.pdf"),
)
```

### Question Answering

```go
resp, err := conv.Send(ctx,
    "What is the refund policy described in this document?",
    sdk.WithDocumentFile("./terms.pdf"),
)
```

### Translation

```go
resp, err := conv.Send(ctx,
    "Translate this document to Spanish, preserving formatting",
    sdk.WithDocumentFile("./contract.pdf"),
)
```

## Error Handling

### File Size Limits

```go
resp, err := conv.Send(ctx, "Summarize",
    sdk.WithDocumentFile("./large.pdf"),
)
if err != nil {
    if strings.Contains(err.Error(), "size exceeds") {
        log.Fatal("PDF too large - max 32MB for Claude, 20MB for Gemini")
    }
    log.Fatal(err)
}
```

### File Not Found

```go
resp, err := conv.Send(ctx, "Analyze",
    sdk.WithDocumentFile("./missing.pdf"),
)
if err != nil {
    if os.IsNotExist(err) {
        log.Fatal("PDF file not found")
    }
    log.Fatal(err)
}
```

### Unsupported Format

```go
// Currently only PDFs are supported
resp, err := conv.Send(ctx, "Analyze",
    sdk.WithDocumentData(wordBytes, "application/msword"), // ❌ Not supported yet
)
```

## Best Practices

### 1. Optimize PDF Size

Keep documents under size limits:

```go
// Check file size before sending
info, _ := os.Stat("document.pdf")
sizeInMB := float64(info.Size()) / (1024 * 1024)

if sizeInMB > 30 {
    log.Printf("Warning: Large PDF (%.1fMB) - may fail with some providers", sizeInMB)
}
```

### 2. Use Specific Prompts

Be explicit about what you want:

```go
// ❌ Too vague
conv.Send(ctx, "Tell me about this", sdk.WithDocumentFile("./doc.pdf"))

// ✅ Specific and actionable
conv.Send(ctx, "Extract the table on page 3 and convert to CSV format",
    sdk.WithDocumentFile("./doc.pdf"))
```

### 3. Handle Streaming for Large Responses

For detailed analysis, use streaming:

```go
for chunk := range conv.Stream(ctx, 
    "Provide a detailed analysis of each section",
    sdk.WithDocumentFile("./report.pdf"),
) {
    if chunk.Error != nil {
        log.Fatal(chunk.Error)
    }
    if chunk.Type == sdk.ChunkDone {
        break
    }
    fmt.Print(chunk.Text)
}
```

### 4. Consider Token Costs

Documents can use many tokens:

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/events"
)

// Monitor provider calls for cost tracking
hooks.OnProviderCall(conv, func(model string, inputTokens, outputTokens int, cost float64) {
    log.Printf("Model %s: %d in, %d out, $%.4f", model, inputTokens, outputTokens, cost)
})

resp, err := conv.Send(ctx, "Analyze",
    sdk.WithDocumentFile("./long-document.pdf"),
)
```

### 5. Test with Mock Provider First

Use Arena to test document flows without API costs:

```yaml
# config.arena.yaml
providers:
  - mock-docs.provider.yaml

scenarios:
  - document-test.scenario.yaml
```

## Pack Configuration

Configure document analysis in your pack:

```json
{
  "prompts": {
    "document-analyzer": {
      "id": "document-analyzer",
      "name": "Document Analyzer",
      "system_template": "You are an expert document analyst. Provide clear, structured analysis of PDF documents.",
      "parameters": {
        "temperature": 0.3,
        "max_tokens": 4096
      }
    }
  },
  "provider": {
    "name": "claude",
    "model": "claude-3-5-haiku-20241022"
  }
}
```

## Testing with Arena

Create automated tests for document analysis:

```yaml
# scenarios/pdf-summary.scenario.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: pdf-summary
spec:
  variables:
    user_question: "Summarize the key points"
  media:
    - type: document
      source: test-docs/sample.pdf
  assertions:
    - type: contains
      value: "summary"
```

Run tests:

```bash
promptarena run config.arena.yaml --scenario pdf-summary
```

See the [Document Analysis Example](../../arena/examples/document-analysis) for a complete working example.

## Limitations

- **Format**: Only PDF documents are currently supported
- **Size**: 32MB max for Claude, 20MB for Gemini
- **Text Extraction**: OCR quality depends on PDF structure
- **Images in PDFs**: Embedded images are processed by the model
- **Formatting**: Complex layouts may not be perfectly preserved

## See Also

- [Multimodal Example](../examples/multimodal) - Images, audio, video
- [Document Analysis Arena Example](../../arena/examples/document-analysis)
- [Arena Scenario Format](../../arena/reference/scenario-format)
- [SDK Reference](../reference/)

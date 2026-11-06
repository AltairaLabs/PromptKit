---
title: Analyze documents
description: How to send PDF documents to an LLM for analysis
sidebar:
  order: 50
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/providers/base_provider.go
    - runtime/providers/claude/claude_multimodal.go
    - runtime/providers/gemini/gemini_multimodal.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/providers/media_loader.go
    - runtime/providers/multimodal.go
    - runtime/providers/openai/openai_multimodal.go
    - runtime/types/content.go
    - sdk/hooks/events.go
    - sdk/options.go
    - sdk/streaming.go
---

Attach PDF documents to a message so the model can summarize, compare or extract information from them.

## Prerequisites

- A pack with a prompt to open, such as `document-analyzer`.
- A provider that accepts documents: Claude or Gemini. Claude takes PDF only. Gemini takes PDF, Microsoft Office and OpenDocument files, and text formats such as plain text, Markdown, CSV, JSON and XML, which it reads as plain text.

## Steps

1. Open the conversation and attach a PDF by path:

   ```go
   package main

   import (
       "context"
       "fmt"
       "log"

       "github.com/AltairaLabs/PromptKit/sdk/v2"
   )

   func main() {
       conv, err := sdk.Open("./app.pack.json", "document-analyzer")
       if err != nil {
           log.Fatal(err)
       }
       defer conv.Close()

       resp, err := conv.Send(context.Background(), "Summarize this document",
           sdk.WithDocumentFile("./report.pdf"),
       )
       if err != nil {
           log.Fatal(err)
       }
       fmt.Println(resp.Text())
   }
   ```

   `WithDocumentFile` infers the MIME type from the file extension and reads the file when the provider call is built.

2. To send a PDF you already hold in memory, use `WithDocumentData` with the MIME type:

   ```go
   resp, err := conv.Send(ctx, "Summarize this",
       sdk.WithDocumentData(pdfBytes, "application/pdf"),
   )
   ```

3. To compare documents, or to combine a document with other media, pass several options. A single message accepts up to 20 media items.

   ```go
   resp, err := conv.Send(ctx, "List all changes between version 1 and version 2",
       sdk.WithDocumentFile("./v1.pdf"),
       sdk.WithDocumentFile("./v2.pdf"),
   )

   resp, err = conv.Send(ctx, "Does the diagram match the spec?",
       sdk.WithDocumentFile("./spec.pdf"),
       sdk.WithImageFile("./architecture.png"),
   )
   ```

4. For long analyses, stream the answer:

   ```go
   for chunk := range conv.Stream(ctx, "Analyze each section in detail",
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

5. Documents consume many input tokens. Read usage from the provider-call event:

   ```go
   hooks.OnProviderCall(conv, func(model string, inputTokens, outputTokens int, cost float64) {
       log.Printf("%s: %d in, %d out", model, inputTokens, outputTokens)
   })
   ```

   This needs `github.com/AltairaLabs/PromptKit/sdk/v2/hooks`. See [Monitor events](/sdk/how-to/observability/monitor-events/).

## Verify

Ask a question only the document can answer, such as "What is the refund policy described in this document?". The reply quotes the document. Check that the provider-call handler logs a high input-token count for the turn.

## Troubleshoot

Before each provider call, the SDK checks every attachment against the modalities and MIME types the provider's adapter can send. The `capabilities` list in a provider config does not narrow this check. An attachment the adapter cannot send fails the `Send` with a `*providers.UnsupportedContentError`, and no request is sent. Use `errors.As` to read its `ContentType`, `MIMEType` and `PartIndex`.

| Error text | Cause |
|---|---|
| `<provider-id>: unsupported document format: <mime> (part N)` | The provider does not take this document type. Claude takes PDF only. Convert the file before sending. |
| `<provider-id>: provider does not support document inputs (part N)` | The provider takes no documents: OpenAI, Ollama and vLLM do not. Use Claude or Gemini. |
| `file size N bytes exceeds maximum M bytes for <path>` | The file is over the 50 MB per-file limit the media loader applies. |
| `media item limit exceeded` | The message carries more than 20 media items. |

The providers declare their own document size limits: 32 MB for Claude and 20 MB for Gemini. The check does not apply these limits, so a larger file within the 50 MB loader limit reaches the provider, which rejects it.

If a custom provider declares less than it sends, open the conversation with `sdk.WithoutContentValidation()` to turn the check off. Attachments then go to the provider as they are.

## Related

- [Preprocess images](/sdk/how-to/multimodal/preprocess-images/)
- [Multimodal example](/sdk/examples/multimodal/)
- [SDK reference](/sdk/reference/)

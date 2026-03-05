# Multimodal Tool Results Example

Demonstrates tools that return **multimodal content** (text + images) alongside standard JSON results using `mock_parts`.

## What This Shows

- **`mock_parts`** on tool descriptors: tools return both JSON data and binary content (images)
- **`file_path` resolution**: mock parts reference local files that get base64-encoded at execution time
- **Multimodal assertions**: `tool_result_has_media` and `tool_result_media_type` verify rich content in tool results
- **Mock provider with `tool_calls`**: canned LLM responses that trigger tool invocations

## Tools

| Tool | Returns |
|------|---------|
| `generate_chart` | JSON summary + PNG chart image |
| `scan_document` | JSON with extracted text + scanned document image |

## Running

```bash
# From this directory:
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --ci --formats html,json

# Open the report to see media badges on tool results:
open out/report.html
```

## How `mock_parts` Works

In each tool YAML, the `mock_parts` field defines multimodal content returned alongside `mock_result`:

```yaml
mock_result:
  title: "Revenue Chart"
  data_points: 90
mock_parts:
  - type: text
    text: "Revenue Chart — 90 data points"
  - type: image
    media:
      file_path: testdata/sample-chart.png
      mime_type: image/png
```

At execution time, `file_path` references are resolved to base64-encoded inline data. The pipeline propagates these parts through `MessageToolResult.Parts` to the LLM provider, which serializes them in the provider-specific format (Claude content blocks, OpenAI data URIs, Gemini inlineData).

## Assertions

The scenarios use two multimodal-specific assertions:

- **`tool_result_has_media`** — verifies the tool result contains non-text content
- **`tool_result_media_type`** — verifies the media type matches (e.g., `image`, `audio`, `video`)

See the [Assertions Reference](https://promptkit.altairalabs.ai/arena/reference/assertions/) for full details.

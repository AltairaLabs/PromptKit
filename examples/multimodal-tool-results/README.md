# Multimodal Tool Results Example

This example demonstrates how tools can return multimodal content (text + images)
using `mock_parts` in tool definitions, and how to write assertions that verify
media is present in tool results.

## What it demonstrates

1. **`mock_parts` in tool definitions** - The `generate_chart` tool is configured
   with `mock_parts` that return both a text description and an image file (PNG).

2. **Mock LLM responses with `tool_calls`** - The mock provider triggers the
   `generate_chart` tool call on the first turn, simulating an LLM that decides
   to invoke a tool.

3. **Multimodal assertions** - The scenario uses three assertion types to verify
   tool results contain the expected media:
   - `tools_called` - verifies the tool was invoked
   - `tool_result_has_media` - verifies the tool result contains image media
   - `tool_result_media_type` - verifies the MIME type is `image/png`
   - `content_includes` - verifies the follow-up response describes the chart

## Running the example

Build PromptArena (if not already built):

```bash
make build-arena
```

Run from this directory:

```bash
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --ci --formats html,json
```

Open the report:

```bash
open out/report.html
```

## File structure

```
multimodal-tool-results/
  config.arena.yaml              # Arena configuration
  mock-responses.yaml            # Mock LLM responses (triggers tool calls)
  prompts/
    data-viz.yaml                # Prompt config for the data visualization assistant
  providers/
    mock-provider.yaml           # Mock provider referencing mock-responses.yaml
  scenarios/
    chart-generation.scenario.yaml  # Scenario with multimodal assertions
  testdata/
    sample-chart.png             # Small test PNG used by the mock tool
  tools/
    generate-chart.tool.yaml     # Tool with mock_parts (text + image)
```

## Key concepts

### mock_parts

The `mock_parts` field on a tool definition lets you specify multimodal content
that the mock executor returns alongside the standard `mock_result` JSON. Each
part has a `type` (text, image, audio, video) and either inline data or a
`file_path` reference that gets resolved to base64 at execution time.

### Multimodal assertions

- `tool_result_has_media` checks that a tool result contains a `ContentPart` of
  the specified `media_type` (e.g., "image", "audio").
- `tool_result_media_type` checks the MIME type of media in a tool result
  (e.g., "image/png", "audio/wav").

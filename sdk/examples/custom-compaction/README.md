# Custom Compaction Example

Customize how conversation context is compacted as it grows.

## What it shows

Three extensibility patterns:

1. **Custom `CompactionRule`** — produce domain-specific summaries (e.g. for search results) instead of the generic preview+bytes format.
2. **Built-in `CollapsePairs`** — collapse superseded assistant+tool pairs when the same tool is called again with the same arguments.
3. **Custom `CompactionStrategy`** — a complete replacement for the default compactor (here, wrapping it with logging).

## Running

```bash
cd sdk/examples/custom-compaction
go run .
```

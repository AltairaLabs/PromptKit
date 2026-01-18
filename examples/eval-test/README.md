# Eval Configuration Test Example

This example demonstrates the new Eval configuration type for evaluating saved conversations.

## Overview

The Eval config type allows you to:
- Load saved conversations from recording files
- Specify judge targets for LLM-based assertions
- Define assertions to validate the conversation
- Categorize evaluations with tags

## Files

- `config.arena.yaml` - Main arena configuration with eval reference
- `evals/basic-eval.eval.yaml` - Eval configuration for testing
- `providers/replay.provider.yaml` - Replay provider for deterministic playback

## Usage

```bash
# From repo root, validate the eval config
PROMPTKIT_SCHEMA_SOURCE=local ./bin/promptarena validate examples/eval-test/config.arena.yaml

# Validate the eval file directly
PROMPTKIT_SCHEMA_SOURCE=local ./bin/promptarena validate --type eval examples/eval-test/evals/basic-eval.eval.yaml
```

**Note:** Use `PROMPTKIT_SCHEMA_SOURCE=local` during development until schemas are published to the hosted location.

## Eval Configuration Format

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Eval
metadata:
  name: basic-eval
spec:
  id: basic-eval-test
  description: Test evaluation of a saved conversation
  recording:
    path: path/to/recording.json
    type: session  # session, arena_output, transcript, or generic
  judge_targets:
    default:
      type: openai
      model: gpt-4o
      id: gpt-4o-judge
  assertions:
    - type: llm_judge
      params:
        judge: default
        criteria: "Your evaluation criteria here"
        expected: pass
  tags:
    - test
    - category
  mode: instant  # instant, realtime, or accelerated
```

## Recording Types

- `session`: Session recording JSON (`.recording.json`)
- `arena_output`: Arena output JSON from previous runs
- `transcript`: Transcript YAML (`.transcript.yaml`)
- `generic`: Generic chat export JSON

## Integration with Issue #215

This implementation provides the foundation for:
- **Issue #215**: Eval config type support ✅
- **Issue #216**: Recording adapter system (future)
- **Issue #217**: Replay provider enhancements (future)

## Next Steps

Future enhancements will add:
1. Recording adapter registry for multiple formats
2. Metadata propagation from recordings to judges
3. Multimodal content pass-through in replay

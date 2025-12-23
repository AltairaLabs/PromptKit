# Session Replay Example

This example demonstrates using the **replay provider** to run scenarios against pre-recorded sessions without making any API calls.

## What's Included

- `recordings/geography-session.recording.json` - A pre-recorded Q&A session about Paris
- `providers/replay.provider.yaml` - Replay provider configuration
- `scenarios/geography.scenario.yaml` - Scenario with assertions to verify responses
- `config.arena.yaml` - Arena configuration

## Running the Example

From the repo root:

```bash
# Build promptarena (if not already built)
go build -o /tmp/promptarena ./tools/arena/cmd/promptarena

# Run the replay scenario
/tmp/promptarena run --config examples/session-replay/config.arena.yaml
```

Or with `go run`:

```bash
go run ./tools/arena/cmd/promptarena run \
  --config examples/session-replay/config.arena.yaml
```

**Note**: Run from the repo root since file paths are relative to the working directory.

## Expected Output

The scenario runs against the recorded session and verifies assertions:

```
Running scenario: geography-replay with provider: replay-geography

Turn 1: What is the capital of France?
  Response: The capital of France is Paris...
  Assertions: PASS (content includes "Paris", "capital")

Turn 2: What is the population of Paris?
  Response: The city of Paris proper has a population of approximately 2.1 million...
  Assertions: PASS (content includes "million", "population")

Turn 3: What river flows through Paris?
  Response: The Seine River flows through Paris...
  Assertions: PASS (content includes "Seine")

Result: ALL ASSERTIONS PASSED
```

## Use Cases

1. **Deterministic Testing**: Verify prompt changes don't break expected outputs
2. **CI/CD Integration**: Run tests without API costs or rate limits
3. **Debugging**: Replay problematic sessions to investigate issues
4. **Baseline Comparison**: Compare new model outputs against recorded baselines

## Recording Your Own Sessions

1. Run a scenario with a real provider (e.g., `gemini-flash`)
2. Export the session to a recording file
3. Update the replay provider to use your recording

## Configuration Options

The replay provider supports:

```yaml
additional_config:
  recording: path/to/recording.json  # Required
  timing: instant | realtime | accelerated
  speed: 2.0  # For accelerated mode
  match: turn | content
```

- **timing**: `instant` (default) delivers responses immediately, `realtime` preserves original timing
- **match**: `turn` (default) matches sequentially, `content` matches by user message content

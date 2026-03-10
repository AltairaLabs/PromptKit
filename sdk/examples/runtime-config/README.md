# RuntimeConfig Example

Demonstrates the Polyglot Exec Protocol and RuntimeConfig — declarative SDK setup with external tools, evals, and hooks written in Python.

## What This Shows

- **RuntimeConfig YAML** — one line (`sdk.WithRuntimeConfig("./runtime.yaml")`) replaces 50+ lines of programmatic setup
- **Exec tools** — a Python sentiment analysis tool, bound by name in RuntimeConfig
- **Exec evals** — Python-based tone and quality checks, run via `sdk.Evaluate()`
- **Exec hooks** — a content policy filter (provider/filter) and audit logger (session/observe)
- **Pack stays agnostic** — the pack defines tool schemas and eval types by name; it never references Python, file paths, or credentials

## Files

```
agent.pack.json     ← What the agent does (portable, platform-agnostic)
runtime.yaml        ← How to run it (environment-specific bindings)
main.go             ← SDK usage
tools/
  sentiment.py      ← Exec tool: keyword-based sentiment analysis
evals/
  tone-check.py     ← Exec eval: professional tone scoring
  response-quality.py ← Exec eval: word count, completeness, repetition
hooks/
  content-policy.py ← Provider hook (filter): blocks prohibited topics
  audit-logger.py   ← Session hook (observe): logs lifecycle events
```

## Run

```bash
# No API keys needed — uses mock provider
go run .
```

## Expected Output

```
=== Part 1: Conversation with RuntimeConfig ===
Assistant: ...

=== Part 2: Standalone Evals (exec eval handlers) ===
  [PASS] tone-check           score=0.90  Detected tone: professional ...
  [PASS] response-quality     score=1.00  Quality score: 1.00 — no issues found

=== Part 3: Eval a poor response ===
  [PASS] tone-check           score=0.70  Detected tone: professional ...
  [FAIL] response-quality     score=0.00  Quality score: 0.00 — issues: too short ...

=== Part 4: Inline evals with exec handlers ===
  [PASS] custom-tone          score=0.80  Detected tone: professional ...
  [PASS] has-greeting         score=1.00  ...
```

## How It Works

1. The **pack** (`agent.pack.json`) defines a `sentiment_analysis` tool with a JSON schema, and two evals (`tone_check`, `response_quality`) by type name
2. The **RuntimeConfig** (`runtime.yaml`) binds those names to Python scripts
3. At runtime, the SDK spawns Python subprocesses per invocation — JSON on stdin, JSON on stdout
4. The pack never knows whether implementations are Go, Python, HTTP, or anything else

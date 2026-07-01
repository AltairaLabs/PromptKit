# reasoning-test

Demonstrates unified model **reasoning ("thinking")** capture and display
(epic #1527). A Gemini 2.5 thinking model solves a multi-step word problem; its
reasoning is surfaced separately from the spoken answer.

## Run it

Requires a `GEMINI_API_KEY` (or `GOOGLE_API_KEY`). From this directory:

```bash
../../bin/promptarena run --ci --formats html,json
open out/report.html
```

(Build the CLI first with `make build-arena` from the repo root.)

## What to look for

The prompt asks for a **terse** answer on purpose, so the distinction is obvious:

- `content` is a single line, e.g. `ANSWER: 16`.
- `reasoning` holds the model's multi-step thinking — captured **separately**.

Reasoning is a **sibling of content** (`Message.Reasoning`), never mixed into the
answer, exports, or future-turn context.

**The definitive check** — see the two side by side (a populated `reasoning`
proves capture; a short `content` proves it didn't leak into the answer):

```bash
jq '.Messages[] | select(.role=="assistant") | {content, reasoning}' out/*.json
```

Also visible in:
- **HTML report** (`out/report.html`): a collapsible `💭 Reasoning` section,
  rendered *only* when reasoning is present — so its appearance is the proof.
- **TUI** (`../../bin/promptarena run` without `--ci`): a `💭 Reasoning` section
  in the turn detail; interactive/voice sessions stream it live.

If you instead see the step-by-step working *inside* `content`, the model ignored
the terse instruction — that's the answer text, not the captured reasoning. The
`reasoning` field is the ground truth.

## How reasoning is enabled

Gemini 2.5 thinking models return thought summaries only when asked. See
`providers/gemini-25-flash.provider.yaml`:

```yaml
additional_config:
  include_thoughts: true   # return thought summaries
  thinking_budget: 1024    # cap on reasoning tokens (count toward max_tokens)
```

Reasoning is **not persisted** to the conversation store by default; the reports
above are built from the live run, so they show it regardless.

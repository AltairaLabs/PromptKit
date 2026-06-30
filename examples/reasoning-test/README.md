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

Reasoning is captured as a **sibling of content** (`Message.Reasoning`), so it is
never mixed into the assistant's answer, exports, or future-turn context.

- **HTML report** (`out/report.html`): each assistant turn has a collapsible
  `💭 Reasoning` section, separate from the answer.
- **JSON report** (`out/*.json`): the assistant message carries a `reasoning`
  field alongside `content`.
- **TUI** (`../../bin/promptarena run` without `--ci`): the conversation detail
  view shows a `💭 Reasoning` section; interactive/voice sessions stream it live.

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

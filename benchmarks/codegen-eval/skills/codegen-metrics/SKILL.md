---
name: codegen-metrics
description: Soft-metric capture scripts for codegen evals. Emits structured JSON the tool_exec eval handler can capture as EvalResult.Value for jq-based reporting.
metadata:
  tags: "metrics, reporting"
---

# Codegen Metrics

Host-side measurement scripts for the codegen-eval methodology. These
are **never invoked by the agent** — they run as session-level evals
via `tool_exec` calling `Bash bash /skills/codegen-metrics/scripts/<name>.sh`,
and the JSON they emit on stdout flows into `EvalResult.Value`.

## Scripts

### `diff_stats.sh`

Emits the line/file deltas of the agent's edits in `/workspace`,
using `git diff --shortstat HEAD` against the seeded baseline. Output:

```json
{
  "loc_added": 12,
  "loc_removed": 3,
  "files_changed": 2
}
```

When the workspace isn't a git repo (e.g. the agent skipped a `git
init` that was part of the seed), the script emits zeros rather than
failing — null metrics distort medians less than missing rows.

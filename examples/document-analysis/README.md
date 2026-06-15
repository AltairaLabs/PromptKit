# Document Analysis — Workflow Composition Example (RFC 0010)

This example demonstrates **workflow composition** (RFC 0010) using PromptArena.
It shows how a multi-step document analysis pipeline — classify, branch, extract,
parallel metadata, synthesize — is authored as a `compositions:` block and tested
with composition-specific assertions.

> **IMPORTANT — requires `PROMPTKIT_SCHEMA_SOURCE=local`**
>
> The `compositions:` and `orchestration: composition` schema fields have not yet
> been published in a PromptKit release. Running this example against the remote
> schemas (the default) will produce a schema validation error. Until the next
> release ships, always set:
>
> ```bash
> export PROMPTKIT_SCHEMA_SOURCE=local
> ```
>
> or prefix the run command with `env PROMPTKIT_SCHEMA_SOURCE=local` as shown below.

## What it demonstrates

- A single-state terminal workflow whose state uses `orchestration: composition`
- A `compositions:` block with all five RFC 0010 step kinds:
  - `prompt` — classify document type
  - `branch` — route to paper vs general extractor based on classify output
  - `prompt` — extract_paper / extract_general (only one runs per document)
  - `parallel` — generate summary and keywords concurrently, reduce with `barrier`
  - `agent` — synthesize a final analysis (with `termination.max_steps`)
- Mock provider with a `steps:` map keyed by composition step ID
- All four composition assertion types: `composition_branch_taken`,
  `composition_parallel_complete`, `composition_step_output`, `composition_output`

## Directory structure

```
document-analysis/
├── config.arena.yaml               # Arena config with workflow + compositions blocks
├── mock-responses.yaml             # Step-keyed mock responses
├── prompts/
│   ├── classify_doc.yaml           # Classify: research_paper | general
│   ├── extract_paper.yaml          # Extract paper title, authors, abstract
│   ├── extract_general.yaml        # Extract summary and key points
│   ├── meta_summary.yaml           # Generate concise summary
│   ├── meta_keywords.yaml          # Extract keywords
│   └── synthesize_doc.yaml         # Synthesize final analysis
├── providers/
│   └── mock-provider.yaml          # Mock provider pointing to mock-responses.yaml
└── scenarios/
    └── research-paper.scenario.yaml  # Single-turn scenario with composition assertions
```

## Run

Build the binary (if not already built):

```bash
make -C /path/to/promptkit build-arena
```

Run the example:

```bash
env PROMPTKIT_SCHEMA_SOURCE=local promptarena run --ci --formats json
```

Or from the promptkit root:

```bash
env -C examples/document-analysis PROMPTKIT_SCHEMA_SOURCE=local \
  bin/promptarena run --ci --formats json
```

Successful output exits with code 0 and all four `composition_*` assertions pass.

## Composition DAG

```
input
  └─ classify (prompt)
       └─ route (branch)
            ├─ [type=research_paper] → extract_paper (prompt)
            └─ [else]               → extract_general (prompt)
  └─ meta (parallel)
       ├─ meta_summary (prompt)
       └─ meta_keywords (prompt)
       reduce: barrier → metadata
  └─ synthesize (agent, max_steps: 3)   ← composition output
```

## Assertions explained

| Assertion type | What it checks |
|---|---|
| `composition_branch_taken` | The `route` branch picked `extract_paper` |
| `composition_parallel_complete` | The `meta` parallel step completed |
| `composition_step_output` | The `classify` step output contains `research_paper` |
| `composition_output` | The final synthesis output contains `deep learning` |

## Schema status

This example requires `PROMPTKIT_SCHEMA_SOURCE=local` because the `compositions`
field and `orchestration: composition` state field have not yet been published to
`https://promptkit.altairalabs.ai/schemas/v1alpha1/`. Once the next PromptKit
release lands, this flag can be dropped and the example will validate against the
remote schemas automatically.

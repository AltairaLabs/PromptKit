---
title: Workflow Compositions
description: Declarative step-graphs that replace LLM-driven orchestration for a workflow state
sidebar:
  order: 9
---

A **composition** is a declarative step-graph the runtime executes for a workflow state, instead of an LLM-driven turn. Where a normal workflow state calls a language model and waits for it to call the `workflow__transition` tool, a composition state runs a structured pipeline of `prompt`, `agent`, `tool`, `branch`, and `parallel` steps and produces a validated structured output — no free-form conversation needed.

Compositions implement [RFC 0010](https://promptpack.org/docs/rfcs/workflow-composition).

---

## Why Compositions?

Some workflow states are purely procedural: classify a document, extract fields, call a tool, decide which path to take, fan out to several sub-tasks and merge the results. Expressing that as a multi-turn chat puts the burden of orchestration on the LLM. Compositions move that control flow into a deterministic, testable declaration.

| | LLM-driven state | Composition state |
|---|---|---|
| **Orchestration** | LLM decides tool order | Explicit step graph |
| **Output** | Free-form assistant message | Validated structured JSON |
| **Testing** | Assert on tool calls | Assert on step outputs, branch taken, final output |
| **Latency** | Multiple round-trips | Predictable step sequence |

---

## How a Composition State Works

A composition state replaces the `ProviderStage` (the LLM + tool loop) with a `CompositionStage`. The stage reads the turn's user message as the composition's input, walks the step DAG, and emits the validated structured output as the turn response.

```
Normal workflow state:  input → PromptAssembly → ProviderStage (LLM tool loop) → output
Composition state:      input → CompositionStage (step DAG) → output
```

The rest of the pipeline — state store, events, eval hooks, guardrails — remains unchanged.

---

## Step Kinds

A composition is made up of steps. Each step has a `kind` that determines how it executes.

### `prompt`

Runs a sub-pipeline (prompt assembly + one LLM call) for the named `prompt_task`. No tool loop. Returns the LLM response as the step output.

```yaml
- id: classify
  kind: prompt
  prompt_task: doc_classifier
  input: ${input.text}
  output_schema: schemas/doctype.json
```

### `agent`

Runs a sub-pipeline with tools and an LLM tool loop. The loop terminates at `max_steps` or when `tool_called` fires.

```yaml
- id: synthesize
  kind: agent
  prompt_task: doc_analyzer
  input: ${meta.output.metadata}
  tools: [doc.section_lookup, ref.search]
  termination: { max_steps: 10 }
  output_schema: schemas/analysis.json
```

Tool access for an `agent` step is the intersection of the prompt template's allowed tools and the step's `tools:` list.

### `tool`

Invokes a tool from the registry directly — no LLM involved.

```yaml
- id: parse_meta
  kind: tool
  tool: doc.parse_structure
  args:
    content: ${input.text}
```

### `branch`

Evaluates a predicate and routes to one of two step IDs (`then` / `else`). `else` is optional: if omitted and the predicate is false, execution skips to the next sequential step.

```yaml
- id: route
  kind: branch
  predicate:
    path: ${classify.output.type}
    op: equals
    value: research_paper
  then: extract_paper
  else: extract_general
```

Predicate operators: `equals`, `not_equals`, `in`, `not_in`, `less_than`, `less_than_or_equals`, `greater_than`, `greater_than_or_equals`, `exists`. Compound predicates use `all_of`, `any_of`, or `not`.

### `parallel`

Runs two or more branch steps concurrently and merges their outputs.

```yaml
- id: meta
  kind: parallel
  branches:
    - { id: structure,  kind: tool, tool: doc.parse_structure,   args: { content: ${input.text} } }
    - { id: citations,  kind: tool, tool: doc.extract_citations,  args: { content: ${input.text} } }
  reduce:
    strategy: barrier
    into: metadata
```

`reduce.strategy` values: `barrier` (wait for all, merge), `append` (concatenate outputs), `replace` (last write wins). `reduce.into` names the key under which the merged result is available to downstream steps as `${meta.output.metadata}`.

---

## Inputs and References

Steps receive data through the `input` field, which can be a `${...}` reference or a literal object mixing references and constants.

| Expression | Resolves to |
|---|---|
| `${input.X}` | Field `X` of the composition's input |
| `${stepID.output.X}` | Field `X` of step `stepID`'s output |
| `${meta.output.metadata}` | The merged output of a `parallel` step named `meta` |

---

## Step Modifiers

Both `prompt` and `agent` steps accept a `modifiers` block.

```yaml
modifiers:
  retry:
    max_attempts: 3
  eval:
    - analysis_quality   # references a pack-level evals: key
```

- `retry.max_attempts` — retries the step on transient failure.
- `eval` — runs the listed pack eval keys against the step's output for observability. These are **pure observability signals** — they are captured in the eval report but never gate control flow.

---

## Authoring in PromptArena

### Pack structure

`compositions:` is a top-level block, sibling of `workflow:` under `spec:`. A workflow state opts into a composition via two new fields: `orchestration: composition` and `composition: <name>`. The `prompt_task` field is omitted on composition states.

```yaml
spec:
  prompt_configs:
    - { id: doc_classifier, file: prompts/doc_classifier.yaml }
    - { id: doc_analyzer,   file: prompts/doc_analyzer.yaml }
  tools:
    - { id: doc.parse_structure,   file: tools/parse_structure.yaml }
    - { id: doc.extract_citations, file: tools/extract_citations.yaml }
    - { id: doc.section_lookup,    file: tools/section_lookup.yaml }
    - { id: ref.search,            file: tools/ref_search.yaml }

  workflow:
    version: 1
    entry: analyze
    states:
      analyze:
        orchestration: composition   # opt into composition mode
        composition: analyze_document
        terminal: true               # prompt_task omitted for composition states

  compositions:
    analyze_document:
      version: 1
      input_schema: schemas/document.json
      output_schema: schemas/analysis.json
      output: synthesize             # final step; defaults to last step if omitted
      steps:
        - id: classify
          kind: prompt
          prompt_task: doc_classifier
          input: ${input.text}
          output_schema: schemas/doctype.json

        - id: route
          kind: branch
          predicate:
            path: ${classify.output.type}
            op: equals
            value: research_paper
          then: extract_paper
          else: extract_general

        - { id: extract_paper,   kind: prompt, prompt_task: research_paper_extractor, input: ${input.text} }
        - { id: extract_general, kind: prompt, prompt_task: general_doc_extractor,    input: ${input.text} }

        - id: meta
          kind: parallel
          branches:
            - { id: structure,  kind: tool, tool: doc.parse_structure,   args: { content: ${input.text} } }
            - { id: citations,  kind: tool, tool: doc.extract_citations,  args: { content: ${input.text} } }
          reduce: { strategy: barrier, into: metadata }

        - id: synthesize
          kind: agent
          prompt_task: doc_analyzer
          input: ${meta.output.metadata}
          tools: [doc.section_lookup, ref.search]
          termination: { max_steps: 10 }
          output_schema: schemas/analysis.json
          modifiers:
            eval: [analysis_quality]
```

### Validation rules

A step's `composition` reference must resolve — an unresolved reference is a hard error. A composition that no state references produces a warning (like an unused prompt). `depends_on` is required on steps that need to join after a `branch` or `parallel`.

---

## SDK Usage

### Embedded mode

When `Send()` lands on a composition state, the runtime runs the `CompositionStage` automatically. Read the output via `resp.CompositionOutput()`.

```go
conv, err := sdk.OpenWorkflow("./pack.json",
    sdk.WithProvider(myProvider),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

resp, err := conv.Send(ctx, `{"text": "Analyze this document..."}`)
if err != nil {
    log.Fatal(err)
}

// resp.CompositionOutput() returns the composition's validated JSON output.
fmt.Println(string(resp.CompositionOutput()))
```

### Function mode

`OpenComposition` runs a named composition directly — no conversational state, no workflow state machine boilerplate.

```go
conv, err := sdk.OpenComposition("./pack.json", "analyze_document",
    sdk.WithProvider(myProvider),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

// Register tool handlers before Send.
conv.OnTool("doc.parse_structure", func(args map[string]any) (any, error) {
    // ... call your real implementation
    return result, nil
})

resp, err := conv.Send(ctx, `{"text": "Analyze this document..."}`)
if err != nil {
    log.Fatal(err)
}

fmt.Println(string(resp.CompositionOutput()))
```

---

## Testing Compositions in Arena

### Mock responses: per-step responses

The mock provider looks up responses by composition step ID. Add a `steps:` map alongside `turns:` in your scenario's entry in `mock-responses.yaml`, keyed by step `id`. `agent` steps support `tool_calls` for simulating the LLM's tool loop.

```yaml
scenarios:
  my-analysis-scenario:
    steps:
      classify:
        response: '{"type": "research_paper"}'
      extract_paper:
        response: '{"title": "...", "abstract": "..."}'
      synthesize:
        response: '{"summary": "...", "key_findings": [...]}'
        tool_calls:
          - name: doc.section_lookup
            arguments:
              section: introduction
```

When a step ID is present in `steps:`, it takes priority over any `turns:` entry. Falls back to the scenario default when no step entry matches.

### Composition assertions

Four assertion handlers let you verify composition behavior at each layer. They are **pure eval primitives** — threshold judgment lives on the `type: assertion` wrapper (or use directly in `assertions:` where the default passes on a truthy result). See [Composition Checks](/reference/checks/#composition-checks) for the full parameter reference.

**Assert a step's output:**

```yaml
assertions:
  - type: composition_step_output
    params:
      step: classify
      contains: '"type": "research_paper"'
```

**Assert a branch was taken:**

```yaml
assertions:
  - type: composition_branch_taken
    params:
      branch: route
      expected: extract_paper
```

**Assert a parallel step completed:**

```yaml
assertions:
  - type: composition_parallel_complete
    params:
      parallel: meta
```

**Assert the final composition output:**

```yaml
conversation_assertions:
  - type: composition_output
    params:
      contains: '"key_findings"'
```

---

## What Is Out of Scope (v1)

The following are reserved for future RFC revisions and are not supported in the current release: loops (`foreach`, `map`, `while`), step kinds `judge`, `refine`, `subflow`, `speculate`, `pause`, step-level `timeout`/`on_error`/`budget` modifiers, durable/persistent step execution, A2A delegation from steps, and per-composition provider selection in the pack schema (use `sdk.WithCompositionProvider` at the SDK level or the opaque `engine` block).

---

## Related Documentation

- [RFC 0010 — Workflow Composition](https://promptpack.org/docs/rfcs/workflow-composition) — the canonical spec
- [State Management](/concepts/state-management/) — workflow state machines and persistence
- [Workflow Regression Testing](/arena/how-to/workflow-regression/) — CI gate patterns for workflow scenarios
- [Composition Checks](/reference/checks/#composition-checks) — `composition_step_output`, `composition_branch_taken`, `composition_parallel_complete`, `composition_output`

---
title: Templates
description: How a pack's system template substitutes variables, which syntax the runtime supports, and how templates fail.
sidebar:
  order: 2
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/packspec/types.go
    - runtime/pipeline/stage/stages_utilities.go
    - runtime/prompt/pack.go
    - runtime/prompt/registry.go
    - runtime/prompt/schema/promptpack.schema.json
    - runtime/template/renderer.go
    - sdk/conversation.go
    - sdk/options.go
    - sdk/sdk.go
---

A template is a prompt with placeholders for values that change. The runtime fills the placeholders on every turn, so one prompt serves many users, tenants, and situations.

## What a template is

Without a template, you assemble the system prompt by hand:

```go
prompt := "You are a support agent for " + company + ". Help the user with: " + question
```

With a template, the pack holds the text and the runtime supplies the values:

```json
{
  "system_template": "You are a {{role}} for {{company}}. Keep responses under {{max_words}} words."
}
```

Templates give you one place to change a prompt, prompts that you can version and review as files, and prompts that you can test without running application code.

## Placeholder syntax

A placeholder is a variable name between double braces: `{{name}}`. The runtime replaces each placeholder with the variable's value by exact string match, so `{{ name }}` with spaces is not a placeholder for `name`.

The runtime performs plain substitution. It has no conditionals, loops, filters, default syntax inside the template, or template inheritance. Compute anything dynamic in Go and pass the result as a variable. The pack's `template_engine` block records the engine version and syntax, and the runtime does not read it to change how it substitutes.

Variable values can contain other placeholders. The renderer repeats substitution for up to 10 passes, so `greeting` set to `Hello {{name}}` resolves once `name` has a value.

## Declaring variables

A prompt in a pack lists its variables in `variables`:

```json
{
  "prompts": {
    "support": {
      "id": "support",
      "name": "Support agent",
      "version": "1.0.0",
      "system_template": "You are a {{role}} for {{company}}.",
      "variables": [
        { "name": "company", "type": "string", "required": true },
        { "name": "role", "type": "string", "required": false, "default": "support agent" }
      ]
    }
  }
}
```

| Field | Meaning |
|-------|---------|
| `name` | Variable name used in the template. |
| `type` | Declared data type. |
| `required` | When `true`, the turn fails unless a non-empty value is supplied. |
| `default` | Value used when none is supplied. A default cannot be set on a required variable, and the runtime applies string defaults. |

## Where values come from

Values come from pack defaults, `sdk.WithVariables`, variable providers, `SetVar`, and per-send input, with later sources winning. [How Variables Resolve](/sdk/explanation/variables/) lists every source and its precedence.

```go
conv, _ := sdk.Open("./support.pack.json", "support",
    sdk.WithVariables(map[string]string{"company": "TechCorp"}),
)
conv.SetVar("role", "billing specialist")
```

Variables apply to the system template only. Message text is sent as written, so a `{{name}}` in a user message stays a placeholder.

## Rendering without a pack

The `template` package exposes the renderer the pipeline uses:

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/template"

r := template.NewRenderer()
text, err := r.Render(
    "You are a {{role}} for {{company}}.",
    map[string]string{"role": "support agent", "company": "TechCorp"},
)
```

`Render` returns an error when any placeholder remains unresolved. `RenderDetailed` also reports which variables were used and unused, and how many passes ran.

## Authoring templates as files

PromptArena and PackC manage prompts as YAML `PromptConfig` files and compile them into a pack. The template lives under `spec.system_template`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: support
  version: v1.0.0
  system_template: |
    You are a customer support agent for {{company}}.
    Answer clearly, stay professional, and escalate complex issues.
  variables:
    - name: company
      type: string
      required: true
```

Compile the prompts into a pack with PackC:

```bash
packc compile --config arena.yaml --output support.pack.json --id support
```

Load the pack in Go with `sdk.Open`. The PromptArena documentation covers the arena configuration that points at your prompt files.

## Failure modes

A placeholder with no value fails the turn before the model runs: `Send` returns an error for which `errors.Is(err, stage.ErrTemplateUnresolved)` holds. The error lists the variable names available and never their values:

```text
stage 'template' (transform) failed: system prompt has unresolved variables:
support: unresolved template placeholders: [{{company}}] (variables available: role)
```

| Symptom | Cause | Fix |
|---------|-------|-----|
| Unresolved placeholder error | No source supplied the variable | Supply it, or declare an optional variable with a `default`. |
| `missing required variables` error | A `required` variable has no value or an empty value | Pass a non-empty value. |
| Literal `{{name}}` in a user message | Variables do not apply to message text | Interpolate message text in Go before `Send`. |

## Related

- [Prompts](/concepts/prompts/) covers prompt design.
- [How Variables Resolve](/sdk/explanation/variables/) covers variable sources and precedence.
- [PackC documentation](https://promptarena.altairalabs.ai/packc/) covers pack compilation.

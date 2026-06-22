---
id: agent-personality
title: Capture the agent's personality from the user
summary: Personality lives in the system_template (identity, tone, guidelines) plus parameters.temperature. The user will have an opinion — elicit it, don't invent it.
tags: [prompt, personality, authoring]
---

An agent's personality is not a separate config field — it lives in the `PromptConfig`'s
`system_template` (its identity, tone, and guidelines) and is reinforced by
`parameters.temperature`. The user will have an opinion on how the agent should come
across, so **ask; don't invent a voice**.

Elicit, then bake in:

- **Identity / role** — who is the agent, for whom? ("a support agent for TechCo")
- **Tone** — professional, empathetic, playful, terse, formal? Often more than one.
- **Verbosity** — concise answers, or thorough and step-by-step?
- **Hard dos & don'ts** — always greet; never speculate on pricing; escalate on X.

Structure the `system_template` so the personality is explicit and testable:

```yaml
spec:
  system_template: |
    You are a support agent for TechCo, a software company.

    Tone: professional, empathetic, solution-focused.

    Guidelines:
    - Greet the customer warmly and confirm their issue.
    - Give clear, step-by-step instructions.
    - Never guess; if unsure, say so and offer to escalate.
  parameters:
    temperature: 0.6   # lower = consistent/factual, higher = creative/varied
```

Tone and guideline lines are also things you can assert on later (e.g. an `llm_judge`
that scores "stayed in persona"), so write them as concrete behaviors, not vibes.

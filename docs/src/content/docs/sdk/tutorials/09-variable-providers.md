---
title: 'Tutorial 9: Variable providers'
sidebar:
  order: 9
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/pipeline/stage/stages_utilities.go
    - runtime/statestore/memory.go
    - runtime/statestore/types.go
    - runtime/variables/chain.go
    - runtime/variables/provider.go
    - runtime/variables/request.go
    - runtime/variables/state.go
    - runtime/variables/time.go
    - sdk/json_input.go
    - sdk/options.go
---
## What we will build

In this tutorial we build a support agent whose system prompt fills itself in on every `Send()`: today's date from the built-in time provider, the customer's plan from conversation state, and an FAQ answer from a provider we write. A variable provider implements `variables.Provider`, and `sdk.WithVariableProvider` registers it; the pipeline calls every registered provider on each `Send()`, before it renders the prompt.

## Prerequisites

Complete [Tutorial 1: First conversation](/sdk/tutorials/01-first-conversation/), and set your provider API key as in that tutorial.

## Step 1: Inject today's date

Create `support.pack.json`. Its system template uses two variables the time provider supplies:

```json
{
  "id": "support",
  "name": "Support",
  "version": "1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "support": {
      "id": "support",
      "name": "Support Agent",
      "version": "1.0.0",
      "system_template": "You are a support agent for Acme. Today is {{current_weekday}}, {{current_date}}."
    }
  }
}
```

Create `main.go`. `variables.NewTimeProvider()` supplies `current_time`, `current_date`, `current_year`, `current_month`, `current_weekday` and `current_hour`:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/v2/variables"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    ctx := context.Background()

    conv, err := sdk.Open("./support.pack.json", "support",
        sdk.WithVariableProvider(variables.NewTimeProvider()),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    resp, err := conv.Send(ctx, "What day is it today?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Text())
}
```

Run it:

```bash
go run .
```

The system prompt the model receives reads, for example, `Today is Thursday, 2026-11-05.`, so the reply typically names today's weekday and date:

```text
Today is Thursday, November 5, 2026.
```

## Step 2: Read the plan from conversation state

`variables.NewStateProvider` turns the metadata saved in a conversation's state into variables. Add the plan to the end of the system template:

```json
"system_template": "You are a support agent for Acme. Today is {{current_weekday}}, {{current_date}}. The customer is on the {{plan}} plan."
```

In `main.go`, add `"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"` to the imports, and save the customer's plan before `sdk.Open`:

```go
store := statestore.NewMemoryStore()
conversationID := "customer-42"
err := store.Save(ctx, &statestore.ConversationState{
    ID:       conversationID,
    Metadata: map[string]any{"plan": "premium"},
})
if err != nil {
    log.Fatal(err)
}
```

Open the conversation on that store and that conversation ID, with the state provider after the time provider:

```go
conv, err := sdk.Open("./support.pack.json", "support",
    sdk.WithVariableProvider(variables.NewTimeProvider()),
    sdk.WithStateStore(store),
    sdk.WithConversationID(conversationID),
    sdk.WithVariableProvider(variables.NewStateProvider(store, conversationID)),
)
```

Change the message to `"What plan am I on?"` and run the program again. `{{plan}}` renders as `premium`, so the reply typically says:

```text
You're on the premium plan.
```

## Step 3: Write an FAQ provider

A provider has two methods, `Name()` and `Provide(ctx)`. This one looks up an answer for a topic the caller passes with each `Send()`; `variables.RequestVars(ctx)` reads those per-send values. Create `faq.go`:

```go
package main

import (
    "context"

    "github.com/AltairaLabs/PromptKit/runtime/v2/variables"
)

// FAQProvider looks up an FAQ answer for the topic passed with each Send.
type FAQProvider struct {
    answers map[string]string
}

func (p *FAQProvider) Name() string {
    return "faq"
}

func (p *FAQProvider) Provide(ctx context.Context) (map[string]string, error) {
    topic := variables.RequestVars(ctx)["topic"]
    answer, ok := p.answers[topic]
    if !ok {
        answer = "No FAQ entry matches this question."
    }
    return map[string]string{"faq_answer": answer}, nil
}
```

`Provide` always returns `faq_answer`, so the template variable resolves even when no topic matches. Add the answer to the end of the system template:

```json
"system_template": "You are a support agent for Acme. Today is {{current_weekday}}, {{current_date}}. The customer is on the {{plan}} plan. Answer from this FAQ entry: {{faq_answer}}"
```

In `main.go`, create the provider before `sdk.Open`:

```go
faq := &FAQProvider{answers: map[string]string{
    "password": "Open Settings > Security and choose Reset password. The reset link is valid for 30 minutes.",
}}
```

Add `sdk.WithVariableProvider(faq),` as the last option to `sdk.Open`. Then replace the `Send` call so it passes the topic with `sdk.WithJSONInput`:

```go
resp, err := conv.Send(ctx, "How do I reset my password?",
    sdk.WithJSONInput(map[string]any{"topic": "password"}),
)
```

Run the program. The FAQ answer is in the system prompt, so the reply typically repeats its steps:

```text
Go to Settings > Security and choose Reset password. The reset link is valid for 30 minutes.
```

## Step 4: Test the provider

A provider is a plain Go type, so we test it without a conversation. `variables.WithRequestVars` puts per-send values on a context, as `WithJSONInput` does. Create `faq_test.go`:

```go
package main

import (
    "context"
    "testing"

    "github.com/AltairaLabs/PromptKit/runtime/v2/variables"
)

func TestFAQProvider(t *testing.T) {
    p := &FAQProvider{answers: map[string]string{"password": "Open Settings > Security."}}

    ctx := variables.WithRequestVars(context.Background(), map[string]string{"topic": "password"})
    vars, err := p.Provide(ctx)
    if err != nil {
        t.Fatal(err)
    }
    if vars["faq_answer"] != "Open Settings > Security." {
        t.Errorf("faq_answer = %q", vars["faq_answer"])
    }

    vars, _ = p.Provide(context.Background())
    if vars["faq_answer"] != "No FAQ entry matches this question." {
        t.Errorf("faq_answer without a topic = %q", vars["faq_answer"])
    }
}
```

Run it:

```bash
go test
```

The test passes:

```text
PASS
ok  	support	0.312s
```

The module name and the time vary with your setup.

## Next steps

- [Variable providers reference](/runtime/reference/variables/): every built-in provider and its options, including `variables.Chain`
- [Tutorial 4: State management](/sdk/tutorials/04-state-management/): conversation state and `SetVar`
- [SDK examples](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples): complete programs

---
title: 'Tutorial 4: Validation and guardrails'
description: Add input and output guardrails to a stage-pipeline chatbot with guardrail hooks, then write your own.
sidebar:
  order: 4
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/evals/handlers/length.go
    - runtime/evals/normalize.go
    - runtime/hooks/errors.go
    - runtime/hooks/guardrails/adapter.go
    - runtime/hooks/guardrails/factory.go
    - runtime/hooks/guardrails/spec.go
    - runtime/hooks/registry.go
    - runtime/hooks/types.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/prompt/registry.go
    - runtime/types/message.go
---
## What we will build

In this tutorial we build a command-line chatbot whose guardrails block a request before it reaches the model, replace a response that breaks a rule, and reject a response that contains personal data. Each guardrail is a hook on the `ProviderStage`.

```mermaid
flowchart LR
    in["user message"] --> prov["ProviderStage"]
    prov -->|BeforeCall| reg["hooks.Registry"]
    prov -->|AfterCall| reg
    reg --> g["guardrail hooks"]
    prov --> out["reply"]
```

## Prerequisites

- Completed [Tutorial 1](/runtime/tutorials/01-first-pipeline/), with `OPENAI_API_KEY` still set

## Step 1: Add guardrails to a pipeline

In a new directory, create a module as in Tutorial 1, then create `main.go`. `guardrails.NewGuardrailHook` builds a hook from an eval type and its params. A `direction` of `input` checks the user's message before the model call. The default direction checks the response. `hooks.NewRegistry` collects the hooks, and `stage.NewProviderStageWithHooks` runs them around each model call.

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func main() {
	ctx := context.Background()

	provider := openai.NewProvider(
		"openai",
		"gpt-4o-mini",
		"",
		providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
		false,
	)
	defer provider.Close()

	// The direction param makes a guardrail check the user's message
	// before the model call. The default direction checks the response.
	noHacking, err := guardrails.NewGuardrailHook("banned_words", map[string]any{
		"words":     []any{"hack", "exploit"},
		"direction": "input",
	}, guardrails.WithMessage("I can't help with that."))
	if err != nil {
		log.Fatal(err)
	}
	noCredentials, err := guardrails.NewGuardrailHook("banned_words", map[string]any{
		"words": []any{"password", "secret"},
	}, guardrails.WithMessage("I can't share credentials."))
	if err != nil {
		log.Fatal(err)
	}
	shortReplies, err := guardrails.NewGuardrailHook("length", map[string]any{
		"max_characters": 500,
	})
	if err != nil {
		log.Fatal(err)
	}

	registry := hooks.NewRegistry(
		hooks.WithProviderHook(noHacking),
		hooks.WithProviderHook(noCredentials),
		hooks.WithProviderHook(shortReplies),
	)

	pipe, err := stage.NewPipelineBuilder().
		Chain(stage.NewProviderStageWithHooks(provider, nil, nil,
			&stage.ProviderConfig{MaxTokens: 500, Temperature: 0.7}, nil, registry)).
		Build()
	if err != nil {
		log.Fatal(err)
	}
	defer pipe.Shutdown(ctx)

	fmt.Print("Safe chatbot. Type 'exit' to quit.\n\nYou: ")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "exit" {
			break
		}
		if input == "" {
			fmt.Print("You: ")
			continue
		}

		msg := &types.Message{Role: "user", Content: input}
		result, err := pipe.ExecuteSync(ctx, stage.NewMessageElement(msg))
		if err != nil {
			log.Printf("Error: %v", err)
			fmt.Print("\nYou: ")
			continue
		}

		reply := result.Messages[len(result.Messages)-1]
		fmt.Printf("\nBot: %s\n", result.Response.Content)
		for _, v := range reply.Validations {
			if !v.Passed {
				fmt.Printf("[guardrail enforced: %s]\n", v.ValidatorType)
			}
		}
		fmt.Print("\nYou: ")
	}
}
```

## Step 2: Try it

```bash
go run .
```

Send these three messages. The first reply varies. The third reply is replaced when the model's answer uses the word "password", which it typically does:

```text
You: Hello!

Bot: Hi! How can I help you?

You: How do I hack a system?

Bot: I can't help with that.
[guardrail enforced: banned_words]

You: What is a good default password for a database?

Bot: I can't share credentials.
[guardrail enforced: banned_words]
```

The input guardrail blocks the `hack` request before the model call, so it costs no tokens. The output guardrail replaces a response that contains a banned word. Neither returns an error: the pipeline returns the replacement text, and the reply message's `Validations` records which guardrail fired.

Banned-word matching is case-insensitive and matches whole words; the [checks reference](/reference/checks/#content-checks) covers the other match modes. The length guardrail truncates a response longer than `max_characters`.

## Step 3: Add a function guardrail

`guardrails.InputFunc` and `guardrails.OutputFunc` turn a plain function into a guardrail spec, and `Hook` builds the hook. Add this to `main.go` before the registry, and pass `noSSN` with another `hooks.WithProviderHook`:

```go
noSSN, err := guardrails.InputFunc("no-ssn", func(_ context.Context, in *hooks.InputRequest) hooks.Decision {
	if strings.Contains(strings.ToLower(in.UserInput), "social security number") {
		in.Replacement = "I can't help with that request."
		return hooks.Enforced("ssn requested", map[string]any{"validator_type": "no-ssn"})
	}
	return hooks.Allow
}).Hook()
if err != nil {
	log.Fatal(err)
}
```

Setting `in.Replacement` chooses the text the user sees, and the `validator_type` metadata makes the firing appear in `Validations`. Ask "What is my social security number?" and the bot answers `I can't help with that request.` with `[guardrail enforced: no-ssn]`.

## Step 4: Write a custom hook

A check that needs more control implements `hooks.ProviderHook`. Create `pii.go`. This hook rejects responses that contain an email address. It returns `hooks.Deny`, which makes `ExecuteSync` return an error instead of replacement text.

```go
package main

import (
	"context"
	"regexp"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
)

// PIIHook rejects responses that contain an email address.
type PIIHook struct {
	email *regexp.Regexp
}

func NewPIIHook() *PIIHook {
	return &PIIHook{email: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)}
}

func (h *PIIHook) Name() string { return "pii_filter" }

func (h *PIIHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *PIIHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	if h.email.MatchString(resp.Message.Content) {
		return hooks.Deny("response contains an email address")
	}
	return hooks.Allow
}
```

Register it next to the other hooks:

```go
hooks.WithProviderHook(NewPIIHook()),
```

Handle the denial in the loop, before the `err != nil` check, by looking for `*hooks.HookDeniedError`. Add `"errors"` to the imports.

```go
var denied *hooks.HookDeniedError
if errors.As(err, &denied) {
	fmt.Printf("\nBot: [blocked: %s]\n\nYou: ", denied.Reason)
	continue
}
```

Ask the bot to "make up a fake contact card with an email address". The reply typically contains an address, so the hook blocks it:

```text
Bot: [blocked: response contains an email address]
```

## Step 5: Build guardrails from declared validators

`guardrails.CompileValidators` turns validator declarations into hooks, the same way a pack's `validators` list does. Replace the banned-word and length hooks from Step 1 with this, add `"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"` to the imports, and build the registry from the compiled hooks:

```go
declared, err := guardrails.CompileValidators([]prompt.ValidatorConfig{
	{
		Type:    "banned_words",
		Params:  map[string]any{"words": []any{"competitor_name"}, "direction": "input"},
		Message: "I can't discuss that.",
	},
	{
		Type:   "length",
		Params: map[string]any{"max_characters": 500},
	},
})
if err != nil {
	log.Fatal(err)
}

opts := []hooks.Option{hooks.WithProviderHook(noSSN), hooks.WithProviderHook(NewPIIHook())}
for _, h := range declared {
	opts = append(opts, hooks.WithProviderHook(h))
}
registry := hooks.NewRegistry(opts...)
```

A validator checks the response unless its `direction` param is `input`. The `Message` field sets the text the user sees when the validator fires. Ask "Tell me about competitor_name":

```text
Bot: I can't discuss that.
[guardrail enforced: banned_words]
```

## Next steps

- Continue to [Tutorial 5: Production deployment](/runtime/tutorials/05-production-deployment/).
- The [checks reference](/reference/checks/) lists every check type and its parameters.
- The [unified check model](/concepts/validation/) explains how guardrails, assertions and evals relate.
- The [hooks and guardrails reference](/runtime/reference/hooks/) covers the runtime hook API.
- [Handle errors](/runtime/how-to/pipeline/handle-errors/) covers error strategies.

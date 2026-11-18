---
title: Choose the text of a blocked message
description: Replace a guardrail's blocked message with your own text, such as a localized string, on the text path and in realtime voice alike.
sidebar:
  order: 10
---
Choose the text a caller sees and hears when a guardrail blocks a turn, for example to localize it or to include the check's reason.

## Prerequisites

- A conversation with guardrails: pack `validators`, `sdk.WithGuardrail`, or your own provider hooks.

## Step 1: Pass a callback

`sdk.WithBlockedMessage` takes a function that returns the text for a firing:

```go
import (
	"context"

	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func localized(_ context.Context, b sdk.BlockedMessage) string {
	if b.ValidatorID == "no-competitors" {
		return "No podemos hablar de eso."
	}
	return "" // keep the guardrail's message
}

func open() (*sdk.Conversation, error) {
	return sdk.Open("./assistant.pack.json", "chat", sdk.WithBlockedMessage(localized))
}
```

The text you return replaces the blocked turn. The caller receives it as the response, the conversation stores it, and in a realtime voice session the cue stage speaks it. An empty string keeps `b.Text`, and so does a callback that panics. The firing's validation records your text in its `Details` under `blocked_message`. Like a pack's `message`, your text is not judged again by the guardrail that fired, so it can name what the guardrail bans, and the callback is called once per firing. In a realtime session a turn gets one message: when a later guardrail blocks a turn an earlier one already cut (an input guardrail on the caller's transcript after an output guardrail cut the reply), the callback is not called again and the first text stands.

## Step 2: Read the firing

`sdk.BlockedMessage` describes the firing:

| Field | Meaning |
|---|---|
| `ValidatorType` | The check's type, such as `banned_words`. Empty for a hook that names none. |
| `ValidatorID` | The pack validator's `id`, when it declares one. |
| `Direction` | `input` when the caller's turn was blocked, `output` when the reply was. |
| `Reason` | The check's explanation, such as an LLM judge's reasoning. |
| `Text` | The text the guardrail chose: the validator's `message`, else the check's own replacement, else the default blocked message. |
| `TurnIndex` | The 0-based index of the turn the firing belongs to: 0 for the conversation's first turn, on every path. |
| `Caller` | Who the conversation acts for (`sdk.WithCaller`). |
| `ConversationID` | The conversation's ID. |

`Reason` can describe the policy, so the caller never sees it unless your text includes it.

## When it is called

The callback runs wherever a guardrail replaces a turn with a message, with the same inputs on every path:

- Before the call, when an input guardrail blocks the caller's turn (typed or, in realtime voice, spoken).
- When a streaming check (`banned_words`) cuts a reply part-way.
- When a check on the completed reply blocks it.

It runs on `Send`, `Stream`, VAD mode and realtime sessions. A guardrail that truncates a reply, or enforces without changing it, blocks it with no message, so it is not called for that. On `Send`, `Stream` and VAD mode it runs on the turn's goroutine: a slow callback delays the blocked turn. In a realtime session it gets at most the cue stage's on-demand budget (1.5 seconds); then its `ctx` is canceled and `b.Text` is used, so the session's output never waits longer.

## Realtime voice

In a realtime session the cue stage speaks the text you return. The pack's own messages are rendered when the session starts; a text of yours that differs from them is rendered when it is first needed. One not ready within the cue stage's on-demand budget (1.5 seconds) plays the earcon, a short tone, with your text shown. The clip it renders is cached, so the next firing with that text plays it. See [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/) for what the caller hears on a cut.

## Verify

Trigger a guardrail and check the response: `resp.Text()` on `Send`, or the text chunk after the `Interrupted` chunk on a realtime session, is your text. `conv.Messages(ctx)` holds it as the assistant turn, with finish reason `safety`.

## Related

- [Write custom hooks](/sdk/how-to/hooks/custom-hooks/)
- [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/)
- [Configure text-to-speech](/sdk/how-to/voice/configure-tts/)

---
title: Bring Your Own Inference Provider
description: Plug a custom classifier (ONNX, a model server, a cloud API) into the SDK as an inference provider
sidebar:
  order: 21
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/evals/handlers/audio_emotion.go
    - runtime/evals/handlers/judge_required.go
    - runtime/evals/handlers/provider_binding.go
    - runtime/evals/handlers/topic_policy.go
    - runtime/events/types.go
    - runtime/inference/factory.go
    - runtime/inference/instrument.go
    - runtime/inference/provider.go
    - runtime/metrics/collector.go
    - runtime/prompt/schema/promptpack.schema.json
    - runtime/providers/base/factory.go
    - sdk/binding.go
    - sdk/examples/onnx-audio-emotion/audio.go
    - sdk/examples/onnx-audio-emotion/main.go
    - sdk/options.go
    - sdk/provider_keys.go
    - sdk/provider_refs.go
    - sdk/requires.go
---

Run inference-backed checks (`audio_emotion`, `image_moderation`, `text_toxicity`, `topic_policy` and the other classifier-backed types) on your own classifier: a local ONNX model, a hosted model server, a cloud API or Core ML. Every such check calls one interface, `inference.Provider`, so the runtime stays free of native dependencies and your module holds the native code.

## Prerequisites

- A pack with a check that uses an inference provider.
- A classifier you can call from Go.

## 1. Implement the provider

A provider is any value with an `Infer` method:

```go
import (
    "context"

    "github.com/AltairaLabs/PromptKit/runtime/v2/inference"
)

type myClassifier struct{ model string }

func (c *myClassifier) Infer(ctx context.Context, req inference.Request) (inference.Response, error) {
    // Run req.Inputs through the model and return one score per label, highest first.
    return inference.Response{
        Model: c.model,
        Scores: []inference.LabelScore{
            {Label: "angry", Score: 0.9},
            {Label: "neutral", Score: 0.1},
        },
    }, nil
}
```

`inference.Request` carries these fields:

| Field | Meaning |
|---|---|
| `Model` | The check's `model` param |
| `Inputs` | `[]types.Message` under judgment: text, or media parts |
| `Labels` | Candidate answers for a zero-shot model; empty means the model's own labels |
| `Prompt` | Instruction text for backends that take one (`topic_policy` sends one) |
| `Params` | API-level tweaks, such as `"multi_label": true` |

A check reads the score of the label it cares about. `audio_emotion` with `expected_label: "angry"` reads that label's score, and `topic_policy` compares `on-topic` with `off-topic`. `inference.Response.Score(label)` looks a label up case-insensitively.

Audio and image checks send the media as an inline base64 part on the first input message, with the part's MIME type. The check passes the stored bytes unchanged and does not resample. A provider whose model needs 16 kHz mono PCM must decode the input and reject or convert anything else.

## 2. Register it

Register an instance with `WithClassifier`. It does no credential resolution, which makes it the route for in-process classifiers and test doubles:

```go
provider, _ := newMyClassifier("wav2vec2-ser") // implements inference.Provider
conv, _ := sdk.Open("caller.pack.json", "assistant",
    sdk.WithClassifier("emotion-classifier", provider),
)
```

To select the provider from configuration (`type`, `model`, credentials), register a factory once and declare a provider spec:

```go
inference.RegisterFactory("my-onnx", func(spec inference.ProviderSpec) (inference.Provider, error) {
    return newMyClassifier(spec.Model)
})

conv, _ := sdk.Open("caller.pack.json", "assistant",
    sdk.WithInferenceProvider(sdk.ProviderSpec{
        ID: "emotion-classifier", Type: "my-onnx", Model: "wav2vec2-ser",
    }),
)
```

A `type` names one vendor API. Register a new type only for an API no existing type speaks.

## 3. Wire it to a check

A check names its provider by a logical name that the pack declares, never by a model, an endpoint or an ID that only means something in your deployment. The pack asks and you decide what answers.

1. Declare the name in the pack's `requires` block:

   ```json
   {
     "requires": {
       "providers": [
         {
           "key": "emotion-classifier",
           "role": "inference",
           "description": "speech-emotion classifier scoring the caller's audio",
           "required": true
         }
       ]
     }
   }
   ```

2. Point the check at the name with `params.provider`:

   ```json
   {
     "id": "caller_anger",
     "type": "audio_emotion",
     "trigger": "every_turn",
     "params": {
       "model": "wav2vec2-ser",
       "expected_label": "angry",
       "message_role": "user",
       "provider": "emotion-classifier"
     }
   }
   ```

3. Bind the same name when you open the conversation, with `WithClassifier` or `WithInferenceProvider` as in step 2. The ID you register under is the logical name.

To swap the classifier, change the registration. The pack does not change.

## Verify

`sdk.Open()` returns without error once the name resolves. A check in a prompt's `evals` or `validators`, or in a pack-level eval that runs on its turns, whose name is undeclared, unbound or bound to something that cannot do the job makes `Open()` fail with an error that names the check and the key. A required provider you did not bind also fails `Open()`.

Results arrive through the conversation's evals. Observe them with an [eval hook](/sdk/how-to/observability/run-evals/) (`sdk.WithEvalHook`) or a metrics recorder. Calls through a registered provider emit `inference.call.completed` and `inference.call.failed` events and report the `inference_*` metrics.

## Related

- [Run evals](/sdk/how-to/observability/run-evals/)
- [Checks reference](/reference/checks/): parameters for each inference-backed check
- [`onnx-audio-emotion`](https://github.com/AltairaLabs/promptkit/tree/main/sdk/examples/onnx-audio-emotion): an `inference.Provider` backed by an ONNX wav2vec2 model, registered with `WithClassifier` and scored with `audio_emotion`. It runs offline with no token. Its cgo and ONNX dependency lives in that standalone module, so the runtime stays CGO-free.

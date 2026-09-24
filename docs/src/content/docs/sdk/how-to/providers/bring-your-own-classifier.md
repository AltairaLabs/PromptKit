---
title: Bring Your Own Inference Provider
description: Plug a custom classifier (ONNX, a model server, a cloud API) into the SDK as an inference provider
sidebar:
  order: 21
---

Inference-backed checks — `audio_emotion`, `image_moderation`, `text_toxicity`, `topic_policy` and friends — call **one interface**, `inference.Provider`, never a specific backend. That means you can plug in *any* classifier: a local ONNX model, a hosted model server, a cloud API, Core ML, whatever. The runtime stays dependency-light; the native code lives in your module.

## The interface

A provider is any value implementing:

```go
type Provider interface {
    Infer(ctx context.Context, req inference.Request) (inference.Response, error)
}

type Request struct {
    Model  string          // the check's model param
    Inputs []types.Message // the content under judgment: text, or media parts
    Labels []string        // candidate answers for a zero-shot model; empty = the model's own labels
    Prompt string          // instruction text, for backends that take one (topic_policy sends one)
    Params map[string]any  // API-level tweaks, e.g. "multi_label": true
}

type Response struct {
    Scores []inference.LabelScore // {Label string; Score float64}, highest first
    // Model, Usage, Raw …
}
```

A check reads the probability of the label it cares about — `audio_emotion` with `expected_label: "angry"` reads that label's score, and `topic_policy` compares `on-topic` with `off-topic`.

Audio and image checks deliver the media as an inline part on the first input message; audio arrives at the target rate — SER models want **16 kHz mono** — so your provider owns decode/normalize/run, not resampling.

## Registering it

### In-process instance — `WithClassifier`

The direct path. Construct your provider and register it under an id:

```go
provider, _ := newMyClassifier(...)          // implements inference.Provider
conv, _ := sdk.Open("caller.pack.json", "assistant",
    sdk.WithClassifier("my-ser", provider),
)
```

`WithClassifier` does no credential resolution — it's the escape hatch for in-process classifiers and test doubles. Calls through it report the `inference_*` metrics like any other provider.

### Config-driven — `WithInferenceProvider` + a factory

If you'd rather select the provider from config (`type:`/`model:`/credentials), register a factory once and declare a provider spec:

```go
inference.RegisterFactory("my-onnx", func(spec inference.ProviderSpec) (inference.Provider, error) {
    return newMyClassifier(spec.Model, spec.AdditionalConfig)
})

conv, _ := sdk.Open("caller.pack.json", "assistant",
    sdk.WithInferenceProvider(sdk.ProviderSpec{ID: "my-ser", Type: "my-onnx", Model: "…"}),
)
```

A `type` names one API: register a new type only for an API no existing type speaks.

## Wiring it to a check

A check names the provider it needs by a **logical name the pack itself
declares** — never a model, an endpoint, or an id that only means something in
your deployment. The pack asks; you decide what answers.

Declare it:

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

Point the check at it:

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

Bind that name when you open the conversation:

```go
conv, _ := sdk.Open("caller.pack.json", "assistant",
    sdk.WithClassifier("emotion-classifier", provider),
)
```

Swapping providers is a one-line change on your side; the pack does not move.
A check whose name is undeclared, unbound, or bound to something that cannot do
the job fails at `Open()` and says which of the three it is.

Observe results with an [eval hook](/sdk/how-to/observability/run-evals/) (`sdk.WithEvalHook`) or your metrics recorder.

## Worked example

The [`onnx-audio-emotion`](https://github.com/AltairaLabs/promptkit/tree/main/sdk/examples/onnx-audio-emotion) SDK example implements `inference.Provider` with an ONNX wav2vec2 model, registers it via `WithClassifier`, and scores caller audio with `audio_emotion` — offline, no token. Its cgo/ONNX dependency lives entirely in that standalone example module, so the runtime stays CGO-free. Keep native inference providers in the consumer, not the runtime.

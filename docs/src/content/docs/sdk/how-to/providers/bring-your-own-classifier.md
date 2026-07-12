---
title: Bring Your Own Classify Backend
description: Plug a custom classifier (ONNX, a model server, a cloud API) into the SDK via WithClassifier
sidebar:
  order: 21
---

Classify-backed checks — `audio_emotion`, `image_moderation`, `text_toxicity`, and friends — call a **task interface** from `runtime/classify`, never a specific backend. That means you can plug in *any* classifier: a local ONNX model, a hosted model server, a cloud API, Core ML, whatever. The runtime stays dependency-light; the native code lives in your module.

## The task interfaces

A backend is any value implementing one or more of these:

```go
type AudioClassifier interface {
    ClassifyAudio(ctx context.Context, audio []byte, opts AudioOptions) ([]LabelScore, error)
}
type TextClassifier interface {
    ClassifyText(ctx context.Context, text string, opts TextOptions) ([]LabelScore, error)
}
// …ImageClassifier, VideoClassifier, Embedder — same shape.
```

Each returns `[]classify.LabelScore` (`{Label string; Score float64}`), ideally sorted by descending score. A handler looks up the label it cares about — e.g. `audio_emotion` with `expected_label: "angry"` reads that label's score.

Audio backends receive the **raw audio bytes** the caller supplied (plus a MIME hint). By convention the caller delivers audio at the target rate — SER models want **16 kHz mono** — so your backend owns decode/normalize/run, not resampling.

## Registering it

### In-process instance — `WithClassifier`

The direct path. Construct your backend and register it under an id:

```go
backend, _ := newMyClassifier(...)          // implements classify.AudioClassifier
conv, _ := sdk.Open("caller.pack.json", "assistant",
    sdk.WithClassifier("my-ser", backend),
)
```

`WithClassifier` type-asserts the value against every task interface it satisfies and registers it for each. It does no credential resolution — it's the escape hatch for in-process classifiers and test doubles.

### Config-driven — `WithInferenceProvider` + a factory

If you'd rather select the backend from config (`type:`/`model:`/credentials), register a factory once and declare a provider spec:

```go
classify.RegisterFactory("my-onnx", func(spec classify.ProviderSpec) (classify.Backend, error) {
    return newMyClassifier(spec.Model, spec.AdditionalConfig)
})

conv, _ := sdk.Open("caller.pack.json", "assistant",
    sdk.WithInferenceProvider(sdk.ProviderSpec{ID: "my-ser", Type: "my-onnx", Model: "…"}),
)
```

## Wiring it to a check

Reference the registered id from the eval's `params.classifier_id`. `WithClassifier` registers a backend but does **not** make it the default, so name it explicitly:

```json
{
  "id": "caller_anger",
  "type": "audio_emotion",
  "trigger": "every_turn",
  "params": {
    "model": "wav2vec2-ser",
    "expected_label": "angry",
    "message_role": "user",
    "classifier_id": "onnx-ser"
  }
}
```

Observe results with an [eval hook](/sdk/how-to/observability/run-evals/) (`sdk.WithEvalHook`) or your metrics recorder.

## Worked example

The [`onnx-audio-emotion`](https://github.com/AltairaLabs/promptkit/tree/main/sdk/examples/onnx-audio-emotion) SDK example implements `classify.AudioClassifier` with an ONNX wav2vec2 model, registers it via `WithClassifier`, and scores caller audio with `audio_emotion` — offline, no token. Its cgo/ONNX dependency lives entirely in that standalone example module, so the runtime stays CGO-free. Keep native inference backends in the consumer, not the runtime.
